//! Root help and usage text.
//!
//! The Go root usage template is hand-maintained in `cmd/templates.go` and its
//! command list is locked by `cmd/templates_test.go`, so the list here is a
//! port of that literal rather than a walk of the command tree.

use crate::flags::{flag_usages, implicit_flags, persistent_flags, trim_trailing_whitespace, Flag};

pub const LONG: &str = "LaunchDarkly CLI to control your feature flags";

/// Width the template pads each command name to before its description.
const NAME_WIDTH: usize = 29;

const COMMANDS: [(&str, &str); 7] = [
    (
        "setup",
        "Set up LaunchDarkly in your project (detect, install, initialize)",
    ),
    (
        "quickstart",
        "Create your first feature flag using a step-by-step guide (deprecated: use setup)",
    ),
    ("config", "View and modify specific configuration values"),
    (
        "completion",
        "Enable command autocompletion within supported shells",
    ),
    ("login", "Log in to your LaunchDarkly account"),
    ("signup", "Create a new LaunchDarkly account"),
    (
        "dev-server",
        "Run a development server to serve flags locally",
    ),
];

const RESOURCE_COMMANDS: [(&str, &str); 8] = [
    (
        "flags",
        "List, create, and modify feature flags and their targeting",
    ),
    ("environments", "List, create, and manage environments"),
    ("projects", "List, create, and manage projects"),
    ("members", "Invite new members to an account"),
    ("segments", "List, create, modify, and delete segments"),
    ("sourcemaps", "Manage sourcemaps for error monitoring"),
    ("symbols", "Manage symbol files for error monitoring"),
    (
        "...",
        "To see more resource commands, run 'ldcli resources'",
    ),
];

fn command_block(title: &str, entries: &[(&str, &str)]) -> String {
    let mut out = format!("\n{title}:\n");
    for (name, description) in entries {
        out.push_str(&format!("  {name:<NAME_WIDTH$} {description}\n"));
    }
    out
}

/// The usage block: everything from `Usage:` through the flag list.
pub fn usage_string(flags: &[Flag]) -> String {
    let mut out = String::from("Usage:\n  ldcli [command]\n");
    out.push_str(&command_block("Commands", &COMMANDS));
    out.push_str(&command_block(
        "Common resource commands",
        &RESOURCE_COMMANDS,
    ));
    out.push_str("\nFlags:\n");
    out.push_str(trim_trailing_whitespace(&flag_usages(flags)));
    out.push('\n');
    out
}

/// What `--help`, `-h`, `help`, and a bare invocation all print.
pub fn help_string(default_output: &'static str) -> String {
    let mut flags = persistent_flags(default_output);
    flags.extend(implicit_flags());
    format!("{LONG}\n\n{}", usage_string(&flags))
}

/// What the unknown-help-topic path prints. Cobra reaches this before it adds
/// the root's help and version flags, so neither appears in the block.
pub fn usage_without_implicit_flags(default_output: &'static str) -> String {
    usage_string(&persistent_flags(default_output))
}

/// Go renders the unknown topic with `%#q` over the argument slice.
pub fn unknown_help_topic(topics: &[String]) -> String {
    let quoted: Vec<String> = topics.iter().map(|topic| format!("`{topic}`")).collect();
    format!("Unknown help topic [{}]\n", quoted.join(" "))
}

pub fn version_line(version: &str) -> String {
    format!("ldcli version {version}\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn help_opens_with_the_long_description_and_a_blank_line() {
        let help = help_string("json");
        assert!(help.starts_with("LaunchDarkly CLI to control your feature flags\n\nUsage:\n"));
    }

    #[test]
    fn the_command_list_is_padded_to_a_fixed_column() {
        let help = help_string("json");
        assert!(help.contains("\n  setup                         Set up LaunchDarkly"));
        assert!(help.contains("\n  ...                           To see more resource commands"));
    }

    #[test]
    fn the_unknown_topic_path_drops_help_and_version() {
        let usage = usage_without_implicit_flags("json");
        assert!(!usage.contains("help for ldcli"), "{usage}");
        assert!(!usage.contains("version for ldcli"), "{usage}");
        assert!(usage.contains("--access-token"));
    }

    #[test]
    fn several_unknown_topics_are_quoted_as_a_list() {
        assert_eq!(
            unknown_help_topic(&["not-real".to_string()]),
            "Unknown help topic [`not-real`]\n"
        );
        assert_eq!(
            unknown_help_topic(&["a".to_string(), "b".to_string()]),
            "Unknown help topic [`a` `b`]\n"
        );
    }
}
