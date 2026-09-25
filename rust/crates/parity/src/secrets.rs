//! Refuse to keep a harness secret under `parity/`.

use anyhow::{anyhow, Result};
use regex::Regex;
use std::fs;
use std::path::Path;
use std::sync::OnceLock;

/// Every minted value starts with one of these. A fresh run mints new values,
/// so the prefixes are what still catch one that an earlier capture wrote.
pub const MINTED_PREFIXES: [&str; 4] = [
    "parity-token-",
    "parity-device-",
    "parity-user-",
    "parity-verify-",
];

/// The values the harness makes up for a run. Cases name them with
/// placeholders, and outputs show them as bracketed placeholders.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Minted {
    pub access_token: String,
    pub device_code: String,
    pub user_code: String,
    /// A path, the way the device-authorization endpoint returns it.
    pub verification_uri: String,
}

impl Minted {
    pub fn new(nonce: &str) -> Self {
        Self {
            access_token: format!("{}{nonce}", MINTED_PREFIXES[0]),
            device_code: format!("{}{nonce}", MINTED_PREFIXES[1]),
            user_code: format!("{}{nonce}", MINTED_PREFIXES[2]),
            verification_uri: format!("/confirm-auth/{}{nonce}", MINTED_PREFIXES[3]),
        }
    }

    /// Each value with the `{{NAME}}` a case writes for it.
    pub fn placeholders(&self) -> [(&'static str, &str); 4] {
        [
            ("{{ACCESS_TOKEN}}", &self.access_token),
            ("{{DEVICE_CODE}}", &self.device_code),
            ("{{USER_CODE}}", &self.user_code),
            ("{{VERIFICATION_URI}}", &self.verification_uri),
        ]
    }

    /// The name of the first minted value `text` still contains.
    pub fn leaked_in(&self, text: &str) -> Option<&'static str> {
        [
            ("access token", &self.access_token),
            ("device code", &self.device_code),
            ("user code", &self.user_code),
            ("verification URI", &self.verification_uri),
        ]
        .into_iter()
        .find(|(_, value)| !value.is_empty() && text.contains(value.as_str()))
        .map(|(name, _)| name)
    }
}

/// Scan `roots` for any non-empty secret.
pub fn scan_tree(roots: &[impl AsRef<Path>], secrets: &[String]) -> Result<()> {
    let needles: Vec<&str> = secrets
        .iter()
        .map(String::as_str)
        .filter(|s| !s.is_empty())
        .collect();
    if needles.is_empty() {
        return Ok(());
    }
    let mut hits = Vec::new();
    for root in roots {
        let root = root.as_ref();
        if !root.exists() {
            continue;
        }
        walk(root, &mut |path, text| {
            if needles.iter().any(|needle| text.contains(needle)) {
                hits.push(format!("{} contains a harness secret", path.display()));
            }
        })?;
    }
    finish(hits)
}

/// Fail when a file stores an `Authorization` value that is not a placeholder.
/// The header name alone is allowed, so help text can mention it.
pub fn scan_authorization(roots: &[impl AsRef<Path>]) -> Result<()> {
    let mut hits = Vec::new();
    for root in roots {
        let root = root.as_ref();
        if !root.exists() {
            continue;
        }
        walk(root, &mut |path, text| {
            let leaked = authorization_re().captures_iter(text).any(|cap| {
                let value = cap.get(1).map(|m| m.as_str()).unwrap_or("");
                !(value.starts_with('[') && value.ends_with(']'))
            });
            if leaked {
                hits.push(format!(
                    "{} contains an Authorization value",
                    path.display()
                ));
            }
        })?;
    }
    finish(hits)
}

fn finish(hits: Vec<String>) -> Result<()> {
    if hits.is_empty() {
        Ok(())
    } else {
        Err(anyhow!(
            "refusing to keep harness secrets under parity/:\n{}",
            hits.join("\n")
        ))
    }
}

fn authorization_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(r"(?im)authorization:[ \t]*(\S+)").expect("authorization regex"))
}

fn walk(dir: &Path, visit: &mut dyn FnMut(&Path, &str)) -> Result<()> {
    for entry in fs::read_dir(dir).map_err(|err| anyhow!("read {}: {err}", dir.display()))? {
        let entry = entry.map_err(|err| anyhow!(err))?;
        let path = entry.path();
        if path.is_dir() {
            walk(&path, visit)?;
            continue;
        }
        let bytes = fs::read(&path).map_err(|err| anyhow!("read {}: {err}", path.display()))?;
        visit(&path, &String::from_utf8_lossy(&bytes));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn scan_fails_when_a_captured_file_contains_the_token() {
        let dir = tempfile::tempdir().unwrap();
        let cases = dir.path().join("cases");
        fs::create_dir_all(cases.join("help")).unwrap();
        fs::write(
            cases.join("help").join("root.stdout"),
            "token parity-token-secret\n",
        )
        .unwrap();
        let err = scan_tree(&[cases], &["parity-token-secret".into()]).unwrap_err();
        assert!(err.to_string().contains("parity/"), "{err}");
        assert!(err.to_string().contains("root.stdout"), "{err}");
    }

    #[test]
    fn placeholder_text_is_not_a_secret() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("note.txt"), "Authorization: [REDACTED]\n").unwrap();
        scan_tree(&[dir.path()], &["parity-token-secret".into()]).unwrap();
        scan_authorization(&[dir.path()]).unwrap();
    }

    #[test]
    fn minted_values_are_distinct_and_each_carries_its_prefix() {
        let minted = Minted::new("abc");
        let values: Vec<&str> = minted.placeholders().iter().map(|(_, v)| *v).collect();
        for (value, prefix) in values.iter().zip(MINTED_PREFIXES) {
            assert!(value.contains(prefix), "{value} lacks {prefix}");
        }
        for (i, a) in values.iter().enumerate() {
            for b in &values[i + 1..] {
                assert!(!a.contains(b) && !b.contains(a), "{a} overlaps {b}");
            }
        }
    }

    #[test]
    fn a_leak_names_the_kind_of_value_that_leaked() {
        let minted = Minted::new("abc");
        assert_eq!(minted.leaked_in("nothing here"), None);
        assert_eq!(
            minted.leaked_in(&format!("code {}", minted.user_code)),
            Some("user code")
        );
        assert_eq!(
            minted.leaked_in(&format!("{{\"deviceCode\":\"{}\"}}", minted.device_code)),
            Some("device code")
        );
    }

    #[test]
    fn scan_rejects_a_device_code_an_earlier_capture_left_behind() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(
            dir.path().join("login.requests"),
            "{\"deviceCode\":\"parity-device-18a2f\"}\n",
        )
        .unwrap();
        let secrets: Vec<String> = MINTED_PREFIXES.iter().map(|p| p.to_string()).collect();
        let err = scan_tree(&[dir.path()], &secrets).unwrap_err();
        assert!(err.to_string().contains("login.requests"), "{err}");
    }

    #[test]
    fn authorization_value_fails_and_a_placeholder_does_not() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("login.stdout");
        fs::write(&path, "Authorization: api-live-token\n").unwrap();
        let err = scan_authorization(&[dir.path()]).unwrap_err();
        assert!(err.to_string().contains("Authorization"), "{err}");
        fs::write(&path, "Authorization: [ACCESS_TOKEN]\n").unwrap();
        scan_authorization(&[dir.path()]).unwrap();
    }
}
