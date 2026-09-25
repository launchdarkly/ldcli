//! Root help and usage text.
//!
//! The Go root usage template is hand-maintained in `cmd/templates.go` and its
//! command list is locked by `cmd/templates_test.go`, so the list here is a
//! port of that literal rather than a walk of the command tree.

use crate::flags::{
    flag_usages, implicit_flags, persistent_flags, trim_trailing_whitespace, Flag, FlagKind,
};

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

/// Help for a subcommand that inherits the root template, which takes the
/// template's non-root branch: the use line, then local flags, then the
/// root's persistent flags under `Global flags`.
pub fn subcommand_help(long: &str, use_line: &str, local: &[Flag], inherited: &[Flag]) -> String {
    let mut out = format!(
        "{}\n\nUsage:\n  {use_line}\n",
        trim_trailing_whitespace(long)
    );
    if !local.is_empty() {
        out.push_str("\n\nFlags:\n");
        out.push_str(trim_trailing_whitespace(&flag_usages(local)));
    }
    if !inherited.is_empty() {
        out.push_str("\n\nGlobal flags:\n");
        out.push_str(trim_trailing_whitespace(&flag_usages(inherited)));
    }
    out.push('\n');
    out
}

/// Help on the resource template (`SubcommandUsageTemplate`) for a command
/// with no flags of its own. The template writes the help line itself, padded
/// to a fixed width, so it does not line up with the flags beneath it.
pub fn resource_help_without_local_flags(long: &str, use_line: &str, inherited: &[Flag]) -> String {
    format!(
        "{}\n\nUsage:\n  {use_line}\n\nGlobal flags:\n{:<29} Get help about any command\n{}\n",
        trim_trailing_whitespace(long),
        "  -h, --help",
        trim_trailing_whitespace(&flag_usages(inherited))
    )
}

/// `whoami` hides the root flags that do not apply to it: the token and base
/// URI come from config, and analytics opt-out is irrelevant.
pub fn whoami_help(default_output: &'static str) -> String {
    let visible: Vec<Flag> = persistent_flags(default_output)
        .into_iter()
        .filter(|flag| !matches!(flag.name, "access-token" | "base-uri" | "analytics-opt-out"))
        .collect();
    resource_help_without_local_flags(
        "Show information about the identity associated with the current access token.",
        "ldcli whoami [flags]",
        &visible,
    )
}

/// The `-h, --help` flag Cobra adds to each command.
pub fn help_flag(usage: &'static str) -> Flag {
    Flag {
        name: "help",
        shorthand: Some('h'),
        usage,
        kind: FlagKind::Bool,
    }
}

/// `login` has no long description, so Cobra falls back to the short one.
pub fn login_help(default_output: &'static str) -> String {
    subcommand_help(
        "Log in to your LaunchDarkly account to set up the CLI",
        "ldcli login [flags]",
        &[help_flag("help for login")],
        &persistent_flags(default_output),
    )
}

pub fn signup_help(default_output: &'static str) -> String {
    subcommand_help(
        "Open your browser to create a new LaunchDarkly account",
        "ldcli signup [flags]",
        &[help_flag("help for signup")],
        &persistent_flags(default_output),
    )
}

/// Setup's subcommands are hidden, so its help lists none and its use line
/// has no `[command]`.
pub fn setup_help(default_output: &'static str) -> String {
    subcommand_help(
        "Guided setup to integrate LaunchDarkly into your codebase.\n\nDetects your project's language and framework, installs the correct SDK,\ninitializes it with your environment's SDK key, creates a feature flag,\nand verifies the connection.",
        "ldcli setup [flags]",
        &[help_flag("help for setup")],
        &persistent_flags(default_output),
    )
}

pub fn setup_detect_help(default_output: &'static str) -> String {
    subcommand_help(
        "Detect language, framework, and recommended SDK for a project",
        "ldcli setup detect [flags]",
        &crate::setup::detect_flags(),
        &persistent_flags(default_output),
    )
}

pub fn setup_install_help(default_output: &'static str) -> String {
    subcommand_help(
        "Install the LaunchDarkly SDK package for the detected project",
        "ldcli setup install [flags]",
        &crate::setup::install_flags(),
        &persistent_flags(default_output),
    )
}

pub fn setup_init_help(default_output: &'static str) -> String {
    subcommand_help(
        "Inject LaunchDarkly SDK initialization code into a file",
        "ldcli setup init [flags]",
        &crate::setup::init_flags(),
        &persistent_flags(default_output),
    )
}

/// quickstart is built with `Use: "setup"` and renamed afterwards, so its
/// help is the old setup command's short description.
pub fn quickstart_help(default_output: &'static str) -> String {
    subcommand_help(
        "Setup guide to create your first feature flag",
        "ldcli quickstart [flags]",
        &[help_flag("help for quickstart")],
        &persistent_flags(default_output),
    )
}

/// Cobra prints this when a deprecated command executes, before it parses
/// the command's flags. `help quickstart` never executes quickstart, so it
/// prints no notice.
pub const QUICKSTART_DEPRECATED: &str =
    "Command \"quickstart\" is deprecated, use 'ldcli setup' for the new guided setup experience\n";

/// Cobra's own help command.
pub fn help_help(default_output: &'static str) -> String {
    subcommand_help(
        "Help provides help for any command in the application.\nSimply type ldcli help [path to command] for full details.",
        "ldcli help [command] [flags]",
        &[help_flag("help for help")],
        &persistent_flags(default_output),
    )
}

/// The help `help <topic>` prints for a command path Find settled on.
pub fn topic_help(path: &[&str], default_output: &'static str) -> Option<String> {
    let text = match path {
        [] => help_string(default_output),
        ["config"] => config_help(default_output),
        ["help"] => help_help(default_output),
        ["login"] => login_help(default_output),
        ["quickstart"] => quickstart_help(default_output),
        ["setup"] => setup_help(default_output),
        ["setup", "detect"] => setup_detect_help(default_output),
        ["setup", "init"] => setup_init_help(default_output),
        ["setup", "install"] => setup_install_help(default_output),
        ["signup"] => signup_help(default_output),
        ["whoami"] => whoami_help(default_output),
        _ => return None,
    };
    Some(text)
}

/// `config`'s help appends the list of settings to its long description.
pub fn config_help(default_output: &'static str) -> String {
    let mut long =
        String::from("View and modify specific configuration values\n\nSupported settings:\n");
    for (name, usage) in crate::config::SETTINGS {
        long.push_str(&format!("- `{name}`: {usage}\n"));
    }
    subcommand_help(
        &long,
        "ldcli config [flags]",
        &crate::flags::config_flags(),
        &persistent_flags(default_output),
    )
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
