//! Run one binary in a fresh config and state directory.

use anyhow::{anyhow, Result};
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

#[derive(Debug, Clone)]
pub struct RunRequest<'a> {
    pub bin: &'a Path,
    pub argv: &'a [String],
    pub declare: &'a [String],
    pub extra_env: &'a BTreeMap<String, String>,
    pub opt_out_update_check: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RunResult {
    pub status: i32,
    pub stdout: String,
    pub stderr: String,
    pub files: BTreeMap<String, String>,
    pub undeclared: Vec<String>,
    pub config_home: PathBuf,
    pub state_home: PathBuf,
    pub sandbox_root: PathBuf,
}

pub fn run_command(request: &RunRequest<'_>) -> Result<RunResult> {
    let root = tempfile::tempdir().map_err(|err| anyhow!("tempdir: {err}"))?;
    let sandbox_root = root.path().to_path_buf();
    let config_home = sandbox_root.join("config");
    let state_home = sandbox_root.join("state");
    let cache_home = sandbox_root.join("cache");
    let data_home = sandbox_root.join("data");
    let home = sandbox_root.join("home");
    let work = sandbox_root.join("work");
    for dir in [
        &config_home,
        &state_home,
        &cache_home,
        &data_home,
        &home,
        &work,
    ] {
        fs::create_dir_all(dir).map_err(|err| anyhow!("mkdir {}: {err}", dir.display()))?;
    }

    // A relative program path is resolved against the child's current_dir on
    // Linux, which is the empty sandbox. Use an absolute path.
    let bin = request
        .bin
        .canonicalize()
        .map_err(|err| anyhow!("binary {}: {err}", request.bin.display()))?;
    let mut cmd = Command::new(&bin);
    // stdin is not a terminal, so the Go help path falls back to width 80.
    cmd.args(request.argv)
        .current_dir(&work)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .env_clear();
    cmd.env("PATH", std::env::var("PATH").unwrap_or_default());
    cmd.env("HOME", &home);
    cmd.env("XDG_CONFIG_HOME", &config_home);
    cmd.env("XDG_STATE_HOME", &state_home);
    cmd.env("XDG_CACHE_HOME", &cache_home);
    cmd.env("XDG_DATA_HOME", &data_home);
    cmd.env("NO_COLOR", "1");
    cmd.env("CLICOLOR", "0");
    cmd.env("TERM", "dumb");
    cmd.env("COLUMNS", "80");
    cmd.env("TZ", "UTC");
    cmd.env("LANG", "C.UTF-8");
    // The dual-binary harness does not post analytics.
    cmd.env("LD_ANALYTICS_OPT_OUT", "true");
    if request.opt_out_update_check {
        cmd.env("LD_UPDATE_CHECK_OPT_OUT", "true");
    }
    for (key, value) in request.extra_env {
        cmd.env(key, value);
    }

    let mut child = cmd
        .spawn()
        .map_err(|err| anyhow!("spawn {}: {err}", bin.display()))?;
    let mut stdout_pipe = child.stdout.take().ok_or_else(|| anyhow!("stdout pipe"))?;
    let mut stderr_pipe = child.stderr.take().ok_or_else(|| anyhow!("stderr pipe"))?;
    let stdout_thread = thread::spawn(move || {
        let mut buf = Vec::new();
        stdout_pipe.read_to_end(&mut buf).map(|_| buf)
    });
    let stderr_thread = thread::spawn(move || {
        let mut buf = Vec::new();
        stderr_pipe.read_to_end(&mut buf).map(|_| buf)
    });

    let started = Instant::now();
    let status = loop {
        match child.try_wait().map_err(|err| anyhow!("wait: {err}"))? {
            Some(status) => break status,
            None if started.elapsed() > Duration::from_secs(30) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(anyhow!(
                    "timed out after 30s: {} {}",
                    bin.display(),
                    request.argv.join(" ")
                ));
            }
            None => thread::sleep(Duration::from_millis(20)),
        }
    };
    let stdout_bytes = stdout_thread
        .join()
        .map_err(|_| anyhow!("stdout reader panicked"))?
        .map_err(|err| anyhow!("read stdout: {err}"))?;
    let stderr_bytes = stderr_thread
        .join()
        .map_err(|_| anyhow!("stderr reader panicked"))?
        .map_err(|err| anyhow!("read stderr: {err}"))?;

    let stdout = String::from_utf8(stdout_bytes).map_err(|_| anyhow!("stdout is not utf-8"))?;
    let stderr = String::from_utf8(stderr_bytes).map_err(|_| anyhow!("stderr is not utf-8"))?;
    let (files, undeclared) = snapshot(&config_home, &state_home, request.declare)?;
    let code = status.code().unwrap_or(128);

    Ok(RunResult {
        status: code,
        stdout,
        stderr,
        files,
        undeclared,
        config_home,
        state_home,
        sandbox_root,
    })
}

fn snapshot(
    config_home: &Path,
    state_home: &Path,
    declare: &[String],
) -> Result<(BTreeMap<String, String>, Vec<String>)> {
    let declared: BTreeSet<&str> = declare.iter().map(String::as_str).collect();
    let mut files = BTreeMap::new();
    let mut undeclared = Vec::new();
    collect(
        config_home,
        "config",
        &declared,
        &mut files,
        &mut undeclared,
    )?;
    collect(state_home, "state", &declared, &mut files, &mut undeclared)?;
    Ok((files, undeclared))
}

fn collect(
    root: &Path,
    prefix: &str,
    declared: &BTreeSet<&str>,
    files: &mut BTreeMap<String, String>,
    undeclared: &mut Vec<String>,
) -> Result<()> {
    let mut found = Vec::new();
    walk_files(root, &mut found)?;
    for path in found {
        let rel = path
            .strip_prefix(root)
            .map_err(|err| anyhow!(err))?
            .components()
            .map(|c| c.as_os_str().to_string_lossy())
            .collect::<Vec<_>>()
            .join("/");
        let key = format!("{prefix}:{rel}");
        let text =
            fs::read_to_string(&path).map_err(|err| anyhow!("read {}: {err}", path.display()))?;
        if declared.contains(key.as_str()) {
            files.insert(key, text);
        } else {
            undeclared.push(key);
        }
    }
    Ok(())
}

fn walk_files(dir: &Path, out: &mut Vec<PathBuf>) -> Result<()> {
    if !dir.exists() {
        return Ok(());
    }
    for entry in fs::read_dir(dir).map_err(|err| anyhow!("read {}: {err}", dir.display()))? {
        let entry = entry.map_err(|err| anyhow!(err))?;
        let path = entry.path();
        if path.is_dir() {
            walk_files(&path, out)?;
        } else if path.is_file() {
            out.push(path);
        }
    }
    Ok(())
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

    #[test]
    fn two_runs_use_different_config_directories() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            "#!/bin/sh\nmkdir -p \"$XDG_CONFIG_HOME/ldcli\"\nprintf '%s\\n' \"$TOKEN\" > \"$XDG_CONFIG_HOME/ldcli/config.yml\"\nprintf 'ok\\n'\n",
        );
        let declare = vec!["config:ldcli/config.yml".to_string()];
        let mut env_a = BTreeMap::new();
        env_a.insert("TOKEN".into(), "token-from-first-case".into());
        let mut env_b = BTreeMap::new();
        env_b.insert("TOKEN".into(), "token-from-second-case".into());
        let first = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &declare,
            extra_env: &env_a,
            opt_out_update_check: true,
        })
        .unwrap();
        let second = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &declare,
            extra_env: &env_b,
            opt_out_update_check: true,
        })
        .unwrap();
        assert_ne!(first.config_home, second.config_home);
        let second_file = second.files.get("config:ldcli/config.yml").unwrap();
        assert!(
            !second_file.contains("token-from-first-case"),
            "{second_file}"
        );
        assert!(
            second_file.contains("token-from-second-case"),
            "{second_file}"
        );
    }

    #[test]
    fn undeclared_config_write_is_reported() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            "#!/bin/sh\nmkdir -p \"$XDG_CONFIG_HOME/ldcli\"\nprintf '{}\\n' > \"$XDG_CONFIG_HOME/ldcli/config.yml\"\n",
        );
        let env = BTreeMap::new();
        let result = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &[],
            extra_env: &env,
            opt_out_update_check: true,
        })
        .unwrap();
        assert!(
            result
                .undeclared
                .iter()
                .any(|p| p == "config:ldcli/config.yml"),
            "{:?}",
            result.undeclared
        );
    }
}
