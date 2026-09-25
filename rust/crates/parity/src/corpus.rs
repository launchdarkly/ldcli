//! Capture Go transcripts and check them. Capture refuses a Rust source.

use crate::case::{
    help_argv, help_case_id, help_command, iter_cases, load_case, save_case, Case, Expectation,
};
use crate::coverage::{check_coverage, load_command_list, load_exemptions, Coverage, Exemption};
use crate::diffutil::unified_diff;
use crate::fixture::{render_requests, FixtureServer};
use crate::normalize::{normalize_stream, RedactionRules, Redactor};
use crate::sandbox::{run_command, RunRequest, RunResult};
use crate::secrets::{scan_authorization, scan_tree};
use anyhow::{anyhow, Result};
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug)]
pub struct CaseFailure {
    pub id: String,
    pub argv: Vec<String>,
    pub detail: String,
}

/// Capture records expectations from the Go oracle. A Rust binary on the
/// capture path would let a Rust bug become the expected output, so the
/// binary the caller actually passed is what this rejects.
pub fn reject_rust_capture(rust_bin: Option<&Path>) -> Result<()> {
    match rust_bin {
        None => Ok(()),
        Some(path) => Err(anyhow!(
            "capture refuses to record from {}; expectations come from the Go binary only",
            path.display()
        )),
    }
}

pub fn hostname() -> Option<String> {
    fs::read_to_string("/etc/hostname")
        .ok()
        .map(|text| text.trim().to_string())
        .filter(|text| !text.is_empty())
}

pub fn redactor_for(sandbox_root: &Path, secrets: &Redactor, rules: RedactionRules) -> Redactor {
    Redactor {
        access_token: secrets.access_token.clone(),
        device_code: secrets.device_code.clone(),
        user_code: secrets.user_code.clone(),
        verification_uri: secrets.verification_uri.clone(),
        sandbox_root: Some(sandbox_root.display().to_string()),
        base_uri: secrets.base_uri.clone(),
        // Reading the machine name matters only to a case that asked to mask
        // it. Otherwise a hostname that happens to be an ordinary word would
        // rewrite that word wherever the CLI printed it.
        hostname: if rules.hostname {
            secrets.hostname.clone().or_else(hostname)
        } else {
            None
        },
        rules,
    }
}

fn base_secrets(token: &str) -> Redactor {
    Redactor {
        access_token: Some(token.to_string()),
        ..Redactor::default()
    }
}

/// One binary's run of a case: what it did, how to normalize it, and the
/// requests it made to the fixture server.
pub struct Execution {
    pub result: RunResult,
    pub redactor: Redactor,
    pub requests: String,
}

const BASE_URI_PLACEHOLDER: &str = "{{BASE_URI}}";
const ACCESS_TOKEN_PLACEHOLDER: &str = "{{ACCESS_TOKEN}}";

fn needs_server(case: &Case) -> bool {
    let named = |text: &String| text.contains(BASE_URI_PLACEHOLDER);
    !case.http.is_empty()
        || case.argv.iter().any(named)
        || case.env.values().any(named)
        || case.seed.values().any(named)
}

pub fn execute_case(bin: &Path, case: &Case, secrets: &Redactor) -> Result<Execution> {
    let rules =
        RedactionRules::parse(&case.redact).map_err(|err| anyhow!("case {}: {err}", case.id))?;
    let server = if needs_server(case) {
        Some(FixtureServer::start(case.http.clone())?)
    } else {
        None
    };
    let base_uri = server.as_ref().map(|server| server.base_uri.clone());
    let token = secrets.access_token.clone().unwrap_or_default();
    let fill = |text: &String| {
        let text = text.replace(ACCESS_TOKEN_PLACEHOLDER, &token);
        match &base_uri {
            Some(base) => text.replace(BASE_URI_PLACEHOLDER, base),
            None => text,
        }
    };
    let argv: Vec<String> = case.argv.iter().map(fill).collect();
    let env: BTreeMap<String, String> = case
        .env
        .iter()
        .map(|(key, value)| (key.clone(), fill(value)))
        .collect();
    let seed: BTreeMap<String, String> = case
        .seed
        .iter()
        .map(|(key, value)| (key.clone(), fill(value)))
        .collect();

    let run = run_command(&RunRequest {
        bin,
        argv: &argv,
        declare: &case.declare,
        extra_env: &env,
        seed: &seed,
        opt_out_update_check: true,
    });
    // Stop the server whether or not the run succeeded.
    let requests = server
        .map(|server| render_requests(&server.finish()))
        .unwrap_or_default();
    let result = run?;
    let mut redactor = redactor_for(&result.sandbox_root, secrets, rules);
    redactor.base_uri = base_uri;
    let requests = normalize_stream(&requests, &redactor);
    Ok(Execution {
        result,
        redactor,
        requests,
    })
}

fn normalized_output(
    result: &RunResult,
    redactor: &Redactor,
) -> (String, String, BTreeMap<String, String>) {
    let stdout = normalize_stream(&result.stdout, redactor);
    let stderr = normalize_stream(&result.stderr, redactor);
    let files = result
        .files
        .iter()
        .map(|(key, value)| (key.clone(), normalize_stream(value, redactor)))
        .collect();
    (stdout, stderr, files)
}

pub fn capture_case(go_bin: &Path, case: &Case, token: &str) -> Result<Case> {
    let secrets = base_secrets(token);
    let Execution {
        result,
        redactor,
        requests,
    } = execute_case(go_bin, case, &secrets)?;
    if !result.undeclared.is_empty() {
        return Err(anyhow!(
            "case {} wrote undeclared paths: {}",
            case.id,
            result.undeclared.join(", ")
        ));
    }
    let (stdout, stderr, files) = normalized_output(&result, &redactor);
    for (label, text) in [
        ("stdout", &stdout),
        ("stderr", &stderr),
        ("requests", &requests),
    ] {
        if token_leaks(text, token) {
            return Err(anyhow!(
                "case {} {label} still contains the harness access token after substitution",
                case.id
            ));
        }
    }
    for (key, text) in &files {
        if token_leaks(text, token) {
            return Err(anyhow!(
                "case {} file {key} still contains the harness access token after substitution",
                case.id
            ));
        }
    }
    Ok(Case {
        expect: Expectation {
            status: result.status,
            stdout,
            stderr,
            files,
            requests,
        },
        ..case.clone()
    })
}

fn token_leaks(text: &str, token: &str) -> bool {
    !token.is_empty() && text.contains(token)
}

pub fn check_case(
    go_bin: &Path,
    rust_bin: Option<&Path>,
    case: &Case,
    token: &str,
) -> Result<Vec<CaseFailure>> {
    let secrets = base_secrets(token);
    let Execution {
        result: go_result,
        redactor: go_redactor,
        requests: go_requests,
    } = execute_case(go_bin, case, &secrets)?;
    let mut failures = Vec::new();
    if !go_result.undeclared.is_empty() {
        failures.push(failure(
            case,
            format!(
                "undeclared filesystem writes: {}",
                go_result.undeclared.join(", ")
            ),
        ));
        return Ok(failures);
    }
    let (go_stdout, go_stderr, go_files) = normalized_output(&go_result, &go_redactor);
    let expected_redactor = redactor_for(Path::new(""), &secrets, go_redactor.rules);
    let expected_stdout = normalize_stream(&case.expect.stdout, &expected_redactor);
    let expected_stderr = normalize_stream(&case.expect.stderr, &expected_redactor);
    let expected_files: BTreeMap<String, String> = case
        .expect
        .files
        .iter()
        .map(|(key, value)| (key.clone(), normalize_stream(value, &expected_redactor)))
        .collect();

    if case.expect.status != go_result.status {
        failures.push(failure(
            case,
            format!(
                "status differs\n--- expected status\n+++ go status\n-{}\n+{}\n",
                case.expect.status, go_result.status
            ),
        ));
    }
    push_diff(
        &mut failures,
        case,
        "stdout",
        unified_diff("expected stdout", "go stdout", &expected_stdout, &go_stdout),
    );
    push_diff(
        &mut failures,
        case,
        "stderr",
        unified_diff("expected stderr", "go stderr", &expected_stderr, &go_stderr),
    );
    diff_files(
        &mut failures,
        case,
        "expected",
        "go",
        &expected_files,
        &go_files,
    );
    push_diff(
        &mut failures,
        case,
        "requests",
        unified_diff(
            "expected requests",
            "go requests",
            &normalize_stream(&case.expect.requests, &expected_redactor),
            &go_requests,
        ),
    );

    if !case.rust {
        return Ok(failures);
    }
    let Some(rust_bin) = rust_bin else {
        failures.push(failure(
            case,
            "case opts into the Rust binary, but no Rust binary was given".into(),
        ));
        return Ok(failures);
    };
    let Execution {
        result: rust_result,
        redactor: rust_redactor,
        requests: rust_requests,
    } = execute_case(rust_bin, case, &secrets)?;
    if !rust_result.undeclared.is_empty() {
        failures.push(failure(
            case,
            format!(
                "rust undeclared filesystem writes: {}",
                rust_result.undeclared.join(", ")
            ),
        ));
        return Ok(failures);
    }
    let (rust_stdout, rust_stderr, rust_files) = normalized_output(&rust_result, &rust_redactor);
    if go_result.status != rust_result.status {
        failures.push(failure(
            case,
            format!(
                "status differs\n--- go status\n+++ rust status\n-{}\n+{}\n",
                go_result.status, rust_result.status
            ),
        ));
    }
    push_diff(
        &mut failures,
        case,
        "stdout",
        unified_diff("go stdout", "rust stdout", &go_stdout, &rust_stdout),
    );
    push_diff(
        &mut failures,
        case,
        "stderr",
        unified_diff("go stderr", "rust stderr", &go_stderr, &rust_stderr),
    );
    diff_files(&mut failures, case, "go", "rust", &go_files, &rust_files);
    push_diff(
        &mut failures,
        case,
        "requests",
        unified_diff("go requests", "rust requests", &go_requests, &rust_requests),
    );
    Ok(failures)
}

fn failure(case: &Case, detail: String) -> CaseFailure {
    CaseFailure {
        id: case.id.clone(),
        argv: case.argv.clone(),
        detail,
    }
}

fn diff_files(
    failures: &mut Vec<CaseFailure>,
    case: &Case,
    left_label: &str,
    right_label: &str,
    left: &BTreeMap<String, String>,
    right: &BTreeMap<String, String>,
) {
    let keys: BTreeSet<&String> = left.keys().chain(right.keys()).collect();
    for key in keys {
        let l = left.get(key).map(String::as_str).unwrap_or("");
        let r = right.get(key).map(String::as_str).unwrap_or("");
        push_diff(
            failures,
            case,
            key,
            unified_diff(
                &format!("{left_label} {key}"),
                &format!("{right_label} {key}"),
                l,
                r,
            ),
        );
    }
}

fn push_diff(failures: &mut Vec<CaseFailure>, case: &Case, stream: &str, diff: Option<String>) {
    if let Some(diff) = diff {
        failures.push(failure(case, format!("{stream} differs\n{diff}")));
    }
}

pub fn help_commands_from_cases(cases_dir: &Path) -> Result<BTreeSet<String>> {
    let mut covered = BTreeSet::new();
    for path in iter_cases(cases_dir)? {
        let case = load_case(&path)?;
        if let Some(command) = help_command(&case.argv) {
            if path.components().any(|c| c.as_os_str() == "help") {
                covered.insert(command);
            }
        }
    }
    Ok(covered)
}

pub fn seed_missing_help(commands: &[String], cases_dir: &Path) -> Result<Vec<PathBuf>> {
    let help_dir = cases_dir.join("help");
    fs::create_dir_all(&help_dir).map_err(|err| anyhow!(err))?;
    let mut created = Vec::new();
    for command in commands {
        let id = help_case_id(command);
        let path = help_dir.join(format!("{id}.toml"));
        if path.exists() {
            continue;
        }
        let case = Case {
            id: id.clone(),
            argv: help_argv(command),
            declare: vec!["config:ldcli/config.yml".into()],
            env: BTreeMap::new(),
            seed: BTreeMap::new(),
            http: Vec::new(),
            redact: Vec::new(),
            rust: false,
            expect: Expectation::default(),
        };
        save_case(&path, &case)?;
        created.push(path);
    }
    Ok(created)
}

pub fn capture_tree(go_bin: &Path, cases_dir: &Path, token: &str) -> Result<()> {
    let paths = iter_cases(cases_dir)?;
    let total = paths.len();
    for (index, path) in paths.iter().enumerate() {
        let case = load_case(path)?;
        if index % 25 == 0 {
            eprintln!("capture {}/{} {}", index + 1, total, case.id);
        }
        let captured = capture_case(go_bin, &case, token)?;
        save_case(path, &captured)?;
    }
    Ok(())
}

pub fn check_tree(
    go_bin: &Path,
    rust_bin: Option<&Path>,
    cases_dir: &Path,
    token: &str,
) -> Result<Vec<CaseFailure>> {
    let mut failures = Vec::new();
    for path in iter_cases(cases_dir)? {
        let case = load_case(&path)?;
        failures.extend(check_case(go_bin, rust_bin, &case, token)?);
    }
    Ok(failures)
}

/// Run the `exit-0` substitutes. `opted_in_only` restricts the run to
/// exemptions that have opted into the Rust binary, the same way a case does.
pub fn run_exit0_substitutes(
    bin: &Path,
    exemptions: &[Exemption],
    opted_in_only: bool,
) -> Result<Vec<CaseFailure>> {
    let mut failures = Vec::new();
    let declare = vec!["config:ldcli/config.yml".to_string()];
    let empty = BTreeMap::new();
    for exemption in exemptions {
        if exemption.substitute != "exit-0" || (opted_in_only && !exemption.rust) {
            continue;
        }
        for command in &exemption.commands {
            let argv: Vec<String> = command
                .split_whitespace()
                .skip(1)
                .map(str::to_string)
                .collect();
            let result = run_command(&RunRequest {
                bin,
                argv: &argv,
                declare: &declare,
                extra_env: &empty,
                seed: &empty,
                opt_out_update_check: true,
            })?;
            // Script bytes are not compared. Exit status is the assertion.
            if result.status != 0 {
                failures.push(CaseFailure {
                    id: exemption.name.clone(),
                    argv: argv.clone(),
                    detail: format!("{command} exited {}, expected 0", result.status),
                });
            }
            if !result.undeclared.is_empty() {
                failures.push(CaseFailure {
                    id: exemption.name.clone(),
                    argv,
                    detail: format!(
                        "undeclared filesystem writes: {}",
                        result.undeclared.join(", ")
                    ),
                });
            }
        }
    }
    Ok(failures)
}

pub fn coverage_report(
    commands_file: &Path,
    cases_dir: &Path,
    exemptions_file: &Path,
) -> Result<Coverage> {
    let commands = load_command_list(commands_file)?;
    let exemptions = load_exemptions(exemptions_file)?;
    let help = help_commands_from_cases(cases_dir)?;
    Ok(check_coverage(&commands, &help, &exemptions))
}

pub fn scan_parity(cases_dir: &Path, fixtures_dir: &Path, token: &str) -> Result<()> {
    // Any minted token uses this prefix. A fresh check must still reject one
    // that capture wrote earlier.
    let secrets = vec![token.to_string(), "parity-token-".to_string()];
    let roots = [cases_dir, fixtures_dir];
    scan_tree(&roots, &secrets)?;
    scan_authorization(&roots)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn script(dir: &Path, body: &str) -> PathBuf {
        let path = dir.join("fake-ldcli");
        fs::write(&path, body).unwrap();
        let mut perms = fs::metadata(&path).unwrap().permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&path, perms).unwrap();
        path
    }

    const WRITE_CONFIG: &str =
        "mkdir -p \"$XDG_CONFIG_HOME/ldcli\"\nprintf '{}\\n' > \"$XDG_CONFIG_HOME/ldcli/config.yml\"\n";

    fn help_case(stdout: &str) -> Case {
        Case {
            id: "ldcli".into(),
            argv: vec!["--help".into()],
            declare: vec!["config:ldcli/config.yml".into()],
            env: BTreeMap::new(),
            seed: BTreeMap::new(),
            http: Vec::new(),
            redact: Vec::new(),
            rust: false,
            expect: Expectation {
                status: 0,
                stdout: stdout.into(),
                stderr: String::new(),
                files: BTreeMap::from([("config:ldcli/config.yml".into(), "{}\n".into())]),
                requests: String::new(),
            },
        }
    }

    #[test]
    fn capture_then_check_matches() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf 'Usage:\\n  ldcli [command]\\n'\n"),
        );
        let captured = capture_case(&bin, &help_case(""), "parity-token-unused").unwrap();
        let failures = check_case(&bin, None, &captured, "parity-token-unused").unwrap();
        assert!(failures.is_empty(), "{failures:?}");
    }

    #[test]
    fn changed_flag_line_fails_with_a_unified_diff() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf 'Flags:\\n      --beta    added\\n'\n"),
        );
        let case = help_case("Flags:\n      --alpha   removed\n");
        let failures = check_case(&bin, None, &case, "parity-token-unused").unwrap();
        let detail = failures
            .iter()
            .map(|f| f.detail.clone())
            .collect::<Vec<_>>()
            .join("\n");
        assert!(detail.contains("--alpha"), "{detail}");
        assert!(detail.contains("--beta"), "{detail}");
        assert!(detail.contains("--- expected stdout"), "{detail}");
    }

    #[test]
    fn json_key_order_matches_and_an_extra_key_does_not() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf '%s\\n' '{{\"b\":1,\"a\":2}}'\n"),
        );
        let mut case = help_case("{\"a\":2,\"b\":1}\n");
        case.argv = vec!["--output".into(), "json".into()];
        let failures = check_case(&bin, None, &case, "parity-token-unused").unwrap();
        assert!(failures.is_empty(), "{failures:?}");

        case.expect.stdout = "{\"a\":2,\"b\":1,\"c\":3}\n".into();
        let failures = check_case(&bin, None, &case, "parity-token-unused").unwrap();
        assert!(!failures.is_empty(), "extra key should fail");
    }

    #[test]
    fn undeclared_config_write_fails_the_case() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf 'ok\\n'\n"),
        );
        let mut case = help_case("ok\n");
        case.declare.clear();
        case.expect.files.clear();
        let failures = check_case(&bin, None, &case, "parity-token-unused").unwrap();
        assert!(
            failures
                .iter()
                .any(|f| f.detail.contains("config:ldcli/config.yml")),
            "{failures:?}"
        );
    }

    #[test]
    fn capture_refuses_a_rust_binary_and_accepts_its_absence() {
        let err = reject_rust_capture(Some(Path::new("rust/target/debug/ldcli"))).unwrap_err();
        assert!(err.to_string().contains("Go binary"), "{err}");
        assert!(err.to_string().contains("rust/target/debug/ldcli"), "{err}");
        reject_rust_capture(None).unwrap();
    }

    fn completion_exemption(rust: bool) -> Exemption {
        Exemption {
            name: "completion-scripts".into(),
            kind: "command".into(),
            commands: vec!["ldcli completion bash".into()],
            reason: "script bytes are not diffed".into(),
            substitute: "exit-0".into(),
            rust,
        }
    }

    #[test]
    fn completion_exit_zero_ignores_script_bytes() {
        let go_dir = tempfile::tempdir().unwrap();
        let go_bin = script(
            go_dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf 'go-script\\n'\nexit 0\n"),
        );
        let rust_dir = tempfile::tempdir().unwrap();
        let rust_bin = script(
            rust_dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}printf 'rust-script-different\\n'\nexit 0\n"),
        );
        let exemptions = vec![completion_exemption(true)];
        assert!(run_exit0_substitutes(&go_bin, &exemptions, false)
            .unwrap()
            .is_empty());
        assert!(run_exit0_substitutes(&rust_bin, &exemptions, true)
            .unwrap()
            .is_empty());

        let failing_dir = tempfile::tempdir().unwrap();
        let failing = script(
            failing_dir.path(),
            &format!("#!/bin/sh\n{WRITE_CONFIG}exit 1\n"),
        );
        assert!(!run_exit0_substitutes(&failing, &exemptions, false)
            .unwrap()
            .is_empty());
    }

    #[test]
    fn a_substitute_runs_against_rust_only_after_it_opts_in() {
        let dir = tempfile::tempdir().unwrap();
        let rust_bin = script(dir.path(), "#!/bin/sh\nexit 1\n");
        // The Rust binary does not have the command yet, so it is skipped.
        assert!(
            run_exit0_substitutes(&rust_bin, &[completion_exemption(false)], true)
                .unwrap()
                .is_empty()
        );
        assert!(
            !run_exit0_substitutes(&rust_bin, &[completion_exemption(true)], true)
                .unwrap()
                .is_empty()
        );
    }
}
