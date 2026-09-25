//! TOML cases. Stdout and stderr live beside the TOML so help transcripts
//! stay readable and are not escaped.

use crate::fixture::Route;
use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Case {
    pub id: String,
    pub argv: Vec<String>,
    #[serde(default)]
    pub declare: Vec<String>,
    #[serde(default)]
    pub env: BTreeMap<String, String>,
    /// Files written into the sandbox before the run, keyed like `declare`.
    #[serde(default)]
    pub seed: BTreeMap<String, String>,
    /// Routes the fixture server answers. A case that has any, or that names
    /// `{{BASE_URI}}`, runs each binary against its own server.
    #[serde(default)]
    pub http: Vec<Route>,
    /// Redaction rules this case needs, by name. Empty means the output is
    /// deterministic and every byte is compared.
    #[serde(default)]
    pub redact: Vec<String>,
    /// When true, check also runs the Rust binary. Help transcripts stay
    /// Go-only until the Rust command exists.
    #[serde(default)]
    pub rust: bool,
    #[serde(default)]
    pub expect: Expectation,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct Expectation {
    pub status: i32,
    #[serde(default)]
    pub stdout: String,
    #[serde(default)]
    pub stderr: String,
    #[serde(default)]
    pub files: BTreeMap<String, String>,
    /// The requests the fixture server received, in arrival order.
    #[serde(default)]
    pub requests: String,
}

#[derive(Debug, Deserialize)]
struct CaseFile {
    id: String,
    argv: Vec<String>,
    #[serde(default)]
    declare: Vec<String>,
    #[serde(default)]
    env: BTreeMap<String, String>,
    #[serde(default)]
    seed: BTreeMap<String, String>,
    #[serde(default)]
    http: Vec<Route>,
    #[serde(default)]
    redact: Vec<String>,
    #[serde(default)]
    rust: bool,
    #[serde(default)]
    expect: ExpectFile,
}

#[derive(Debug, Deserialize, Default)]
struct ExpectFile {
    #[serde(default)]
    status: i32,
    #[serde(default)]
    stdout: String,
    #[serde(default)]
    stderr: String,
    #[serde(default)]
    stdout_file: String,
    #[serde(default)]
    stderr_file: String,
    #[serde(default)]
    requests_file: String,
    #[serde(default)]
    files: BTreeMap<String, String>,
}

pub fn load_case(path: &Path) -> Result<Case> {
    let text = fs::read_to_string(path).map_err(|err| anyhow!("read {}: {err}", path.display()))?;
    let parsed: CaseFile =
        toml::from_str(&text).map_err(|err| anyhow!("parse {}: {err}", path.display()))?;
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    let stdout = read_stream(dir, &parsed.expect.stdout, &parsed.expect.stdout_file)?;
    let stderr = read_stream(dir, &parsed.expect.stderr, &parsed.expect.stderr_file)?;
    let requests = read_stream(dir, "", &parsed.expect.requests_file)?;
    Ok(Case {
        id: parsed.id,
        argv: parsed.argv,
        declare: parsed.declare,
        env: parsed.env,
        seed: parsed.seed,
        http: parsed.http,
        redact: parsed.redact,
        rust: parsed.rust,
        expect: Expectation {
            status: parsed.expect.status,
            stdout,
            stderr,
            files: parsed.expect.files,
            requests,
        },
    })
}

fn read_stream(dir: &Path, inline: &str, file_name: &str) -> Result<String> {
    if file_name.is_empty() {
        return Ok(inline.to_string());
    }
    let path = dir.join(file_name);
    fs::read_to_string(&path).map_err(|err| anyhow!("read {}: {err}", path.display()))
}

pub fn save_case(path: &Path, case: &Case) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|err| anyhow!(err))?;
    }
    let stem = path
        .file_stem()
        .and_then(|s| s.to_str())
        .ok_or_else(|| anyhow!("case path has no stem: {}", path.display()))?;
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    let stdout_name = format!("{stem}.stdout");
    fs::write(dir.join(&stdout_name), &case.expect.stdout)
        .map_err(|err| anyhow!("write stdout: {err}"))?;
    let stderr_name = write_optional(dir, stem, "stderr", &case.expect.stderr)?;
    let requests_name = write_optional(dir, stem, "requests", &case.expect.requests)?;

    let mut body = String::new();
    body.push_str(&format!("id = {}\n", toml_basic(&case.id)));
    body.push_str(&format!("argv = {}\n", toml_list(&case.argv)));
    body.push_str(&format!("declare = {}\n", toml_list(&case.declare)));
    if !case.redact.is_empty() {
        body.push_str(&format!("redact = {}\n", toml_list(&case.redact)));
    }
    if case.rust {
        body.push_str("rust = true\n");
    }
    if !case.env.is_empty() {
        body.push_str("\n[env]\n");
        for (key, value) in &case.env {
            body.push_str(&format!("{key} = {}\n", toml_basic(value)));
        }
    }
    if !case.seed.is_empty() {
        body.push_str("\n[seed]\n");
        for (key, value) in &case.seed {
            body.push_str(&format!("{} = {}\n", toml_basic(key), toml_basic(value)));
        }
    }
    for route in &case.http {
        body.push_str("\n[[http]]\n");
        body.push_str(&format!("method = {}\n", toml_basic(&route.method)));
        body.push_str(&format!("path = {}\n", toml_basic(&route.path)));
        body.push_str(&format!("status = {}\n", route.status));
        body.push_str(&format!("body = {}\n", toml_basic(&route.body)));
        for response in &route.then {
            body.push_str("\n[[http.then]]\n");
            body.push_str(&format!("status = {}\n", response.status));
            body.push_str(&format!("body = {}\n", toml_basic(&response.body)));
        }
    }
    body.push_str("\n[expect]\n");
    body.push_str(&format!("status = {}\n", case.expect.status));
    body.push_str(&format!("stdout_file = {}\n", toml_basic(&stdout_name)));
    if !stderr_name.is_empty() {
        body.push_str(&format!("stderr_file = {}\n", toml_basic(&stderr_name)));
    }
    if !requests_name.is_empty() {
        body.push_str(&format!("requests_file = {}\n", toml_basic(&requests_name)));
    }
    if !case.expect.files.is_empty() {
        body.push_str("\n[expect.files]\n");
        for (key, value) in &case.expect.files {
            body.push_str(&format!("{} = {}\n", toml_basic(key), toml_basic(value)));
        }
    }
    fs::write(path, body).map_err(|err| anyhow!("write {}: {err}", path.display()))?;
    Ok(())
}

/// Write `<stem>.<extension>` when there is text for it, and remove a stale
/// one when there is not. Returns the file name, or an empty string.
fn write_optional(dir: &Path, stem: &str, extension: &str, text: &str) -> Result<String> {
    let name = format!("{stem}.{extension}");
    let path = dir.join(&name);
    if text.is_empty() {
        if path.exists() {
            fs::remove_file(&path).map_err(|err| anyhow!("remove {}: {err}", path.display()))?;
        }
        return Ok(String::new());
    }
    fs::write(&path, text).map_err(|err| anyhow!("write {}: {err}", path.display()))?;
    Ok(name)
}

pub fn iter_cases(root: &Path) -> Result<Vec<PathBuf>> {
    let mut paths = Vec::new();
    if !root.exists() {
        return Ok(paths);
    }
    walk_toml(root, &mut paths)?;
    paths.sort();
    Ok(paths)
}

fn walk_toml(dir: &Path, out: &mut Vec<PathBuf>) -> Result<()> {
    for entry in fs::read_dir(dir).map_err(|err| anyhow!("read {}: {err}", dir.display()))? {
        let entry = entry.map_err(|err| anyhow!(err))?;
        let path = entry.path();
        if path.is_dir() {
            walk_toml(&path, out)?;
        } else if path.extension().and_then(|ext| ext.to_str()) == Some("toml") {
            out.push(path);
        }
    }
    Ok(())
}

/// Command path from a help argv. `["--help"]` is the root `ldcli`.
pub fn help_command(argv: &[String]) -> Option<String> {
    if argv.last().map(String::as_str) != Some("--help") {
        return None;
    }
    let mut parts = vec!["ldcli".to_string()];
    parts.extend(argv[..argv.len() - 1].iter().cloned());
    Some(parts.join(" "))
}

pub fn help_case_id(command: &str) -> String {
    command.split_whitespace().collect::<Vec<_>>().join("__")
}

pub fn help_argv(command: &str) -> Vec<String> {
    let mut argv: Vec<String> = command
        .split_whitespace()
        .skip(1)
        .map(str::to_string)
        .collect();
    argv.push("--help".to_string());
    argv
}

fn toml_list(items: &[String]) -> String {
    let rendered: Vec<String> = items.iter().map(|item| toml_basic(item)).collect();
    format!("[{}]", rendered.join(", "))
}

fn toml_basic(value: &str) -> String {
    let mut out = String::from("\"");
    for ch in value.chars() {
        match ch {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fixture::Response;

    #[test]
    fn round_trip_keeps_stdout_separate() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ldcli__config.toml");
        let case = Case {
            id: "ldcli__config".into(),
            argv: vec!["config".into(), "--help".into()],
            declare: vec!["config:ldcli/config.yml".into()],
            env: BTreeMap::new(),
            seed: BTreeMap::new(),
            http: vec![
                Route {
                    method: "POST".into(),
                    path: "/internal/device-authorization/token".into(),
                    status: 400,
                    body: "{\"code\":\"authorization_pending\"}".into(),
                    then: vec![Response {
                        status: 200,
                        body: "{\"accessToken\":\"{{ACCESS_TOKEN}}\"}".into(),
                    }],
                },
                Route {
                    method: "GET".into(),
                    path: "/api/v2/caller-identity".into(),
                    status: 200,
                    body: "{}".into(),
                    then: Vec::new(),
                },
            ],
            redact: vec!["hostname".into()],
            rust: false,
            expect: Expectation {
                status: 0,
                stdout: "Usage:\n  ldcli config\n".into(),
                stderr: "deprecated\n".into(),
                files: BTreeMap::from([("config:ldcli/config.yml".into(), "{}\n".into())]),
                requests: "GET /api/v2/caller-identity\n".into(),
            },
        };
        save_case(&path, &case).unwrap();
        assert!(dir.path().join("ldcli__config.requests").is_file());
        assert!(dir.path().join("ldcli__config.stdout").is_file());
        assert!(dir.path().join("ldcli__config.stderr").is_file());
        let loaded = load_case(&path).unwrap();
        assert_eq!(loaded, case);
        assert_eq!(help_command(&loaded.argv).as_deref(), Some("ldcli config"));
    }

    #[test]
    fn a_case_without_redact_rules_round_trips_empty() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ldcli.toml");
        let case = Case {
            id: "ldcli".into(),
            argv: vec!["--help".into()],
            declare: vec![],
            env: BTreeMap::new(),
            seed: BTreeMap::new(),
            http: Vec::new(),
            redact: vec![],
            rust: false,
            expect: Expectation::default(),
        };
        save_case(&path, &case).unwrap();
        let text = fs::read_to_string(&path).unwrap();
        assert!(!text.contains("redact"), "{text}");
        assert_eq!(load_case(&path).unwrap().redact, Vec::<String>::new());
    }
}
