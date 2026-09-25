//! Refuse to keep a harness secret under `parity/`.

use anyhow::{anyhow, Result};
use regex::Regex;
use std::fs;
use std::path::Path;
use std::sync::OnceLock;

/// Scan `roots` for any non-empty secret.
pub fn scan_tree(roots: &[impl AsRef<Path>], secrets: &[String]) -> Result<()> {
    let needles: Vec<&str> = secrets
        .iter()
        .map(String::as_str)
        .filter(|s| !s.is_empty())
        .collect();
    if needles.is_empty() {
        return Ok(());
    }
    let mut hits = Vec::new();
    for root in roots {
        let root = root.as_ref();
        if !root.exists() {
            continue;
        }
        walk(root, &mut |path, text| {
            if needles.iter().any(|needle| text.contains(needle)) {
                hits.push(format!("{} contains a harness secret", path.display()));
            }
        })?;
    }
    finish(hits)
}

/// Fail when a file stores an `Authorization` value that is not a placeholder.
/// The header name alone is allowed, so help text can mention it.
pub fn scan_authorization(roots: &[impl AsRef<Path>]) -> Result<()> {
    let mut hits = Vec::new();
    for root in roots {
        let root = root.as_ref();
        if !root.exists() {
            continue;
        }
        walk(root, &mut |path, text| {
            let leaked = authorization_re().captures_iter(text).any(|cap| {
                let value = cap.get(1).map(|m| m.as_str()).unwrap_or("");
                !(value.starts_with('[') && value.ends_with(']'))
            });
            if leaked {
                hits.push(format!(
                    "{} contains an Authorization value",
                    path.display()
                ));
            }
        })?;
    }
    finish(hits)
}

fn finish(hits: Vec<String>) -> Result<()> {
    if hits.is_empty() {
        Ok(())
    } else {
        Err(anyhow!(
            "refusing to keep harness secrets under parity/:\n{}",
            hits.join("\n")
        ))
    }
}

fn authorization_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(r"(?i)authorization:\s*(\S+)").expect("authorization regex"))
}

fn walk(dir: &Path, visit: &mut dyn FnMut(&Path, &str)) -> Result<()> {
    for entry in fs::read_dir(dir).map_err(|err| anyhow!("read {}: {err}", dir.display()))? {
        let entry = entry.map_err(|err| anyhow!(err))?;
        let path = entry.path();
        if path.is_dir() {
            walk(&path, visit)?;
            continue;
        }
        let bytes = fs::read(&path).map_err(|err| anyhow!("read {}: {err}", path.display()))?;
        visit(&path, &String::from_utf8_lossy(&bytes));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn scan_fails_when_a_captured_file_contains_the_token() {
        let dir = tempfile::tempdir().unwrap();
        let cases = dir.path().join("cases");
        fs::create_dir_all(cases.join("help")).unwrap();
        fs::write(
            cases.join("help").join("root.stdout"),
            "token parity-token-secret\n",
        )
        .unwrap();
        let err = scan_tree(&[cases], &["parity-token-secret".into()]).unwrap_err();
        assert!(err.to_string().contains("parity/"), "{err}");
        assert!(err.to_string().contains("root.stdout"), "{err}");
    }

    #[test]
    fn placeholder_text_is_not_a_secret() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("note.txt"), "Authorization: [REDACTED]\n").unwrap();
        scan_tree(&[dir.path()], &["parity-token-secret".into()]).unwrap();
        scan_authorization(&[dir.path()]).unwrap();
    }

    #[test]
    fn authorization_value_fails_and_a_placeholder_does_not() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("login.stdout");
        fs::write(&path, "Authorization: api-live-token\n").unwrap();
        let err = scan_authorization(&[dir.path()]).unwrap_err();
        assert!(err.to_string().contains("Authorization"), "{err}");
        fs::write(&path, "Authorization: [ACCESS_TOKEN]\n").unwrap();
        scan_authorization(&[dir.path()]).unwrap();
    }
}
