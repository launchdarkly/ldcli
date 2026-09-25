//! Analytics event properties.
//!
//! The harness never posts these, so the property map is what has to match.
//! `known_agents.json` is read from the Go tree at build time so the two
//! binaries cannot drift apart on agent detection.

use serde::Deserialize;
use serde_json::{Map, Value};
use std::ffi::OsString;
use std::sync::OnceLock;

const KNOWN_AGENTS: &str = include_str!("../../../../cmd/analytics/known_agents.json");

pub const HELP: &str = "help";
pub const ERROR: &str = "error";
pub const SUCCESS: &str = "success";

#[derive(Debug, Deserialize)]
struct AgentEnvVar {
    env_var: String,
    label: String,
}

#[derive(Debug, Deserialize)]
struct KnownAgents {
    agents: Vec<AgentEnvVar>,
    ci_env_vars: Vec<String>,
}

fn known_agents() -> &'static KnownAgents {
    static PARSED: OnceLock<KnownAgents> = OnceLock::new();
    PARSED.get_or_init(|| serde_json::from_str(KNOWN_AGENTS).expect("known_agents.json"))
}

/// A label for the environment the CLI is running in, or an empty string when
/// it looks like a person at a terminal.
///
/// The terminal check comes before the CI check, so an interactive shell
/// inside CI reports as interactive.
pub fn detect_agent_context(
    env: &dyn Fn(&str) -> Option<OsString>,
    stdin_is_terminal: bool,
    stdout_is_terminal: bool,
) -> String {
    let lookup = |name: &str| {
        env(name)
            .and_then(|value| value.into_string().ok())
            .filter(|value| !value.is_empty())
    };

    if let Some(value) = lookup("LD_CLI_AGENT") {
        return format!("explicit:{value}");
    }
    for agent in &known_agents().agents {
        if lookup(&agent.env_var).is_some() {
            return agent.label.clone();
        }
    }
    if stdin_is_terminal || stdout_is_terminal {
        return String::new();
    }
    for ci in &known_agents().ci_env_vars {
        if lookup(ci).is_some() {
            return "ci".to_string();
        }
    }
    "unknown-non-interactive".to_string()
}

/// The `CLI Command Run` properties. `flags` are the flag names actually set
/// on argv, in the order pflag visits them. `base_uri` is reported only when
/// it differs from the default.
pub fn cmd_run_event_properties(
    name: &str,
    action: &str,
    flags: &[String],
    base_uri: &str,
    overrides: &[(&str, Value)],
) -> Map<String, Value> {
    let mut properties = Map::new();
    properties.insert("name".to_string(), Value::String(name.to_string()));
    properties.insert("action".to_string(), Value::String(action.to_string()));
    properties.insert(
        "flags".to_string(),
        Value::Array(flags.iter().cloned().map(Value::String).collect()),
    );
    if base_uri != crate::settings::BASE_URI_DEFAULT {
        properties.insert("baseURI".to_string(), Value::String(base_uri.to_string()));
    }
    for (key, value) in overrides {
        properties.insert((*key).to_string(), value.clone());
    }
    properties
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn lookup_from(pairs: Vec<(String, String)>) -> impl Fn(&str) -> Option<OsString> {
        move |key: &str| {
            pairs
                .iter()
                .find(|(name, _)| name == key)
                .map(|(_, value)| OsString::from(value))
        }
    }

    #[test]
    fn the_embedded_agent_list_is_the_one_the_go_binary_uses() {
        let agents = known_agents();
        assert!(!agents.agents.is_empty());
        assert!(!agents.ci_env_vars.is_empty());
        let go_source = std::fs::read_to_string(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../../cmd/analytics/known_agents.json"
        ))
        .expect("read the Go copy");
        assert_eq!(go_source, KNOWN_AGENTS);
    }

    #[test]
    fn an_explicit_agent_wins_over_everything() {
        let env = lookup_from(vec![("LD_CLI_AGENT".into(), "my-agent".into())]);
        assert_eq!(
            detect_agent_context(&env, false, false),
            "explicit:my-agent"
        );
    }

    #[test]
    fn a_known_agent_variable_wins_over_ci_and_the_terminal() {
        let agent = &known_agents().agents[0];
        let env = lookup_from(vec![(agent.env_var.clone(), "1".into())]);
        assert_eq!(detect_agent_context(&env, true, true), agent.label);
    }

    #[test]
    fn a_terminal_is_reported_as_interactive_even_inside_ci() {
        let ci = known_agents().ci_env_vars[0].clone();
        let env = lookup_from(vec![(ci, "true".into())]);
        assert_eq!(detect_agent_context(&env, true, false), "");
        assert_eq!(detect_agent_context(&env, false, true), "");
        assert_eq!(detect_agent_context(&env, false, false), "ci");
    }

    #[test]
    fn no_signal_at_all_is_unknown_non_interactive() {
        let env = lookup_from(Vec::new());
        assert_eq!(
            detect_agent_context(&env, false, false),
            "unknown-non-interactive"
        );
    }

    #[test]
    fn properties_carry_the_name_action_and_flags_that_were_set() {
        let properties = cmd_run_event_properties(
            "flags",
            "list",
            &["project".to_string(), "output".to_string()],
            crate::settings::BASE_URI_DEFAULT,
            &[],
        );
        assert_eq!(properties["name"], json!("flags"));
        assert_eq!(properties["action"], json!("list"));
        assert_eq!(properties["flags"], json!(["project", "output"]));
        // The default base URI is left out entirely.
        assert!(!properties.contains_key("baseURI"));
    }

    #[test]
    fn a_non_default_base_uri_is_reported_and_overrides_win() {
        let properties = cmd_run_event_properties(
            "flags",
            "list",
            &[],
            "https://example.test",
            &[("action", json!("help"))],
        );
        assert_eq!(properties["baseURI"], json!("https://example.test"));
        assert_eq!(properties["action"], json!("help"));
    }

    #[test]
    fn no_property_carries_a_token_or_an_authorization_header() {
        let properties = cmd_run_event_properties(
            "flags",
            "list",
            &["access-token".to_string()],
            "https://example.test",
            &[],
        );
        let rendered = serde_json::to_string(&properties).unwrap();
        // The flag name is recorded; its value never is.
        assert!(rendered.contains("access-token"));
        assert!(!rendered.to_lowercase().contains("authorization"));
        assert!(!rendered.contains("api-"));
    }
}
