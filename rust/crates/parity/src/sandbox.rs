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
    /// Files to write before the run, keyed `config:<path>` or `state:<path>`.
    pub seed: &'a BTreeMap<String, String>,
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

/// The names a CLI looks for when it opens a browser: `xdg-open` and its two
/// fallbacks on Linux, `open` on macOS.
pub const BROWSER_OPENERS: [&str; 4] = ["xdg-open", "x-www-browser", "www-browser", "open"];

/// What every browser opener in the sandbox does instead of opening one. It
/// names the URL on stderr, which the CLI's own stderr inherits, and fails the
/// way a machine with no usable browser does.
pub const BROWSER_SHIM: &str = "#!/bin/sh\necho \"parity browser shim: $*\" >&2\nexit 1\n";

pub fn run_command(request: &RunRequest<'_>) -> Result<RunResult> {
    let root = tempfile::tempdir().map_err(|err| anyhow!("tempdir: {err}"))?;
    let sandbox_root = root.path().to_path_buf();
    let config_home = sandbox_root.join("config");
    let state_home = sandbox_root.join("state");
    let cache_home = sandbox_root.join("cache");
    let data_home = sandbox_root.join("data");
    let home = sandbox_root.join("home");
    let work = sandbox_root.join("work");
    let shims = sandbox_root.join("bin");
    for dir in [
        &config_home,
        &state_home,
        &cache_home,
        &data_home,
        &home,
        &work,
        &shims,
    ] {
        fs::create_dir_all(dir).map_err(|err| anyhow!("mkdir {}: {err}", dir.display()))?;
    }
    for name in BROWSER_OPENERS {
        write_executable(&shims.join(name), BROWSER_SHIM)?;
    }
    for (key, contents) in request.seed {
        let path = seed_path(key, &config_home, &state_home)?;
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .map_err(|err| anyhow!("mkdir {}: {err}", parent.display()))?;
        }
        fs::write(&path, contents).map_err(|err| anyhow!("seed {key}: {err}"))?;
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
    // The parent's PATH is not passed on. What a command finds there, such as
    // a package manager or a real browser, would make the transcript depend
    // on the machine, and running it would reach outside the sandbox. A case
    // that sets PATH gets it after the shims, so an opener is still a shim.
    let path = match request.extra_env.get("PATH") {
        Some(extra) => format!("{}:{extra}", shims.display()),
        None => shims.display().to_string(),
    };
    cmd.env("PATH", path);

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

fn write_executable(path: &Path, contents: &str) -> Result<()> {
    use std::os::unix::fs::PermissionsExt;
    fs::write(path, contents).map_err(|err| anyhow!("write {}: {err}", path.display()))?;
    fs::set_permissions(path, fs::Permissions::from_mode(0o755))
        .map_err(|err| anyhow!("chmod {}: {err}", path.display()))
}

/// Resolve a `config:` or `state:` key inside the sandbox. A key that would
/// climb out of it is refused.
fn seed_path(key: &str, config_home: &Path, state_home: &Path) -> Result<PathBuf> {
    let (root, rel) = if let Some(rel) = key.strip_prefix("config:") {
        (config_home, rel)
    } else if let Some(rel) = key.strip_prefix("state:") {
        (state_home, rel)
    } else {
        return Err(anyhow!("seed key {key} must start with config: or state:"));
    };
    if rel.is_empty() || rel.starts_with('/') || rel.split('/').any(|part| part == "..") {
        return Err(anyhow!("seed key {key} must stay inside the sandbox"));
    }
    Ok(root.join(rel))
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

    /// The sandbox PATH holds only the browser shims, so a fake binary
    /// that calls `mkdir` or `cat` names where they live itself.
    fn script(dir: &Path, body: &str) -> PathBuf {
        let path = dir.join("fake-ldcli");
        let body = body.replacen('\n', "\nPATH=\"$PATH:/usr/bin:/bin\"\n", 1);
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
            seed: &BTreeMap::new(),
            opt_out_update_check: true,
        })
        .unwrap();
        let second = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &declare,
            extra_env: &env_b,
            seed: &BTreeMap::new(),
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
            seed: &BTreeMap::new(),
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

    fn run_raw(body: &str, extra_env: &BTreeMap<String, String>) -> RunResult {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("fake-ldcli");
        write_executable(&path, body).unwrap();
        run_command(&RunRequest {
            bin: &path,
            argv: &[],
            declare: &[],
            extra_env,
            seed: &BTreeMap::new(),
            opt_out_update_check: true,
        })
        .unwrap()
    }

    #[test]
    fn path_holds_only_the_browser_shims_and_every_opener_fails_without_a_browser() {
        // Only shell builtins here: nothing else is reachable on this PATH.
        let result = run_raw(
            "#!/bin/sh\necho \"$PATH\"\nfor name in xdg-open x-www-browser www-browser open; do\n  \"$name\" \"https://example.test/$name\"\n  echo \"$name exited $?\"\ndone\n",
            &BTreeMap::new(),
        );
        let root = result.sandbox_root.display().to_string();
        assert_eq!(
            result.stdout,
            format!(
                "{root}/bin\nxdg-open exited 1\nx-www-browser exited 1\nwww-browser exited 1\nopen exited 1\n"
            )
        );
        assert_eq!(
            result.stderr,
            "parity browser shim: https://example.test/xdg-open\nparity browser shim: https://example.test/x-www-browser\nparity browser shim: https://example.test/www-browser\nparity browser shim: https://example.test/open\n"
        );
    }

    #[test]
    fn a_case_path_comes_after_the_shims() {
        let env = BTreeMap::from([("PATH".to_string(), "/nonexistent".to_string())]);
        let result = run_raw("#!/bin/sh\necho \"$PATH\"\n", &env);
        assert_eq!(
            result.stdout,
            format!("{}/bin:/nonexistent\n", result.sandbox_root.display())
        );
    }

    #[test]
    fn a_seeded_file_is_in_place_before_the_run_and_cannot_escape() {
        let dir = tempfile::tempdir().unwrap();
        let bin = script(
            dir.path(),
            "#!/bin/sh\ncat \"$XDG_CONFIG_HOME/ldcli/config.yml\"\n",
        );
        let declare = vec!["config:ldcli/config.yml".to_string()];
        let seed = BTreeMap::from([(
            "config:ldcli/config.yml".to_string(),
            "output: markdown\n".to_string(),
        )]);
        let env = BTreeMap::new();
        let result = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &declare,
            extra_env: &env,
            seed: &seed,
            opt_out_update_check: true,
        })
        .unwrap();
        assert_eq!(result.stdout, "output: markdown\n");

        let escape = BTreeMap::from([("config:../outside".to_string(), "x".to_string())]);
        let err = run_command(&RunRequest {
            bin: &bin,
            argv: &[],
            declare: &declare,
            extra_env: &env,
            seed: &escape,
            opt_out_update_check: true,
        })
        .unwrap_err();
        assert!(err.to_string().contains("inside the sandbox"), "{err}");
    }
}
