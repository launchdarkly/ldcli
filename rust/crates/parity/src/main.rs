use anyhow::{anyhow, Result};
use parity::corpus::{
    capture_tree, check_tree, coverage_report, reject_rust_capture, run_exit0_substitutes,
    scan_parity, seed_missing_help, CaseFailure,
};
use parity::coverage::{load_command_list, load_exemptions};
use parity::secrets::Minted;
use std::env;
use std::path::PathBuf;
use std::process::ExitCode;
use std::time::{SystemTime, UNIX_EPOCH};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("parity: {err:#}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<()> {
    let mut args = env::args().skip(1);
    let command = args
        .next()
        .ok_or_else(|| anyhow!("usage: parity check|capture ..."))?;
    let opts = Options::parse(args)?;
    match command.as_str() {
        "check" => cmd_check(&opts),
        "capture" => cmd_capture(&opts),
        other => Err(anyhow!(
            "unknown command {other}; expected check or capture"
        )),
    }
}

struct Options {
    go_bin: PathBuf,
    rust_bin: Option<PathBuf>,
    cases: PathBuf,
    fixtures: PathBuf,
    exemptions: PathBuf,
    commands: PathBuf,
    seed_missing_help: bool,
}

impl Options {
    fn parse(args: impl Iterator<Item = String>) -> Result<Self> {
        let mut go_bin = None;
        let mut rust_bin = None;
        let mut cases = None;
        let mut fixtures = None;
        let mut exemptions = None;
        let mut commands = None;
        let mut seed_missing_help = false;
        let mut rest = args;
        while let Some(arg) = rest.next() {
            match arg.as_str() {
                "--seed-missing-help" => seed_missing_help = true,
                "--go-bin" => go_bin = Some(need(&mut rest, "--go-bin")?),
                "--rust-bin" => rust_bin = Some(need(&mut rest, "--rust-bin")?),
                "--cases" => cases = Some(need(&mut rest, "--cases")?),
                "--fixtures" => fixtures = Some(need(&mut rest, "--fixtures")?),
                "--exemptions" => exemptions = Some(need(&mut rest, "--exemptions")?),
                "--commands" => commands = Some(need(&mut rest, "--commands")?),
                other => return Err(anyhow!("unknown argument {other}")),
            }
        }
        let cases = PathBuf::from(cases.ok_or_else(|| anyhow!("--cases is required"))?);
        let fixtures = fixtures
            .map(PathBuf::from)
            .unwrap_or_else(|| cases.join("../fixtures"));
        Ok(Self {
            go_bin: PathBuf::from(go_bin.ok_or_else(|| anyhow!("--go-bin is required"))?),
            rust_bin: rust_bin.map(PathBuf::from),
            cases,
            fixtures,
            exemptions: PathBuf::from(
                exemptions.unwrap_or_else(|| "parity/exemptions.toml".into()),
            ),
            commands: PathBuf::from(commands.ok_or_else(|| anyhow!("--commands is required"))?),
            seed_missing_help,
        })
    }
}

fn need(args: &mut impl Iterator<Item = String>, flag: &str) -> Result<String> {
    args.next().ok_or_else(|| anyhow!("{flag} needs a value"))
}

fn mint() -> Minted {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    Minted::new(&format!("{nanos:x}"))
}

fn cmd_capture(opts: &Options) -> Result<()> {
    reject_rust_capture(opts.rust_bin.as_deref())?;
    if opts.seed_missing_help {
        let commands = load_command_list(&opts.commands)?;
        let created = seed_missing_help(&commands, &opts.cases)?;
        eprintln!("seeded {} help cases", created.len());
    }
    let minted = mint();
    capture_tree(&opts.go_bin, &opts.cases, &minted)?;
    scan_parity(&opts.cases, &opts.fixtures, &minted)?;
    println!("captured cases under {}", opts.cases.display());
    Ok(())
}

fn cmd_check(opts: &Options) -> Result<()> {
    let minted = mint();
    let failures = check_tree(&opts.go_bin, opts.rust_bin.as_deref(), &opts.cases, &minted)?;
    let exemptions = load_exemptions(&opts.exemptions)?;
    let mut substitute_failures = run_exit0_substitutes(&opts.go_bin, &exemptions, false)?;
    if let Some(rust_bin) = &opts.rust_bin {
        substitute_failures.extend(run_exit0_substitutes(rust_bin, &exemptions, true)?);
    }
    let coverage = coverage_report(&opts.commands, &opts.cases, &opts.exemptions)?;
    scan_parity(&opts.cases, &opts.fixtures, &minted)?;
    // The report prints whether or not the gate passed, so a reviewer reading a
    // failure still sees each command's state beside the uncovered ones.
    for line in &coverage.report {
        println!("{line}");
    }
    print_failures(&failures);
    print_failures(&substitute_failures);
    if !coverage.passed() || !failures.is_empty() || !substitute_failures.is_empty() {
        for failure in &coverage.failures {
            println!("{}", failure.message);
        }
        return Err(anyhow!(
            "{} case failure(s), {} substitute failure(s), {} uncovered command(s)",
            failures.len(),
            substitute_failures.len(),
            coverage.failures.len()
        ));
    }
    println!(
        "ok: {} covered or exempt, cases matched",
        coverage.report.len()
    );
    Ok(())
}

fn print_failures(failures: &[CaseFailure]) {
    for failure in failures {
        println!("case {}", failure.id);
        println!("argv: {}", failure.argv.join(" "));
        println!("{}", failure.detail);
    }
}
