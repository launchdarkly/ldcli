//! The config file: where it lives, what it holds, and how it is rewritten.
//!
//! `GetConfigFile` in `internal/config/config.go` uses `$XDG_CONFIG_HOME`, or
//! `$HOME/.config` when that is unset, on every operating system. The
//! `directories` crate would put this under Application Support on macOS,
//! which is why the path is assembled here instead.
//!
//! Writes follow what Viper produces: every set field of the Go `Config`
//! struct, keys sorted, scalars quoted the way `yaml.v3` quotes them. A key
//! the struct does not have is dropped on the next write, as it is in Go.

use serde::Deserialize;
use serde_json::{Map, Value};
use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

/// Shown in place of a set access token wherever config is printed.
pub const REDACTED_VALUE: &str = "[REDACTED]";

/// Every key `config --set` accepts, with its help text: `cliflags.AllFlagsHelp`.
/// Four of these have no field in the Go `Config` struct, so setting them is
/// accepted and reported but writes nothing.
pub const SETTINGS: [(&str, &str); 13] = [
    ("access-token", "LaunchDarkly access token with write-level access"),
    ("analytics-opt-out", "Opt out of analytics tracking"),
    ("base-uri", "LaunchDarkly base URI"),
    ("cors-enabled", "Enable CORS headers for browser-based developer tools (default: false)"),
    ("cors-origin", "Allowed CORS origin. Use '*' for all origins (default: '*')"),
    ("dev-stream-uri", "Streaming service endpoint that the dev server uses to obtain authoritative flag data. This may be a LaunchDarkly or Relay Proxy endpoint"),
    ("environment", "Default environment key"),
    ("flag", "Default feature flag key"),
    ("output", "Output format: json, plaintext, or markdown (default: plaintext in a terminal, json otherwise)"),
    ("port", "Port for the dev server to run on"),
    ("project", "Default project key"),
    ("sync-once", "Only sync new projects. Existing projects will neither be resynced nor have overrides specified by CLI flags applied."),
    ("update-check-opt-out", "Opt out of update check"),
];

pub fn is_setting(key: &str) -> bool {
    SETTINGS.iter().any(|(name, _)| *name == key)
}

#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct Config {
    #[serde(default, rename = "access-token")]
    pub access_token: Option<String>,
    #[serde(default, rename = "analytics-opt-out")]
    pub analytics_opt_out: Option<bool>,
    #[serde(default, rename = "base-uri")]
    pub base_uri: Option<String>,
    #[serde(default, rename = "dev-stream-uri")]
    pub dev_stream_uri: Option<String>,
    #[serde(default)]
    pub environment: Option<String>,
    #[serde(default)]
    pub flag: Option<String>,
    #[serde(default)]
    pub output: Option<String>,
    #[serde(default)]
    pub project: Option<String>,
    #[serde(default, rename = "update-check-opt-out")]
    pub update_check_opt_out: Option<bool>,
}

/// A config value as it is written: text, or a bool.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Scalar {
    Text(String),
    Bool(bool),
}

impl Config {
    /// Look a setting up by its flag name, so precedence can treat the file
    /// as one source among several.
    pub fn get(&self, flag: &str) -> Option<String> {
        self.fields()
            .into_iter()
            .find(|(key, _)| *key == flag)
            .map(|(_, value)| match value {
                Scalar::Text(text) => text,
                Scalar::Bool(b) => b.to_string(),
            })
    }

    /// The fields Go would write, in key order. An empty string is omitted,
    /// because the Go struct tags say `omitempty`; a bool is kept even when
    /// false, because it is a pointer.
    pub fn fields(&self) -> Vec<(&'static str, Scalar)> {
        let text = |key: &'static str, value: &Option<String>| {
            value
                .as_ref()
                .filter(|v| !v.is_empty())
                .map(|v| (key, Scalar::Text(v.clone())))
        };
        let flag = |key: &'static str, value: &Option<bool>| value.map(|v| (key, Scalar::Bool(v)));
        [
            text("access-token", &self.access_token),
            flag("analytics-opt-out", &self.analytics_opt_out),
            text("base-uri", &self.base_uri),
            text("dev-stream-uri", &self.dev_stream_uri),
            text("environment", &self.environment),
            text("flag", &self.flag),
            text("output", &self.output),
            text("project", &self.project),
            flag("update-check-opt-out", &self.update_check_opt_out),
        ]
        .into_iter()
        .flatten()
        .collect()
    }

    /// The same config with a set access token hidden.
    pub fn redacted(&self) -> Config {
        let mut copy = self.clone();
        if copy.access_token.as_deref().is_some_and(|t| !t.is_empty()) {
            copy.access_token = Some(REDACTED_VALUE.to_string());
        }
        copy
    }

    /// The fields as a JSON object. The keys happen to be alphabetical in the
    /// Go struct as well, so a sorted map gives the same bytes.
    pub fn to_json_map(&self) -> Map<String, Value> {
        self.fields()
            .into_iter()
            .map(|(key, value)| {
                let value = match value {
                    Scalar::Text(text) => Value::String(text),
                    Scalar::Bool(b) => Value::Bool(b),
                };
                (key.to_string(), value)
            })
            .collect()
    }

    /// `Config.Update`: key-value pairs, validated before any of them is set.
    /// Returns the keys in the order given.
    pub fn update(&self, kvs: &[String]) -> Result<(Config, Vec<String>), String> {
        if !kvs.len().is_multiple_of(2) {
            return Err("flag needs an argument: --set".to_string());
        }
        for key in kvs.iter().step_by(2) {
            if !is_setting(key) {
                return Err(format!(
                    "{} is not a valid configuration option",
                    safe_key_for_error(key)
                ));
            }
        }
        let mut updated = self.clone();
        let mut keys = Vec::new();
        for pair in kvs.chunks(2) {
            let (key, value) = (pair[0].as_str(), pair[1].clone());
            keys.push(key.to_string());
            match key {
                "access-token" => updated.access_token = Some(value),
                "analytics-opt-out" => {
                    updated.analytics_opt_out = Some(
                        parse_go_bool(&value).ok_or("analytics-opt-out must be true or false")?,
                    )
                }
                "base-uri" => updated.base_uri = Some(value),
                "dev-stream-uri" => updated.dev_stream_uri = Some(value),
                "environment" => updated.environment = Some(value),
                "flag" => updated.flag = Some(value),
                "output" => {
                    let kind = crate::output::OutputKind::parse(&value)?;
                    updated.output = Some(kind.as_str().to_string());
                }
                "project" => updated.project = Some(value),
                "update-check-opt-out" => {
                    updated.update_check_opt_out = Some(
                        parse_go_bool(&value)
                            .ok_or("update-check-opt-out must be true or false")?,
                    )
                }
                // Accepted and reported, but the Go struct has no field for it.
                _ => {}
            }
        }
        Ok((updated, keys))
    }

    /// `Config.Remove` only validates the key; the write leaves it out.
    pub fn validate_removal(key: &str) -> Result<(), String> {
        if is_setting(key) {
            Ok(())
        } else {
            Err(format!(
                "{} is not a valid configuration option",
                safe_key_for_error(key)
            ))
        }
    }

    /// The YAML document Viper writes for this config, less `skip`.
    pub fn to_yaml(&self, skip: Option<&str>) -> String {
        let lines: Vec<String> = self
            .fields()
            .into_iter()
            .filter(|(key, _)| Some(*key) != skip)
            .map(|(key, value)| match value {
                Scalar::Text(text) => format!("{key}: {}", yaml_scalar(&text)),
                Scalar::Bool(b) => format!("{key}: {b}"),
            })
            .collect();
        if lines.is_empty() {
            "{}\n".to_string()
        } else {
            format!("{}\n", lines.join("\n"))
        }
    }
}

/// `strconv.ParseBool`.
fn parse_go_bool(value: &str) -> Option<bool> {
    match value {
        "1" | "t" | "T" | "TRUE" | "true" | "True" => Some(true),
        "0" | "f" | "F" | "FALSE" | "false" | "False" => Some(false),
        _ => None,
    }
}

/// An unrecognized key is echoed back so a typo is easy to spot, unless it
/// does not look like a key at all: a transposed `--set` can put an access
/// token in that position.
fn safe_key_for_error(key: &str) -> String {
    static KEY_SHAPE: OnceLock<regex::Regex> = OnceLock::new();
    let shape = KEY_SHAPE
        .get_or_init(|| regex::Regex::new(r"^[a-z0-9]+(-[a-z0-9]+)*$").expect("key regex"));
    if key.len() <= 32 && shape.is_match(key) {
        key.to_string()
    } else {
        REDACTED_VALUE.to_string()
    }
}

/// Resolve the config file path from the environment.
pub fn config_file(env: &dyn Fn(&str) -> Option<OsString>) -> Option<PathBuf> {
    let base = match env("XDG_CONFIG_HOME").filter(|value| !value.is_empty()) {
        Some(value) => PathBuf::from(value),
        None => PathBuf::from(env("HOME").filter(|value| !value.is_empty())?).join(".config"),
    };
    Some(base.join("ldcli").join("config.yml"))
}

/// Read the config file for precedence. A missing, unreadable, or invalid
/// file is an empty config: Go ignores the read error at startup and only
/// reports it from the `config` command itself.
pub fn load(path: Option<&PathBuf>) -> Config {
    path.and_then(|path| load_strict(path).ok())
        .unwrap_or_default()
}

/// `config.New`: read and parse, with the error text Go uses.
pub fn load_strict(path: &Path) -> Result<Config, String> {
    let text = std::fs::read_to_string(path).map_err(|_| {
        format!(
            "unable to open config file. The file or directory may not exist: {}",
            path.display()
        )
    })?;
    parse(&text).map_err(|_| "config file is invalid yaml".to_string())
}

/// Go's startup creates the config file when it is absent and leaves it alone
/// otherwise: `setFlagsFromConfig` makes the directory, and a failed read is
/// followed by `SafeWriteConfigAs`, which refuses to overwrite. An empty Viper
/// config serializes to `{}`. Invalid YAML therefore keeps its bytes, because
/// the file already exists.
pub fn ensure_file(path: &Path) {
    if let Some(dir) = path.parent() {
        // A failure here surfaces on the next read; Go discards it too.
        let _ = std::fs::create_dir_all(dir);
    }
    if !path.exists() {
        let _ = std::fs::write(path, "{}\n");
    }
}

/// Replace the file with a complete document or leave it untouched: the new
/// bytes go to a sibling temp file, which is renamed over the old one. The old
/// file's permissions carry over.
pub fn write_atomic(path: &Path, contents: &str) -> std::io::Result<()> {
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    std::fs::create_dir_all(dir)?;
    let temp = dir.join(format!(
        ".{}.{}.tmp",
        path.file_name()
            .and_then(|name| name.to_str())
            .unwrap_or("config.yml"),
        std::process::id()
    ));
    let result = (|| {
        std::fs::write(&temp, contents)?;
        if let Ok(metadata) = std::fs::metadata(path) {
            std::fs::set_permissions(&temp, metadata.permissions())?;
        }
        std::fs::rename(&temp, path)
    })();
    if result.is_err() {
        let _ = std::fs::remove_file(&temp);
    }
    result
}

pub fn parse(text: &str) -> Result<Config, serde_yaml::Error> {
    // An empty document deserializes to a null, which is not a map.
    if text.trim().is_empty() {
        return Ok(Config::default());
    }
    serde_yaml::from_str(text)
}

/// Quote a string the way `yaml.v3` does in a block mapping.
///
/// Double quotes when the plain text would read back as something other than
/// a string, which includes the YAML 1.1 words such as `yes` and `off`, or
/// when it holds a control character. Single quotes when the text cannot be a
/// plain scalar for syntactic reasons. Otherwise plain.
pub fn yaml_scalar(text: &str) -> String {
    if text.chars().any(char::is_control) || resolves_to_non_string(text) {
        return double_quoted(text);
    }
    if !plain_allowed(text) {
        return format!("'{}'", text.replace('\'', "''"));
    }
    text.to_string()
}

fn resolves_to_non_string(text: &str) -> bool {
    const WORDS: [&str; 32] = [
        "y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO", "true", "True", "TRUE", "false",
        "False", "FALSE", "on", "On", "ON", "off", "Off", "OFF", "~", "null", "Null", "NULL",
        ".inf", ".Inf", ".INF", ".nan", ".NaN", ".NAN",
    ];
    static NUMERIC: OnceLock<regex::Regex> = OnceLock::new();
    let numeric = NUMERIC.get_or_init(|| {
        regex::Regex::new(concat!(
            r"^(?:",
            // Integers in the bases strconv.ParseInt accepts with base 0.
            r"[-+]?(?:0[xX][0-9a-fA-F_]+|0[oO]?[0-7_]+|0[bB][01_]+|[0-9][0-9_]*)",
            // Floats in the style yaml.v3 resolves, and signed infinities.
            r"|[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?",
            r"|[-+]\.(?:inf|Inf|INF)",
            // Sexagesimal numbers, which YAML 1.1 reads as floats.
            r"|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+(?:\.[0-9_]*)?",
            // Dates and timestamps.
            r"|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:[Tt ][0-9]{1,2}:[0-9]{1,2}:[0-9]{1,2}(?:\.[0-9]*)?(?:Z|[-+][0-9]{1,2}(?::[0-9]{2})?)?)?",
            r")$"
        ))
        .expect("numeric regex")
    });
    text.is_empty() || WORDS.contains(&text) || numeric.is_match(text)
}

fn plain_allowed(text: &str) -> bool {
    let Some(first) = text.chars().next() else {
        return false;
    };
    if text.starts_with("---") || text.starts_with("...") {
        return false;
    }
    if text.starts_with(' ') || text.ends_with(' ') {
        return false;
    }
    if ",[]{}#&*!|>'\"%@`".contains(first) {
        return false;
    }
    // `-`, `?`, and `:` start a plain scalar only when followed by something.
    if "-?:".contains(first) {
        match text.chars().nth(1) {
            None | Some(' ') => return false,
            _ => {}
        }
    }
    !(text.contains(": ") || text.ends_with(':') || text.contains(" #"))
}

fn double_quoted(text: &str) -> String {
    let mut out = String::from("\"");
    for ch in text.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\t' => out.push_str("\\t"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\0' => out.push_str("\\0"),
            c if c.is_control() => out.push_str(&format!("\\x{:02X}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env_from<'a>(pairs: &'a [(&'a str, &'a str)]) -> impl Fn(&str) -> Option<OsString> + 'a {
        move |key: &str| {
            pairs
                .iter()
                .find(|(name, _)| *name == key)
                .map(|(_, value)| OsString::from(*value))
        }
    }

    #[test]
    fn xdg_config_home_wins_when_it_is_set() {
        let env = env_from(&[("XDG_CONFIG_HOME", "/tmp/xdg"), ("HOME", "/home/someone")]);
        assert_eq!(
            config_file(&env).unwrap(),
            PathBuf::from("/tmp/xdg/ldcli/config.yml")
        );
    }

    #[test]
    fn an_unset_or_empty_xdg_config_home_falls_back_to_dot_config_under_home() {
        for env in [
            env_from(&[("HOME", "/home/someone")]),
            env_from(&[("XDG_CONFIG_HOME", ""), ("HOME", "/home/someone")]),
        ] {
            assert_eq!(
                config_file(&env).unwrap(),
                PathBuf::from("/home/someone/.config/ldcli/config.yml")
            );
        }
    }

    #[test]
    fn hyphenated_keys_map_onto_their_fields_and_unknown_keys_are_ignored() {
        let config =
            parse("access-token: api-abc\noutput: markdown\nanalytics-opt-out: true\nport: 9000\n")
                .expect("parse");
        assert_eq!(config.get("access-token").as_deref(), Some("api-abc"));
        assert_eq!(config.get("output").as_deref(), Some("markdown"));
        assert_eq!(config.get("analytics-opt-out").as_deref(), Some("true"));
        assert_eq!(config.get("project"), None);
        assert_eq!(config.get("port"), None);
    }

    #[test]
    fn an_empty_document_is_an_empty_config() {
        assert_eq!(parse("").unwrap(), Config::default());
        assert_eq!(parse("{}\n").unwrap(), Config::default());
    }

    #[test]
    fn startup_creates_a_missing_file_and_leaves_an_invalid_one_alone() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ldcli").join("config.yml");
        ensure_file(&path);
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{}\n");

        std::fs::write(&path, "output: [unclosed\n").unwrap();
        ensure_file(&path);
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "output: [unclosed\n"
        );
        assert_eq!(load(Some(&path)), Config::default());
        assert_eq!(
            load_strict(&path).unwrap_err(),
            "config file is invalid yaml"
        );
    }

    #[test]
    fn the_written_document_sorts_keys_keeps_false_bools_and_drops_empty_strings() {
        let config = Config {
            project: Some("p".into()),
            analytics_opt_out: Some(false),
            environment: Some(String::new()),
            output: Some("json".into()),
            ..Config::default()
        };
        assert_eq!(
            config.to_yaml(None),
            "analytics-opt-out: false\noutput: json\nproject: p\n"
        );
        assert_eq!(
            config.to_yaml(Some("project")),
            "analytics-opt-out: false\noutput: json\n"
        );
        assert_eq!(Config::default().to_yaml(None), "{}\n");
    }

    #[test]
    fn scalars_are_quoted_the_way_yaml_v3_quotes_them() {
        let cases = [
            (
                "https://app.launchdarkly.com",
                "https://app.launchdarkly.com",
            ),
            ("api-abc123", "api-abc123"),
            ("a b", "a b"),
            ("a'b", "a'b"),
            ("yes", "\"yes\""),
            ("off", "\"off\""),
            ("True", "\"True\""),
            ("~", "\"~\""),
            ("NULL", "\"NULL\""),
            ("1.5", "\"1.5\""),
            ("0x10", "\"0x10\""),
            ("0o17", "\"0o17\""),
            ("1e3", "\"1e3\""),
            (".nan", "\".nan\""),
            ("2024-01-01", "\"2024-01-01\""),
            ("12345", "\"12345\""),
            ("tab\tx", "\"tab\\tx\""),
            ("a: b", "'a: b'"),
            ("#x", "'#x'"),
            (" lead", "' lead'"),
            ("trail ", "'trail '"),
            ("-", "'-'"),
            ("@x", "'@x'"),
            ("*x", "'*x'"),
            ("[x", "'[x'"),
        ];
        for (input, expected) in cases {
            assert_eq!(yaml_scalar(input), expected, "for {input:?}");
        }
    }

    #[test]
    fn an_update_validates_every_key_before_setting_any_value() {
        let base = Config::default();
        let kvs = |items: &[&str]| items.iter().map(|s| s.to_string()).collect::<Vec<_>>();

        assert_eq!(
            base.update(&kvs(&["output"])).unwrap_err(),
            "flag needs an argument: --set"
        );
        assert_eq!(
            base.update(&kvs(&["project", "p", "nope", "x"]))
                .unwrap_err(),
            "nope is not a valid configuration option"
        );
        assert_eq!(
            base.update(&kvs(&["api-0123456789abcdef0123456789abcdef", "x"]))
                .unwrap_err(),
            "[REDACTED] is not a valid configuration option"
        );
        assert_eq!(
            base.update(&kvs(&["analytics-opt-out", "maybe"]))
                .unwrap_err(),
            "analytics-opt-out must be true or false"
        );
        assert_eq!(
            base.update(&kvs(&["output", "yaml"])).unwrap_err(),
            crate::output::INVALID_OUTPUT_KIND
        );

        let (updated, keys) = base
            .update(&kvs(&[
                "project",
                "p",
                "port",
                "9000",
                "analytics-opt-out",
                "T",
            ]))
            .unwrap();
        assert_eq!(keys, vec!["project", "port", "analytics-opt-out"]);
        assert_eq!(updated.project.as_deref(), Some("p"));
        assert_eq!(updated.analytics_opt_out, Some(true));
        // port is accepted, but there is nowhere to put it.
        assert_eq!(
            updated.to_yaml(None),
            "analytics-opt-out: true\nproject: p\n"
        );
    }

    #[test]
    fn a_set_access_token_is_redacted_and_an_unset_one_stays_absent() {
        let config = Config {
            access_token: Some("api-secret".into()),
            ..Config::default()
        };
        assert_eq!(
            config.redacted().to_json_map()["access-token"],
            Value::String(REDACTED_VALUE.into())
        );
        assert!(!Config::default()
            .redacted()
            .to_json_map()
            .contains_key("access-token"));
    }

    #[test]
    fn an_atomic_write_replaces_the_file_whole_and_keeps_its_permissions() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.yml");
        std::fs::write(&path, "old: value\n").unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)).unwrap();

        write_atomic(&path, "project: p\n").unwrap();
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "project: p\n");
        let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
        // Nothing is left behind next to it.
        assert_eq!(std::fs::read_dir(dir.path()).unwrap().count(), 1);
    }
}
