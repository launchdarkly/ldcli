//! `github.com/pkg/browser.OpenURL`, with the error text Go returns.
//!
//! On Linux the first of `xdg-open`, `x-www-browser`, and `www-browser` that
//! `exec.LookPath` finds is run with the URL; on macOS it is `open`. The
//! opener inherits this process's stdout and stderr, so whatever it prints
//! lands between the CLI's own lines.

use std::ffi::{OsStr, OsString};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::{CommandExt, ExitStatusExt};
use std::path::{Path, PathBuf};
use std::process::Command;

#[cfg(target_os = "macos")]
const PROVIDERS: &[&str] = &["open"];
#[cfg(not(target_os = "macos"))]
const PROVIDERS: &[&str] = &["xdg-open", "x-www-browser", "www-browser"];

/// Open `url`. `path` is the `PATH` the lookup searches.
pub fn open_url(url: &str, path: Option<&OsStr>) -> Result<(), String> {
    if cfg!(target_os = "macos") {
        // `exec.Command` records a failed lookup and `Run` returns it.
        let program = look_path(PROVIDERS[0], path)?;
        return run(PROVIDERS[0], &program, url);
    }
    for provider in PROVIDERS {
        if let Ok(program) = look_path(provider, path) {
            return run(provider, &program, url);
        }
    }
    Err(format!(
        "exec: {}: executable file not found in $PATH",
        crate::gostr::quote(&PROVIDERS.join(","))
    ))
}

fn run(name: &str, program: &Path, url: &str) -> Result<(), String> {
    let status = Command::new(program)
        .arg0(name)
        .arg(url)
        .status()
        .map_err(|err| format!("fork/exec {}: {}", program.display(), errno_text(&err)))?;
    if status.success() {
        return Ok(());
    }
    match (status.code(), status.signal()) {
        (Some(code), _) => Err(format!("exit status {code}")),
        (None, Some(signal)) => Err(format!("signal: {}", signal_name(signal))),
        (None, None) => Err("exit status -1".to_string()),
    }
}

/// `exec.LookPath`. A name with a slash is checked as given. Otherwise each
/// `PATH` entry is tried in turn, an empty entry meaning the current
/// directory; a hit under a relative entry is refused rather than skipped.
pub fn look_path(file: &str, path: Option<&OsStr>) -> Result<PathBuf, String> {
    let error = |reason: &str| format!("exec: {}: {reason}", crate::gostr::quote(file));
    if file.contains('/') {
        return find_executable(Path::new(file))
            .map(|()| PathBuf::from(file))
            .map_err(|reason| error(&reason));
    }
    let path = path.map(OsString::from).unwrap_or_default();
    for dir in std::env::split_paths(&path) {
        let dir = if dir.as_os_str().is_empty() {
            PathBuf::from(".")
        } else {
            dir
        };
        let candidate = go_join(&dir, file);
        if find_executable(&candidate).is_ok() {
            if !candidate.is_absolute() {
                return Err(error(
                    "cannot run executable found relative to current directory",
                ));
            }
            return Ok(candidate);
        }
    }
    Err(error("executable file not found in $PATH"))
}

/// `filepath.Join(dir, file)`, which cleans away a leading `./`.
fn go_join(dir: &Path, file: &str) -> PathBuf {
    let joined = dir.join(file);
    let text = joined.to_string_lossy();
    match text.strip_prefix("./") {
        Some(rest) => PathBuf::from(rest),
        None => joined,
    }
}

fn find_executable(file: &Path) -> Result<(), String> {
    let metadata = std::fs::metadata(file)
        .map_err(|err| format!("stat {}: {}", file.display(), errno_text(&err)))?;
    if metadata.is_dir() {
        return Err("is a directory".to_string());
    }
    if metadata.permissions().mode() & 0o111 == 0 {
        return Err("permission denied".to_string());
    }
    Ok(())
}

/// Go's `syscall.Errno` text, which is the C library's message in lower case.
pub(crate) fn errno_text(err: &std::io::Error) -> String {
    let text = err.to_string();
    let message = match text.find(" (os error ") {
        Some(i) => &text[..i],
        None => &text,
    };
    let mut chars = message.chars();
    match chars.next() {
        Some(first) => first.to_lowercase().chain(chars).collect(),
        None => String::new(),
    }
}

fn signal_name(signal: i32) -> String {
    match signal {
        1 => "hangup".into(),
        2 => "interrupt".into(),
        3 => "quit".into(),
        6 => "aborted".into(),
        9 => "killed".into(),
        11 => "segmentation fault".into(),
        13 => "broken pipe".into(),
        15 => "terminated".into(),
        other => format!("signal {other}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn executable(dir: &Path, name: &str, body: &str) {
        let path = dir.join(name);
        std::fs::write(&path, body).unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
    }

    #[test]
    fn the_first_opener_on_the_path_is_used() {
        let dir = tempfile::tempdir().unwrap();
        executable(dir.path(), "x-www-browser", "#!/bin/sh\nexit 0\n");
        let path = dir.path().as_os_str();
        assert_eq!(open_url("http://h", Some(path)), Ok(()));
        executable(dir.path(), "xdg-open", "#!/bin/sh\nexit 3\n");
        assert_eq!(
            open_url("http://h", Some(path)),
            Err("exit status 3".to_string())
        );
    }

    #[test]
    fn no_opener_names_all_three_in_one_error() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(
            open_url("http://h", Some(dir.path().as_os_str())),
            Err(
                "exec: \"xdg-open,x-www-browser,www-browser\": executable file not found in $PATH"
                    .to_string()
            )
        );
    }

    #[test]
    fn a_file_that_is_not_executable_is_passed_over() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("xdg-open"), "").unwrap();
        assert!(look_path("xdg-open", Some(dir.path().as_os_str())).is_err());
    }

    #[test]
    fn a_directory_named_like_the_opener_is_passed_over() {
        let first = tempfile::tempdir().unwrap();
        let second = tempfile::tempdir().unwrap();
        std::fs::create_dir(first.path().join("tool")).unwrap();
        executable(second.path(), "tool", "#!/bin/sh\n");
        let path = std::env::join_paths([first.path(), second.path()]).unwrap();
        assert_eq!(
            look_path("tool", Some(&path)),
            Ok(second.path().join("tool"))
        );
    }

    #[test]
    fn a_path_entry_joins_the_way_go_cleans_it() {
        assert_eq!(go_join(Path::new("."), "tool"), PathBuf::from("tool"));
        assert!(!go_join(Path::new("."), "tool").is_absolute());
        assert_eq!(
            go_join(Path::new("/bin"), "tool"),
            PathBuf::from("/bin/tool")
        );
    }

    #[test]
    fn errno_text_is_lower_case_without_the_os_error_suffix() {
        let err = std::io::Error::from_raw_os_error(2);
        assert_eq!(errno_text(&err), "no such file or directory");
    }
}
