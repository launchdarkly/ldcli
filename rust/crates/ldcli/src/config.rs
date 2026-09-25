//! The config file: where it lives and what it holds.
//!
//! `GetConfigFile` in `internal/config/config.go` uses `$XDG_CONFIG_HOME`, or
//! `$HOME/.config` when that is unset, on every operating system. The
//! `directories` crate would put this under Application Support on macOS,
//! which is why the path is assembled here instead.

use serde::Deserialize;
use std::ffi::OsString;
use std::path::{Path, PathBuf};

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

impl Config {
    /// Look a setting up by its flag name, so precedence can treat the file
    /// as one source among several.
    pub fn get(&self, flag: &str) -> Option<String> {
        match flag {
            "access-token" => self.access_token.clone(),
            "analytics-opt-out" => self.analytics_opt_out.map(|v| v.to_string()),
            "base-uri" => self.base_uri.clone(),
            "dev-stream-uri" => self.dev_stream_uri.clone(),
            "environment" => self.environment.clone(),
            "flag" => self.flag.clone(),
            "output" => self.output.clone(),
            "project" => self.project.clone(),
            "update-check-opt-out" => self.update_check_opt_out.map(|v| v.to_string()),
            _ => None,
        }
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

/// Read the config file. A missing or unreadable file is an empty config, and
/// so is invalid YAML: Go ignores the read error at startup and only reports
/// it from the `config` command itself.
pub fn load(path: Option<&PathBuf>) -> Config {
    let Some(path) = path else {
        return Config::default();
    };
    let Ok(text) = std::fs::read_to_string(path) else {
        return Config::default();
    };
    parse(&text).unwrap_or_default()
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

pub fn parse(text: &str) -> Result<Config, serde_yaml::Error> {
    // An empty document deserializes to a null, which is not a map.
    if text.trim().is_empty() {
        return Ok(Config::default());
    }
    serde_yaml::from_str(text)
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
    fn an_unset_xdg_config_home_falls_back_to_dot_config_under_home() {
        let env = env_from(&[("HOME", "/home/someone")]);
        assert_eq!(
            config_file(&env).unwrap(),
            PathBuf::from("/home/someone/.config/ldcli/config.yml")
        );
    }

    #[test]
    fn an_empty_xdg_config_home_is_treated_as_unset() {
        let env = env_from(&[("XDG_CONFIG_HOME", ""), ("HOME", "/home/someone")]);
        assert_eq!(
            config_file(&env).unwrap(),
            PathBuf::from("/home/someone/.config/ldcli/config.yml")
        );
    }

    #[test]
    fn hyphenated_keys_map_onto_their_fields() {
        let config = parse("access-token: api-abc\noutput: markdown\nanalytics-opt-out: true\n")
            .expect("parse");
        assert_eq!(config.get("access-token").as_deref(), Some("api-abc"));
        assert_eq!(config.get("output").as_deref(), Some("markdown"));
        assert_eq!(config.get("analytics-opt-out").as_deref(), Some("true"));
        assert_eq!(config.get("project"), None);
    }

    #[test]
    fn an_empty_document_is_an_empty_config() {
        assert_eq!(parse("").unwrap(), Config::default());
        assert_eq!(parse("{}\n").unwrap(), Config::default());
    }

    #[test]
    fn startup_creates_a_missing_config_file_with_an_empty_document() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ldcli").join("config.yml");
        ensure_file(&path);
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{}\n");
    }

    #[test]
    fn startup_leaves_an_existing_file_alone_even_when_it_is_invalid() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ldcli").join("config.yml");
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, "output: [unclosed\n").unwrap();
        ensure_file(&path);
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "output: [unclosed\n"
        );
        // A command that does not touch config still starts, because the
        // parse error is swallowed at startup.
        assert_eq!(load(Some(&path)), Config::default());
    }
}
