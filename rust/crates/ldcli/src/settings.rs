//! Precedence: an explicit flag, then an `LD_` environment variable, then the
//! config file, then the default.
//!
//! Viper's own merge order lets the environment win over a flag, so this is
//! spelled out here instead. The environment name is the flag name uppercased
//! with hyphens turned into underscores, which is what `SetEnvKeyReplacer`
//! does in `cmd/root.go`.

use crate::config::Config;
use std::collections::BTreeMap;
use std::ffi::OsString;

pub const BASE_URI_DEFAULT: &str = "https://app.launchdarkly.com";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Source {
    Flag,
    Environment,
    ConfigFile,
    Default,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Resolved {
    pub value: String,
    pub source: Source,
}

pub fn env_name(flag: &str) -> String {
    format!("LD_{}", flag.to_uppercase().replace('-', "_"))
}

/// Resolve one setting. `flags` holds only the flags actually present on argv.
pub fn resolve(
    flag: &str,
    flags: &BTreeMap<String, String>,
    env: &dyn Fn(&str) -> Option<OsString>,
    config: &Config,
    default: &str,
) -> Resolved {
    if let Some(value) = flags.get(flag) {
        return Resolved {
            value: value.clone(),
            source: Source::Flag,
        };
    }
    if let Some(value) = env(&env_name(flag))
        .and_then(|value| value.into_string().ok())
        .filter(|value| !value.is_empty())
    {
        return Resolved {
            value,
            source: Source::Environment,
        };
    }
    if let Some(value) = config.get(flag).filter(|value| !value.is_empty()) {
        return Resolved {
            value,
            source: Source::ConfigFile,
        };
    }
    Resolved {
        value: default.to_string(),
        source: Source::Default,
    }
}

/// Go defaults `--output` to plaintext in a terminal and json otherwise, and
/// treats any non-empty `FORCE_TTY` or `LD_FORCE_TTY` as a terminal.
pub fn default_output(
    stdout_is_terminal: bool,
    env: &dyn Fn(&str) -> Option<OsString>,
) -> &'static str {
    let forced = ["FORCE_TTY", "LD_FORCE_TTY"]
        .iter()
        .any(|name| env(name).is_some_and(|value| !value.is_empty()));
    if forced || stdout_is_terminal {
        "plaintext"
    } else {
        "json"
    }
}

/// `--json` is shorthand for `--output json` on the commands that honor it.
/// It is a flag only: `LD_JSON` has no effect, because the Go flag is never
/// bound to Viper.
pub fn output_kind(json_flag: bool, resolved_output: &str) -> String {
    if json_flag {
        "json".to_string()
    } else {
        resolved_output.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env_from(
        pairs: &'static [(&'static str, &'static str)],
    ) -> impl Fn(&str) -> Option<OsString> {
        move |key: &str| {
            pairs
                .iter()
                .find(|(name, _)| *name == key)
                .map(|(_, value)| OsString::from(*value))
        }
    }

    fn flags(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
        pairs
            .iter()
            .map(|(k, v)| ((*k).to_string(), (*v).to_string()))
            .collect()
    }

    fn config_with_output(output: &str) -> Config {
        Config {
            output: Some(output.to_string()),
            ..Config::default()
        }
    }

    #[test]
    fn a_flag_beats_the_environment_and_the_config_file() {
        let resolved = resolve(
            "output",
            &flags(&[("output", "plaintext")]),
            &env_from(&[("LD_OUTPUT", "json")]),
            &config_with_output("markdown"),
            "json",
        );
        assert_eq!(resolved.value, "plaintext");
        assert_eq!(resolved.source, Source::Flag);
    }

    #[test]
    fn the_environment_beats_the_config_file() {
        let resolved = resolve(
            "output",
            &flags(&[]),
            &env_from(&[("LD_OUTPUT", "json")]),
            &config_with_output("markdown"),
            "plaintext",
        );
        assert_eq!(resolved.value, "json");
        assert_eq!(resolved.source, Source::Environment);
    }

    #[test]
    fn the_config_file_beats_the_default() {
        let resolved = resolve(
            "output",
            &flags(&[]),
            &env_from(&[]),
            &config_with_output("markdown"),
            "json",
        );
        assert_eq!(resolved.value, "markdown");
        assert_eq!(resolved.source, Source::ConfigFile);
    }

    #[test]
    fn nothing_set_falls_through_to_the_default() {
        let resolved = resolve(
            "output",
            &flags(&[]),
            &env_from(&[]),
            &Config::default(),
            "json",
        );
        assert_eq!(resolved.value, "json");
        assert_eq!(resolved.source, Source::Default);
    }

    #[test]
    fn a_hyphenated_flag_reads_an_underscored_variable() {
        assert_eq!(env_name("access-token"), "LD_ACCESS_TOKEN");
        assert_eq!(env_name("analytics-opt-out"), "LD_ANALYTICS_OPT_OUT");
        assert_eq!(env_name("output"), "LD_OUTPUT");
        let resolved = resolve(
            "access-token",
            &flags(&[]),
            &env_from(&[("LD_ACCESS_TOKEN", "api-from-env")]),
            &Config::default(),
            "",
        );
        assert_eq!(resolved.value, "api-from-env");
    }

    #[test]
    fn output_defaults_to_json_off_a_terminal_and_plaintext_on_one() {
        assert_eq!(default_output(false, &env_from(&[])), "json");
        assert_eq!(default_output(true, &env_from(&[])), "plaintext");
    }

    #[test]
    fn any_non_empty_force_tty_value_counts_including_zero() {
        assert_eq!(
            default_output(false, &env_from(&[("FORCE_TTY", "1")])),
            "plaintext"
        );
        assert_eq!(
            default_output(false, &env_from(&[("LD_FORCE_TTY", "1")])),
            "plaintext"
        );
        // Go tests the variable for emptiness, not for truth.
        assert_eq!(
            default_output(false, &env_from(&[("FORCE_TTY", "0")])),
            "plaintext"
        );
        assert_eq!(
            default_output(false, &env_from(&[("FORCE_TTY", "")])),
            "json"
        );
    }

    #[test]
    fn the_json_flag_overrides_output_but_ld_json_does_not() {
        assert_eq!(output_kind(true, "markdown"), "json");
        assert_eq!(output_kind(false, "markdown"), "markdown");
        // LD_JSON is not bound to anything, so it cannot reach output_kind.
        let resolved = resolve(
            "output",
            &flags(&[]),
            &env_from(&[("LD_JSON", "true"), ("LD_OUTPUT", "markdown")]),
            &Config::default(),
            "json",
        );
        assert_eq!(output_kind(false, &resolved.value), "markdown");
    }
}
