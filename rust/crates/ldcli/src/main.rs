//! Entry point. Resolves the process environment and maps an outcome onto an
//! exit code; everything else lives in the library.

use ldcli::{cli, config};
use std::io::{IsTerminal, Write};
use std::process::ExitCode;

/// The Go build injects this with `-X main.version`; the Makefile sets the
/// same value here. An unset build reports `dev`, as Go's does.
const VERSION: &str = match option_env!("LDCLI_VERSION") {
    Some(version) => version,
    None => "dev",
};

fn main() -> ExitCode {
    let argv: Vec<String> = std::env::args().skip(1).collect();
    let var = |name: &str| std::env::var_os(name);
    let stdout = |text: &str| print(&mut std::io::stdout(), text);
    let env = cli::Env {
        var: &var,
        stdout_is_terminal: std::io::stdout().is_terminal(),
        stdout: &stdout,
    };

    // Go does this before it parses argv, so even a failed parse leaves the
    // file behind.
    if let Some(path) = config::config_file(&var) {
        config::ensure_file(&path);
    }

    match cli::run(&argv, VERSION, &env) {
        cli::Outcome::Stdout(text) => {
            print(&mut std::io::stdout(), &text);
            ExitCode::SUCCESS
        }
        cli::Outcome::StderrOk(text) => {
            print(&mut std::io::stderr(), &text);
            ExitCode::SUCCESS
        }
        cli::Outcome::Failure(text) => {
            print(&mut std::io::stderr(), &text);
            ExitCode::from(1)
        }
    }
}

/// A closed pipe is not an error worth reporting; Go's writes fail silently
/// here too.
fn print(out: &mut impl Write, text: &str) {
    let _ = out.write_all(text.as_bytes());
    let _ = out.flush();
}
