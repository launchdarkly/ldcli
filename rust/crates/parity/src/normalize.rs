//! Stream normalization shared by capture and check.
//!
//! JSON object keys are sorted. Help text stays text. Redaction covers the
//! fields R3 names, and secret substitution covers the values R17 names.
//!
//! A rule that rewrites content a command is supposed to print is opt-in per
//! case. Help text quotes UUIDs, timestamps, and versions as documentation,
//! and masking those bytes would hide a real difference between the two
//! binaries. A case that prints a genuinely varying value asks for the rule it
//! needs. Only the sandbox path and temp paths, which change on every run, are
//! always redacted, alongside the secret substitution R17 requires.

use anyhow::{anyhow, Result};
use regex::Regex;
use serde_json::{Map, Value};
use std::sync::OnceLock;

/// Opt-in redactions, named by a case's `redact` list.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct RedactionRules {
    pub uuid: bool,
    pub timestamp: bool,
    pub version: bool,
    pub hostname: bool,
    pub update_notice: bool,
    pub signup_browser: bool,
}

pub const REDACTION_RULE_NAMES: [&str; 6] = [
    "uuid",
    "timestamp",
    "version",
    "hostname",
    "update-notice",
    "signup-browser",
];

impl RedactionRules {
    pub fn parse(names: &[String]) -> Result<Self> {
        let mut rules = Self::default();
        for name in names {
            match name.as_str() {
                "uuid" => rules.uuid = true,
                "timestamp" => rules.timestamp = true,
                "version" => rules.version = true,
                "hostname" => rules.hostname = true,
                "update-notice" => rules.update_notice = true,
                "signup-browser" => rules.signup_browser = true,
                other => {
                    return Err(anyhow!(
                        "unknown redaction rule '{other}'; expected one of {}",
                        REDACTION_RULE_NAMES.join(", ")
                    ))
                }
            }
        }
        Ok(rules)
    }
}

#[derive(Debug, Clone, Default)]
pub struct Redactor {
    pub access_token: Option<String>,
    pub device_code: Option<String>,
    pub user_code: Option<String>,
    pub verification_uri: Option<String>,
    pub sandbox_root: Option<String>,
    /// The fixture server's address, which has a different port every run.
    pub base_uri: Option<String>,
    pub hostname: Option<String>,
    pub rules: RedactionRules,
}

impl Redactor {
    pub fn substitute_secrets(&self, input: &str) -> String {
        let mut out = input.to_string();
        // Longer values first so a token that contains another secret is replaced whole.
        let mut pairs: Vec<(&str, &str)> = Vec::new();
        if let Some(v) = self.access_token.as_deref() {
            pairs.push((v, "[ACCESS_TOKEN]"));
        }
        if let Some(v) = self.device_code.as_deref() {
            pairs.push((v, "[DEVICE_CODE]"));
        }
        if let Some(v) = self.user_code.as_deref() {
            pairs.push((v, "[USER_CODE]"));
        }
        if let Some(v) = self.verification_uri.as_deref() {
            pairs.push((v, "[VERIFICATION_URI]"));
        }
        pairs.sort_by_key(|(secret, _)| std::cmp::Reverse(secret.len()));
        for (secret, placeholder) in pairs {
            if secret.is_empty() {
                continue;
            }
            out = out.replace(secret, placeholder);
        }
        out
    }

    pub fn redact_ephemeral(&self, input: &str) -> String {
        let mut out = input.to_string();
        if let Some(root) = self.sandbox_root.as_deref() {
            if !root.is_empty() {
                out = out.replace(root, "[SANDBOX]");
            }
        }
        if let Some(base) = self.base_uri.as_deref() {
            if !base.is_empty() {
                out = out.replace(base, "[BASE_URI]");
            }
        }
        out = temp_path_re().replace_all(&out, "[TEMP]").into_owned();
        if self.rules.hostname {
            if let Some(host) = self.hostname.as_deref() {
                if host.len() > 1 {
                    out = out.replace(host, "[HOSTNAME]");
                }
            }
        }
        if self.rules.uuid {
            out = uuid_re().replace_all(&out, "[UUID]").into_owned();
        }
        if self.rules.timestamp {
            out = timestamp_re().replace_all(&out, "[TIMESTAMP]").into_owned();
        }
        if self.rules.version {
            out = semver_re().replace_all(&out, "[VERSION]").into_owned();
        }
        if self.rules.update_notice {
            out = update_notice_re()
                .replace_all(&out, "\n[UPDATE_NOTICE]\n")
                .into_owned();
        }
        if self.rules.signup_browser {
            out = signup_browser_re()
                .replace_all(
                    &out,
                    "Warning: failed to open browser automatically: [REDACTED]",
                )
                .into_owned();
        }
        out
    }
}

pub fn normalize_stream(raw: &str, redactor: &Redactor) -> String {
    let substituted = redactor.substitute_secrets(raw);
    let canonical = canonicalize_if_json(&substituted);
    redactor.redact_ephemeral(&canonical)
}

/// Canonicalize a JSON document: object keys sorted, array order preserved.
/// Returns an error when `input` is not a single JSON value.
pub fn canonicalize_json(input: &str) -> Result<String> {
    let trimmed = input.trim();
    let value: Value = serde_json::from_str(trimmed).map_err(|err| anyhow!(err))?;
    let mut out = canon_value(&value);
    if input.ends_with('\n') {
        out.push('\n');
    }
    Ok(out)
}

fn canonicalize_if_json(input: &str) -> String {
    match canonicalize_json(input) {
        Ok(canon) => canon,
        Err(_) => input.to_string(),
    }
}

fn canon_value(value: &Value) -> String {
    serde_json::to_string(&sorted_value(value)).unwrap_or_else(|_| "null".to_string())
}

fn sorted_value(value: &Value) -> Value {
    match value {
        Value::Object(map) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            let mut ordered = Map::new();
            for key in keys {
                if let Some(child) = map.get(key) {
                    ordered.insert(key.clone(), sorted_value(child));
                }
            }
            Value::Object(ordered)
        }
        Value::Array(items) => Value::Array(items.iter().map(sorted_value).collect()),
        other => other.clone(),
    }
}

fn uuid_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        Regex::new(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
            .expect("uuid regex")
    })
}

fn timestamp_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        Regex::new(r"\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?")
            .expect("timestamp regex")
    })
}

fn semver_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        Regex::new(r"\b\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?\b").expect("semver regex")
    })
}

fn temp_path_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(r#"(?:/tmp|/var/folders)/[^\s"']+"#).expect("temp path regex"))
}

fn update_notice_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        Regex::new(
            r"\nA new version of ldcli is available:.*\nhttps://github.com/launchdarkly/ldcli/releases/latest\n",
        )
        .expect("update notice regex")
    })
}

fn signup_browser_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        Regex::new(r"Warning: failed to open browser automatically:.*")
            .expect("signup browser regex")
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn json_key_order_compares_equal_and_extra_key_does_not() {
        let left = canonicalize_json("{\"b\":1,\"a\":{\"d\":true,\"c\":[1,2]}}\n").unwrap();
        let right = canonicalize_json("{\"a\":{\"c\":[1,2],\"d\":true},\"b\":1}\n").unwrap();
        assert_eq!(left, right);
        let extra = canonicalize_json("{\"a\":1,\"b\":2}\n").unwrap();
        let missing = canonicalize_json("{\"a\":1}\n").unwrap();
        assert_ne!(extra, missing);
    }

    #[test]
    fn non_json_is_left_as_text() {
        let redactor = Redactor::default();
        let help = "Usage:\n  ldcli [command]\n";
        assert_eq!(normalize_stream(help, &redactor), help);
    }

    #[test]
    fn help_text_keeps_the_uuids_timestamps_and_versions_it_documents() {
        let redactor = Redactor::default();
        let help = concat!(
            "  \"_id\": \"a1b2c3d4-e5f6-7890-abcd-ef1234567890\"\n",
            "  myField after \"2022-09-21T19:03:15Z\"\n",
            "ldcli version 0.28.1\n",
        );
        assert_eq!(normalize_stream(help, &redactor), help);
    }

    #[test]
    fn a_case_that_asks_for_a_rule_gets_it() {
        let rules = RedactionRules::parse(&["uuid".into(), "hostname".into()]).unwrap();
        let redactor = Redactor {
            hostname: Some("build-host".into()),
            rules,
            ..Redactor::default()
        };
        let raw = "id a1b2c3d4-e5f6-7890-abcd-ef1234567890 on build-host at 0.28.1\n";
        let got = normalize_stream(raw, &redactor);
        assert!(got.contains("[UUID]"), "{got}");
        assert!(got.contains("[HOSTNAME]"), "{got}");
        // Not requested, so the version stays comparable.
        assert!(got.contains("0.28.1"), "{got}");
    }

    #[test]
    fn an_unknown_rule_name_is_rejected() {
        let err = RedactionRules::parse(&["uuid".into(), "hostnmae".into()]).unwrap_err();
        assert!(err.to_string().contains("hostnmae"), "{err}");
        assert!(err.to_string().contains("hostname"), "{err}");
    }

    #[test]
    fn secrets_are_replaced_before_a_diff() {
        let redactor = Redactor {
            access_token: Some("parity-token-abc".into()),
            device_code: Some("device-123".into()),
            user_code: Some("USER-CODE".into()),
            verification_uri: Some("https://example.test/verify/device-123".into()),
            sandbox_root: Some("/tmp/case-a".into()),
            base_uri: Some("http://127.0.0.1:40123".into()),
            hostname: Some("buildhost".into()),
            rules: RedactionRules::parse(&["hostname".into()]).unwrap(),
        };
        let raw = "token parity-token-abc at /tmp/case-a on buildhost via http://127.0.0.1:40123/x device-123 USER-CODE https://example.test/verify/device-123\n";
        let got = normalize_stream(raw, &redactor);
        assert!(!got.contains("parity-token-abc"));
        assert!(!got.contains("device-123"));
        assert!(!got.contains("USER-CODE"));
        assert!(got.contains("[ACCESS_TOKEN]"));
        assert!(got.contains("[DEVICE_CODE]"));
        assert!(got.contains("[USER_CODE]"));
        assert!(got.contains("[VERIFICATION_URI]"));
        assert!(got.contains("[SANDBOX]"));
        assert!(got.contains("[BASE_URI]/x"), "{got}");
        assert!(got.contains("[HOSTNAME]"));
    }
}
