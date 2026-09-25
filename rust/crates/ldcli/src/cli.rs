//! Argv parsing and the exit codes that go with it.
//!
//! clap parses; the messages and exit codes are Cobra's. Cobra sets
//! `SilenceErrors` and `SilenceUsage` and then prints the error itself before
//! `os.Exit(1)`, so a usage error prints one line to stderr with no usage
//! block, and exits 1 rather than clap's 2.

use crate::flags::{persistent_flags, Flag};
use crate::help;
use crate::settings;
use clap::error::{ContextKind, ContextValue, ErrorKind};
use clap::{Arg, ArgAction, Command};
use std::ffi::OsString;

pub struct Env<'a> {
    pub var: &'a dyn Fn(&str) -> Option<OsString>,
    pub stdout_is_terminal: bool,
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

pub fn build_command(flags: &[Flag]) -> Command {
    let mut command = Command::new("ldcli")
        .disable_help_flag(true)
        .disable_version_flag(true)
        .disable_help_subcommand(true)
        .allow_external_subcommands(true);

    for flag in flags {
        let mut arg = Arg::new(flag.name).long(flag.name);
        if let Some(short) = flag.shorthand {
            arg = arg.short(short);
        }
        arg = if flag.takes_value() {
            arg.num_args(1).action(ArgAction::Set)
        } else {
            arg.num_args(0).action(ArgAction::SetTrue)
        };
        command = command.arg(arg);
    }
    command
        .arg(
            Arg::new("help")
                .long("help")
                .short('h')
                .num_args(0)
                .action(ArgAction::SetTrue),
        )
        .arg(
            Arg::new("version")
                .long("version")
                .short('v')
                .num_args(0)
                .action(ArgAction::SetTrue),
        )
}

pub fn run(argv: &[String], version: &str, env: &Env<'_>) -> Outcome {
    let default_output = settings::default_output(env.stdout_is_terminal, env.var);
    let flags = persistent_flags(default_output);
    let command = build_command(&flags);

    let mut full_argv = vec!["ldcli".to_string()];
    full_argv.extend(argv.iter().cloned());

    let matches = match command.try_get_matches_from(full_argv) {
        Ok(matches) => matches,
        Err(err) => return Outcome::Failure(parse_error_message(&err, argv, &flags)),
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
        Some(("help", sub)) => {
            let topics = external_args(sub);
            if topics.is_empty() {
                Outcome::Stdout(help::help_string(default_output))
            } else {
                Outcome::StderrOk(format!(
                    "{}{}",
                    help::unknown_help_topic(&topics),
                    help::usage_without_implicit_flags(default_output)
                ))
            }
        }
        Some((name, _)) => Outcome::Failure(format!("unknown command {name:?} for \"ldcli\"\n")),
    }
}

/// An external subcommand's trailing words arrive under the empty argument id.
fn external_args(matches: &clap::ArgMatches) -> Vec<String> {
    matches
        .get_many::<OsString>("")
        .into_iter()
        .flatten()
        .map(|value| value.to_string_lossy().into_owned())
        .collect()
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
    fn an_unknown_command_fails_without_printing_usage() {
        let message = failure(&["not-a-command"]);
        assert_eq!(message, "unknown command \"not-a-command\" for \"ldcli\"\n");
        assert!(!message.contains("Usage:"));
    }

    #[test]
    fn an_unknown_flag_reports_the_flag_it_saw() {
        assert_eq!(failure(&["--nope"]), "unknown flag: --nope\n");
        assert_eq!(failure(&["-x"]), "unknown shorthand flag: 'x' in -x\n");
    }

    #[test]
    fn a_flag_missing_its_value_is_named_the_way_it_was_typed() {
        assert_eq!(failure(&["--output"]), "flag needs an argument: --output\n");
        assert_eq!(failure(&["-o"]), "flag needs an argument: 'o' in -o\n");
        assert_eq!(
            failure(&["--access-token"]),
            "flag needs an argument: --access-token\n"
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
    fn a_terminal_changes_the_default_shown_in_help() {
        let argv = vec!["--help".to_string()];
        let on_terminal = run(
            &argv,
            "test",
            &Env {
                var: &no_env,
                stdout_is_terminal: true,
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
}
