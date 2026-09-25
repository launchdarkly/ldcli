//! Argv parsing and the exit codes that go with it.
//!
//! clap parses; the messages and exit codes are Cobra's. Cobra sets
//! `SilenceErrors` and `SilenceUsage` and then prints the error itself before
//! `os.Exit(1)`, so a usage error prints one line to stderr with no usage
//! block, and exits 1 rather than clap's 2.
//!
//! The root's persistent flags are global, so they parse before or after a
//! subcommand. Help and version belong to the root alone: placed before a
//! subcommand they still print the root's help or version, as Cobra does,
//! and after one they are unknown.

use crate::cobra_find::{self, Found};
use crate::config_cmd::{self, ConfigArgs, ConfigContext};
use crate::flags::{persistent_flags, Flag};
use crate::login::{self, LoginContext};
use crate::signup::{self, SignupContext};
use crate::whoami::{self, WhoamiContext};
use crate::{config, help, settings, setup};
use clap::error::{ContextKind, ContextValue, ErrorKind};
use clap::parser::ValueSource;
use clap::{Arg, ArgAction, ArgMatches, Command};
use std::collections::BTreeMap;
use std::ffi::OsString;
use std::path::PathBuf;

pub struct Env<'a> {
    pub var: &'a dyn Fn(&str) -> Option<OsString>,
    pub stdout_is_terminal: bool,
    /// Writes to stdout at once, for output that has to appear before the
    /// command finishes. Everything else goes in the `Outcome`.
    pub stdout: &'a dyn Fn(&str),
    /// Writes to stderr at once, for a notice that precedes the outcome.
    pub stderr: &'a dyn Fn(&str),
}

#[derive(Debug, PartialEq, Eq)]
pub enum Outcome {
    /// Print to stdout and exit 0.
    Stdout(String),
    /// Print to stderr and exit 1.
    Failure(String),
    /// Print to stderr and exit 0, which is what an unknown help topic does.
    StderrOk(String),
}

fn flag_arg(flag: &Flag) -> Arg {
    let mut arg = Arg::new(flag.name).long(flag.name);
    if let Some(short) = flag.shorthand {
        arg = arg.short(short);
    }
    if flag.takes_value() {
        arg.num_args(1).action(ArgAction::Set)
    } else {
        arg.num_args(0).action(ArgAction::SetTrue)
    }
}

fn config_command() -> Command {
    let mut command = Command::new("config")
        .disable_help_flag(true)
        .disable_version_flag(true);
    for flag in crate::flags::config_flags() {
        command = command.arg(flag_arg(&flag));
    }
    command.arg(Arg::new("args").num_args(0..).action(ArgAction::Append))
}

/// A subcommand with only a help flag of its own. Stray arguments are
/// collected: `whoami` rejects them with Cobra's NoArgs message, and the
/// others accept them as Cobra's legacy argument check does.
fn plain_command(name: &'static str, help_usage: &'static str) -> Command {
    Command::new(name)
        .disable_help_flag(true)
        .disable_version_flag(true)
        .arg(flag_arg(&help::help_flag(help_usage)))
        .arg(Arg::new("args").num_args(0..).action(ArgAction::Append))
}

/// A subcommand with its own flags. Stray arguments are accepted, as Cobra's
/// legacy argument check does for a command without subcommands.
fn flagged_command(name: &'static str, flags: &[Flag]) -> Command {
    let mut command = Command::new(name)
        .disable_help_flag(true)
        .disable_version_flag(true);
    for flag in flags {
        command = command.arg(flag_arg(flag));
    }
    command.arg(Arg::new("args").num_args(0..).action(ArgAction::Append))
}

fn setup_command() -> Command {
    plain_command("setup", "help for setup")
        .subcommand(flagged_command("detect", &crate::setup::detect_flags()))
        .subcommand(flagged_command("install", &crate::setup::install_flags()))
        .subcommand(flagged_command("init", &crate::setup::init_flags()))
}

pub fn build_command(flags: &[Flag]) -> Command {
    let mut command = Command::new("ldcli")
        .disable_help_flag(true)
        .disable_version_flag(true)
        .disable_help_subcommand(true)
        .allow_external_subcommands(true);
    for flag in flags {
        command = command.arg(flag_arg(flag).global(true));
    }
    for flag in crate::flags::implicit_flags() {
        command = command.arg(flag_arg(&flag));
    }
    command
        .subcommand(config_command())
        .subcommand(plain_command("help", "help for help"))
        .subcommand(plain_command("login", "help for login"))
        .subcommand(plain_command("quickstart", "help for quickstart"))
        .subcommand(setup_command())
        .subcommand(plain_command("signup", "help for signup"))
        .subcommand(plain_command("whoami", "help for whoami"))
}

/// Only Find's answer says whether quickstart executed, and so printed its
/// deprecation notice, before a flag failed to parse.
fn names_quickstart(argv: &[String]) -> bool {
    matches!(cobra_find::find(argv), Found::Command(path, _) if path == ["quickstart"])
}

pub fn run(argv: &[String], version: &str, env: &Env<'_>) -> Outcome {
    let default_output = settings::default_output(env.stdout_is_terminal, env.var);
    let flags = persistent_flags(default_output);
    let command = build_command(&flags);

    let mut full_argv = vec!["ldcli".to_string()];
    full_argv.extend(argv.iter().cloned());

    let matches = match command.try_get_matches_from(full_argv) {
        Ok(matches) => matches,
        Err(err) => {
            let message = parse_error_message(&err, argv, &flags);
            if names_quickstart(argv) {
                return Outcome::Failure(format!("{}{message}", help::QUICKSTART_DEPRECATED));
            }
            return Outcome::Failure(message);
        }
    };

    // Cobra checks the help flag before the version flag, so `--help
    // --version` prints help.
    if matches.get_flag("help") {
        return Outcome::Stdout(help::help_string(default_output));
    }
    if matches.get_flag("version") {
        return Outcome::Stdout(help::version_line(version));
    }

    match matches.subcommand() {
        None => Outcome::Stdout(help::help_string(default_output)),
        Some(("config", sub)) => run_config(sub, version, env, default_output),
        Some(("login", sub)) => run_login(sub, version, env, default_output),
        Some(("signup", sub)) => run_signup(sub, env, default_output),
        Some(("whoami", sub)) => run_whoami(sub, version, env, default_output),
        Some(("help", sub)) => run_help(sub, default_output),
        Some(("quickstart", sub)) => {
            (env.stderr)(help::QUICKSTART_DEPRECATED);
            if sub.get_flag("help") {
                return Outcome::Stdout(help::quickstart_help(default_output));
            }
            Outcome::Failure(WIZARD_NOT_PORTED.replace("{}", "quickstart"))
        }
        Some(("setup", sub)) => run_setup(sub, env, default_output),
        Some((name, _)) => Outcome::Failure(format!("unknown command {name:?} for \"ldcli\"\n")),
    }
}

const WIZARD_NOT_PORTED: &str =
    "the {} wizard is not ported to the Rust ldcli; run it with the Go ldcli\n";

/// Cobra's help command parses its own flags, then asks Find which command
/// the remaining words name. Words after the deepest command are ignored.
fn run_help(sub: &ArgMatches, default_output: &'static str) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::help_help(default_output));
    }
    let topics: Vec<String> = sub
        .get_many::<String>("args")
        .into_iter()
        .flatten()
        .cloned()
        .collect();
    let found = match cobra_find::find(&topics) {
        Found::Command(path, _) => help::topic_help(&path, default_output),
        Found::Unknown => None,
    };
    match found {
        Some(text) => Outcome::Stdout(text),
        None => Outcome::StderrOk(format!(
            "{}{}",
            help::unknown_help_topic(&topics),
            help::usage_without_implicit_flags(default_output)
        )),
    }
}

fn run_setup(sub: &ArgMatches, env: &Env<'_>, default_output: &'static str) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::setup_help(default_output));
    }
    let lossy = |name: &str| (env.var)(name).map(|value| value.to_string_lossy().into_owned());
    let path = (env.var)("PATH");
    let virtual_env = lossy("VIRTUAL_ENV");
    let pwd = lossy("PWD");
    let machine = setup::Machine {
        path: path.as_deref(),
        virtual_env: virtual_env.as_deref(),
        pwd: pwd.as_deref(),
    };
    let string = |matches: &ArgMatches, name: &str| {
        matches.get_one::<String>(name).cloned().unwrap_or_default()
    };
    let output_kind = |matches: &ArgMatches| {
        let config_path = config::config_file(env.var).unwrap_or_default();
        let resolved = Resolved::new(matches, env, &config_path, default_output);
        settings::output_kind(matches.get_flag("json"), &resolved.output)
    };
    match sub.subcommand() {
        Some(("detect", sub)) if sub.get_flag("help") => {
            Outcome::Stdout(help::setup_detect_help(default_output))
        }
        Some(("detect", sub)) => setup::run_detect(&setup::DetectContext {
            path: &string(sub, "path"),
            json: output_kind(sub) == "json",
            machine: &machine,
        }),
        Some(("install", sub)) if sub.get_flag("help") => {
            Outcome::Stdout(help::setup_install_help(default_output))
        }
        Some(("install", sub)) if !sub.contains_id("sdk-id") => {
            Outcome::Failure("required flag(s) \"sdk-id\" not set\n".into())
        }
        Some(("install", sub)) => setup::run_install(
            &setup::InstallContext {
                path: &string(sub, "path"),
                sdk_id: &string(sub, "sdk-id"),
                package_manager: &string(sub, "package-manager"),
                dry_run: sub.get_flag("dry-run"),
                json: output_kind(sub) == "json",
                machine: &machine,
            },
            env.stderr,
        ),
        Some(("init", sub)) if sub.get_flag("help") => {
            Outcome::Stdout(help::setup_init_help(default_output))
        }
        Some((name, _)) => Outcome::Failure(format!(
            "ldcli setup {name} is not ported to the Rust ldcli yet\n"
        )),
        None => Outcome::Failure(WIZARD_NOT_PORTED.replace("{}", "setup")),
    }
}

fn run_config(
    sub: &ArgMatches,
    version: &str,
    env: &Env<'_>,
    default_output: &'static str,
) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::config_help(default_output));
    }
    let path = config::config_file(env.var).unwrap_or_default();
    let resolved = Resolved::new(sub, env, &path, default_output);
    let args = ConfigArgs {
        list: sub.get_flag("list"),
        set: sub.get_flag("set"),
        unset: sub.get_one::<String>("unset").cloned(),
        args: sub
            .get_many::<String>("args")
            .into_iter()
            .flatten()
            .cloned()
            .collect(),
    };
    config_cmd::run(
        &args,
        &ConfigContext {
            path: &path,
            output: &resolved.output,
            base_uri: &resolved.base_uri,
            version,
            help: help::config_help(default_output),
        },
    )
}

fn run_login(
    sub: &ArgMatches,
    version: &str,
    env: &Env<'_>,
    default_output: &'static str,
) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::login_help(default_output));
    }
    let path = config::config_file(env.var).unwrap_or_default();
    let resolved = Resolved::new(sub, env, &path, default_output);
    login::run(&LoginContext {
        config_path: &path,
        base_uri: &resolved.base_uri,
        version,
        path: (env.var)("PATH"),
        device_name: login::device_name(),
        interval: login::TOKEN_INTERVAL,
        max_attempts: login::MAX_FETCH_TOKEN_ATTEMPTS,
        stdout: env.stdout,
    })
}

fn run_signup(sub: &ArgMatches, env: &Env<'_>, default_output: &'static str) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::signup_help(default_output));
    }
    let path = config::config_file(env.var).unwrap_or_default();
    let resolved = Resolved::new(sub, env, &path, default_output);
    signup::run(&SignupContext {
        base_uri: &resolved.base_uri,
        path: (env.var)("PATH"),
        stdout: env.stdout,
    })
}

fn run_whoami(
    sub: &ArgMatches,
    version: &str,
    env: &Env<'_>,
    default_output: &'static str,
) -> Outcome {
    if sub.get_flag("help") {
        return Outcome::Stdout(help::whoami_help(default_output));
    }
    if let Some(extra) = sub
        .get_many::<String>("args")
        .and_then(|mut args| args.next())
    {
        return Outcome::Failure(format!("unknown command {extra:?} for \"ldcli whoami\"\n"));
    }
    let path = config::config_file(env.var).unwrap_or_default();
    let resolved = Resolved::new(sub, env, &path, default_output);
    let output = settings::output_kind(sub.get_flag("json"), &resolved.output);
    whoami::run(&WhoamiContext {
        access_token: &resolved.access_token,
        base_uri: &resolved.base_uri,
        output: &output,
        version,
    })
}

/// Settings resolved by precedence for one invocation.
struct Resolved {
    output: String,
    base_uri: String,
    access_token: String,
}

impl Resolved {
    fn new(matches: &ArgMatches, env: &Env<'_>, path: &PathBuf, default_output: &str) -> Self {
        let on_command_line: BTreeMap<String, String> = ["output", "base-uri", "access-token"]
            .into_iter()
            .filter(|name| matches.value_source(name) == Some(ValueSource::CommandLine))
            .filter_map(|name| {
                matches
                    .get_one::<String>(name)
                    .map(|value| (name.to_string(), value.clone()))
            })
            .collect();
        let file = config::load(Some(path));
        let resolve = |name: &str, default: &str| {
            settings::resolve(name, &on_command_line, env.var, &file, default).value
        };
        Self {
            output: resolve("output", default_output),
            base_uri: resolve("base-uri", settings::BASE_URI_DEFAULT),
            access_token: resolve("access-token", ""),
        }
    }
}

/// Translate a clap parse failure into the pflag sentence Go prints.
///
/// clap always names the long form of a flag. pflag names whichever form the
/// user typed, so `-o` with no value reports the shorthand.
fn parse_error_message(err: &clap::Error, argv: &[String], flags: &[Flag]) -> String {
    let invalid_arg = match err.get(ContextKind::InvalidArg) {
        Some(ContextValue::String(value)) => value.clone(),
        Some(ContextValue::Strings(values)) => values.first().cloned().unwrap_or_default(),
        _ => String::new(),
    };
    // clap names a flag that takes a value as `--output <output>`; Go prints
    // the flag alone.
    let invalid_arg = match invalid_arg.split_once(" <") {
        Some((name, _)) => name.to_string(),
        None => invalid_arg,
    };
    match err.kind() {
        ErrorKind::UnknownArgument => match short_flag(&invalid_arg) {
            Some(short) => format!("unknown shorthand flag: '{short}' in -{short}\n"),
            None => format!("unknown flag: {invalid_arg}\n"),
        },
        _ if invalid_arg.is_empty() => format!("{err}\n"),
        _ => match typed_shorthand(&invalid_arg, argv, flags) {
            Some(short) => format!("flag needs an argument: '{short}' in -{short}\n"),
            None => format!("flag needs an argument: {invalid_arg}\n"),
        },
    }
}

/// Did the user write this flag's shorthand rather than its long name?
fn typed_shorthand(invalid_arg: &str, argv: &[String], flags: &[Flag]) -> Option<char> {
    if let Some(short) = short_flag(invalid_arg) {
        return Some(short);
    }
    let name = invalid_arg.strip_prefix("--")?;
    let short = flags
        .iter()
        .find(|flag| flag.name == name)
        .and_then(|flag| flag.shorthand)?;
    // A shorthand token is a single dash, so `--output` cannot match here.
    let typed = argv.iter().any(|arg| {
        arg.starts_with('-') && !arg.starts_with("--") && arg.chars().skip(1).any(|c| c == short)
    });
    typed.then_some(short)
}

/// `-x` is a shorthand; `--x` is not.
fn short_flag(name: &str) -> Option<char> {
    let rest = name.strip_prefix('-')?;
    if rest.starts_with('-') {
        return None;
    }
    let mut chars = rest.chars();
    let first = chars.next()?;
    chars.next().is_none().then_some(first)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn no_env(_: &str) -> Option<OsString> {
        None
    }

    fn env() -> Env<'static> {
        Env {
            var: &no_env,
            stdout_is_terminal: false,
            stdout: &|_| {},
            stderr: &|_| {},
        }
    }

    fn run_argv(args: &[&str]) -> Outcome {
        let argv: Vec<String> = args.iter().map(|a| (*a).to_string()).collect();
        run(&argv, "test", &env())
    }

    fn failure(args: &[&str]) -> String {
        match run_argv(args) {
            Outcome::Failure(message) => message,
            other => panic!("{args:?}: {other:?}"),
        }
    }

    #[test]
    fn help_and_a_bare_invocation_print_the_same_text() {
        let bare = run_argv(&[]);
        assert_eq!(bare, run_argv(&["--help"]));
        assert_eq!(bare, run_argv(&["-h"]));
        assert_eq!(bare, run_argv(&["help"]));
        match bare {
            Outcome::Stdout(text) => assert!(text.starts_with("LaunchDarkly CLI")),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn version_prints_the_injected_version() {
        assert_eq!(
            run_argv(&["--version"]),
            Outcome::Stdout("ldcli version test\n".to_string())
        );
        assert_eq!(run_argv(&["-v"]), run_argv(&["--version"]));
    }

    #[test]
    fn help_wins_over_version() {
        assert_eq!(run_argv(&["--help", "--version"]), run_argv(&["--help"]));
        assert_eq!(run_argv(&["--version", "--help"]), run_argv(&["--help"]));
    }

    #[test]
    fn root_flags_before_a_subcommand_still_act_on_the_root() {
        assert_eq!(run_argv(&["--help", "config"]), run_argv(&["--help"]));
        assert_eq!(run_argv(&["-v", "config"]), run_argv(&["--version"]));
        assert_eq!(
            failure(&["config", "-v"]),
            "unknown shorthand flag: 'v' in -v\n"
        );
    }

    #[test]
    fn config_help_is_reachable_three_ways() {
        let help = run_argv(&["config", "--help"]);
        assert_eq!(help, run_argv(&["config", "-h"]));
        assert_eq!(help, run_argv(&["help", "config"]));
        match help {
            Outcome::Stdout(text) => {
                assert!(text.starts_with("View and modify specific configuration values\n"));
                assert!(text.contains("\nGlobal flags:\n"));
            }
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn an_unknown_command_fails_without_printing_usage() {
        let message = failure(&["not-a-command"]);
        assert_eq!(message, "unknown command \"not-a-command\" for \"ldcli\"\n");
        assert!(!message.contains("Usage:"));
    }

    #[test]
    fn an_unknown_flag_reports_the_flag_it_saw() {
        assert_eq!(failure(&["--nope"]), "unknown flag: --nope\n");
        assert_eq!(failure(&["-x"]), "unknown shorthand flag: 'x' in -x\n");
        assert_eq!(failure(&["config", "--nope"]), "unknown flag: --nope\n");
    }

    #[test]
    fn a_flag_missing_its_value_is_named_the_way_it_was_typed() {
        assert_eq!(failure(&["--output"]), "flag needs an argument: --output\n");
        assert_eq!(failure(&["-o"]), "flag needs an argument: 'o' in -o\n");
        assert_eq!(
            failure(&["--access-token"]),
            "flag needs an argument: --access-token\n"
        );
        assert_eq!(
            failure(&["config", "--unset"]),
            "flag needs an argument: --unset\n"
        );
    }

    #[test]
    fn an_unknown_help_topic_exits_zero_on_stderr() {
        match run_argv(&["help", "not-real"]) {
            Outcome::StderrOk(text) => {
                assert!(text.starts_with("Unknown help topic [`not-real`]\nUsage:\n"));
                assert!(!text.contains("help for ldcli"));
            }
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_help_topic_ignores_words_after_the_deepest_command_it_names() {
        assert_eq!(
            run_argv(&["help", "setup", "nope"]),
            run_argv(&["setup", "-h"])
        );
        assert_eq!(
            run_argv(&["help", "setup", "detect", "x"]),
            run_argv(&["setup", "detect", "--help"])
        );
        assert_eq!(
            run_argv(&["help", "login", "extra"]),
            run_argv(&["login", "-h"])
        );
        assert_eq!(
            run_argv(&["help", "--json", "config"]),
            run_argv(&["config", "-h"])
        );
        assert_eq!(
            run_argv(&["help", "help", "setup"]),
            run_argv(&["help", "-h"])
        );
    }

    #[test]
    fn help_parses_its_own_flags_before_it_reads_the_topic() {
        assert_eq!(
            failure(&["help", "--output"]),
            "flag needs an argument: --output\n"
        );
        assert_eq!(failure(&["help", "--nope"]), "unknown flag: --nope\n");
        match run_argv(&["help", "nope", "--json"]) {
            Outcome::StderrOk(text) => {
                assert!(text.starts_with("Unknown help topic [`nope`]\n"), "{text}")
            }
            other => panic!("{other:?}"),
        }
    }

    fn run_with_stderr(args: &[&str]) -> (Outcome, String) {
        let argv: Vec<String> = args.iter().map(|a| (*a).to_string()).collect();
        let stderr = std::cell::RefCell::new(String::new());
        let write = |text: &str| stderr.borrow_mut().push_str(text);
        let outcome = run(
            &argv,
            "test",
            &Env {
                var: &no_env,
                stdout_is_terminal: false,
                stdout: &|_| {},
                stderr: &write,
            },
        );
        (outcome, stderr.into_inner())
    }

    #[test]
    fn quickstart_help_carries_the_deprecation_notice_only_when_quickstart_executes() {
        let (outcome, stderr) = run_with_stderr(&["quickstart", "--help"]);
        assert_eq!(outcome, Outcome::Stdout(help::quickstart_help("json")));
        assert_eq!(stderr, help::QUICKSTART_DEPRECATED);

        let (outcome, stderr) = run_with_stderr(&["help", "quickstart"]);
        assert_eq!(outcome, Outcome::Stdout(help::quickstart_help("json")));
        assert_eq!(stderr, "");
    }

    #[test]
    fn a_quickstart_flag_error_follows_the_deprecation_notice_if_find_reached_quickstart() {
        assert_eq!(
            failure(&["--json", "quickstart", "--nope"]),
            format!("{}unknown flag: --nope\n", help::QUICKSTART_DEPRECATED)
        );
        assert_eq!(failure(&["--nope", "quickstart"]), "unknown flag: --nope\n");
    }

    #[test]
    fn a_terminal_changes_the_default_shown_in_help() {
        let argv = vec!["--help".to_string()];
        let on_terminal = run(
            &argv,
            "test",
            &Env {
                var: &no_env,
                stdout_is_terminal: true,
                stdout: &|_| {},
                stderr: &|_| {},
            },
        );
        match on_terminal {
            Outcome::Stdout(text) => assert!(text.contains(r#"(default "plaintext")"#), "{text}"),
            other => panic!("{other:?}"),
        }
        match run_argv(&["--help"]) {
            Outcome::Stdout(text) => assert!(text.contains(r#"(default "json")"#), "{text}"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn config_output_follows_flag_then_environment_then_file() {
        let dir = tempfile::tempdir().unwrap();
        let config_home = dir.path().to_path_buf();
        std::fs::create_dir_all(config_home.join("ldcli")).unwrap();
        std::fs::write(config_home.join("ldcli/config.yml"), "output: plaintext\n").unwrap();

        let run_with = |extra: Option<(&str, &str)>, args: &[&str]| {
            let home = config_home.clone();
            let lookup = move |name: &str| match name {
                "XDG_CONFIG_HOME" => Some(OsString::from(home.clone())),
                _ => extra
                    .filter(|(key, _)| *key == name)
                    .map(|(_, value)| OsString::from(value)),
            };
            let argv: Vec<String> = args.iter().map(|a| (*a).to_string()).collect();
            run(
                &argv,
                "test",
                &Env {
                    var: &lookup,
                    stdout_is_terminal: false,
                    stdout: &|_| {},
                    stderr: &|_| {},
                },
            )
        };

        // The file says plaintext.
        assert_eq!(
            run_with(None, &["config", "--list"]),
            Outcome::Stdout("output: plaintext\n".into())
        );
        // LD_OUTPUT beats the file, and LD_JSON is not a setting at all.
        assert_eq!(
            run_with(Some(("LD_OUTPUT", "json")), &["config", "--list"]),
            Outcome::Stdout("{\"output\":\"plaintext\"}\n".into())
        );
        assert_eq!(
            run_with(Some(("LD_JSON", "true")), &["config", "--list"]),
            Outcome::Stdout("output: plaintext\n".into())
        );
        // A flag beats both, before or after the subcommand, and --json is
        // ignored by config.
        assert_eq!(
            run_with(
                Some(("LD_OUTPUT", "plaintext")),
                &["config", "--list", "-o", "json"]
            ),
            Outcome::Stdout("{\"output\":\"plaintext\"}\n".into())
        );
        assert_eq!(
            run_with(None, &["-o", "json", "config", "--list"]),
            Outcome::Stdout("{\"output\":\"plaintext\"}\n".into())
        );
        assert_eq!(
            run_with(None, &["config", "--list", "--json"]),
            Outcome::Stdout("output: plaintext\n".into())
        );
    }
}
