//! Go's `path/filepath` on Unix, and the `os` calls setup makes through it.
//!
//! Setup prints the paths it builds, so they have to be built the way Go
//! builds them: joined and cleaned lexically, globbed with `filepath.Match`
//! (which treats `*?[\` in a directory name as pattern syntax too), and
//! walked in lexical order without following symlinks.

use std::fs;
use std::os::unix::fs::MetadataExt;

/// `filepath.Clean`.
pub fn clean(path: &str) -> String {
    String::from_utf8_lossy(&crate::gourl::path_clean(path.as_bytes())).into_owned()
}

/// `filepath.Join`: empty elements are ignored, and the result is cleaned.
pub fn join(elems: &[&str]) -> String {
    match elems.iter().position(|e| !e.is_empty()) {
        Some(first) => clean(&elems[first..].join("/")),
        None => String::new(),
    }
}

pub fn is_abs(path: &str) -> bool {
    path.starts_with('/')
}

/// `filepath.Base`.
pub fn base(path: &str) -> String {
    if path.is_empty() {
        return ".".into();
    }
    let trimmed = path.trim_end_matches('/');
    if trimmed.is_empty() {
        return "/".into();
    }
    match trimmed.rfind('/') {
        Some(i) => trimmed[i + 1..].to_string(),
        None => trimmed.to_string(),
    }
}

/// `filepath.Dir`.
pub fn dir(path: &str) -> String {
    let split = path.rfind('/').map(|i| i + 1).unwrap_or(0);
    clean(&path[..split])
}

/// `os.Getwd`: `$PWD` when it is absolute and names the current directory,
/// which keeps the symlinked path the shell shows; otherwise `getcwd`.
pub fn getwd(pwd: Option<&str>) -> Result<String, String> {
    let dot = fs::metadata(".").map_err(|err| format!("stat .: {}", errno_text(&err)))?;
    if let Some(pwd) = pwd.filter(|pwd| is_abs(pwd)) {
        if let Ok(named) = fs::metadata(pwd) {
            if named.dev() == dot.dev() && named.ino() == dot.ino() {
                return Ok(pwd.to_string());
            }
        }
    }
    std::env::current_dir()
        .map(|dir| dir.to_string_lossy().into_owned())
        .map_err(|err| format!("getwd: {}", errno_text(&err)))
}

/// `filepath.Abs`.
pub fn abs(path: &str, pwd: Option<&str>) -> Result<String, String> {
    if is_abs(path) {
        return Ok(clean(path));
    }
    Ok(join(&[&getwd(pwd)?, path]))
}

/// `filepath.Rel`.
pub fn rel(basepath: &str, targpath: &str) -> Option<String> {
    let base = clean(basepath);
    let targ = clean(targpath);
    if targ == base {
        return Some(".".into());
    }
    let base = if base == "." { String::new() } else { base };
    if base.starts_with('/') != targ.starts_with('/') {
        return None;
    }
    let (b, t) = (base.as_bytes(), targ.as_bytes());
    let (bl, tl) = (b.len(), t.len());
    let (mut b0, mut bi, mut t0, mut ti) = (0, 0, 0, 0);
    loop {
        while bi < bl && b[bi] != b'/' {
            bi += 1;
        }
        while ti < tl && t[ti] != b'/' {
            ti += 1;
        }
        if t[t0..ti] != b[b0..bi] {
            break;
        }
        if bi < bl {
            bi += 1;
        }
        if ti < tl {
            ti += 1;
        }
        b0 = bi;
        t0 = ti;
        if b0 >= bl && t0 >= tl {
            break;
        }
    }
    if &b[b0..bi] == b".." {
        return None;
    }
    if b0 != bl {
        let seps = b[b0..bl].iter().filter(|c| **c == b'/').count();
        let mut out = String::from("..");
        for _ in 0..seps {
            out.push_str("/..");
        }
        if t0 != tl {
            out.push('/');
            out.push_str(&targ[t0..]);
        }
        return Some(out);
    }
    Some(targ[t0..].to_string())
}

/// `os.Stat(path)` succeeded.
pub fn exists(path: &str) -> bool {
    fs::metadata(path).is_ok()
}

/// `os.Stat(path)` succeeded on something that is not a directory.
pub fn is_file(path: &str) -> bool {
    fs::metadata(path).is_ok_and(|m| !m.is_dir())
}

/// `os.ReadFile`.
pub fn read(path: &str) -> Option<Vec<u8>> {
    fs::read(path).ok()
}

/// A directory entry as `fs.DirEntry` reports it: a symlink is not a
/// directory, whatever it points at.
pub struct Entry {
    pub name: String,
    pub is_dir: bool,
}

/// `os.ReadDir`: entries sorted by name, or none when the directory cannot
/// be read.
pub fn read_dir(path: &str) -> Vec<Entry> {
    let Ok(entries) = fs::read_dir(path) else {
        return Vec::new();
    };
    let mut out: Vec<Entry> = entries
        .filter_map(Result::ok)
        .map(|entry| Entry {
            name: entry.file_name().to_string_lossy().into_owned(),
            is_dir: entry.file_type().is_ok_and(|t| t.is_dir()),
        })
        .collect();
    out.sort_by(|a, b| a.name.cmp(&b.name));
    out
}

pub enum Walk {
    Continue,
    SkipDir,
    SkipAll,
}

/// `filepath.WalkDir` with a callback that ignores errors, which is how
/// every setup caller uses it.
pub fn walk_dir(root: &str, visit: &mut dyn FnMut(&str, &Entry) -> Walk) {
    let Ok(info) = fs::symlink_metadata(root) else {
        return;
    };
    let entry = Entry {
        name: base(root),
        is_dir: info.is_dir(),
    };
    walk(root, &entry, visit);
}

/// Returns what `walkDir` returns: `Continue` for nil. A `SkipDir` for a
/// file is passed up, and its parent then skips the file's later siblings.
fn walk(path: &str, entry: &Entry, visit: &mut dyn FnMut(&str, &Entry) -> Walk) -> Walk {
    match visit(path, entry) {
        Walk::SkipDir if entry.is_dir => return Walk::Continue,
        Walk::Continue if entry.is_dir => {}
        other => return other,
    }
    for child in read_dir(path) {
        match walk(&join(&[path, &child.name]), &child, visit) {
            Walk::Continue => {}
            Walk::SkipDir => break,
            Walk::SkipAll => return Walk::SkipAll,
        }
    }
    Walk::Continue
}

fn has_meta(path: &str) -> bool {
    path.contains(['*', '?', '[', '\\'])
}

/// `filepath.ErrBadPattern`.
pub struct BadPattern;

/// `filepath.Glob`. Every setup caller ignores the error and reads an empty
/// result.
pub fn glob(pattern: &str) -> Vec<String> {
    glob_with_limit(pattern, 0).unwrap_or_default()
}

fn glob_with_limit(pattern: &str, depth: usize) -> Result<Vec<String>, BadPattern> {
    match_pattern(pattern, "")?;
    if depth == 10000 {
        return Err(BadPattern);
    }
    if !has_meta(pattern) {
        if fs::symlink_metadata(pattern).is_err() {
            return Ok(Vec::new());
        }
        return Ok(vec![pattern.to_string()]);
    }
    let split = pattern.rfind('/').map(|i| i + 1).unwrap_or(0);
    let (dir, file) = pattern.split_at(split);
    let dir = match dir {
        "" => ".".to_string(),
        "/" => "/".to_string(),
        _ => dir[..dir.len() - 1].to_string(),
    };
    if !has_meta(&dir) {
        return glob_in(&dir, file, Vec::new());
    }
    if dir == pattern {
        return Err(BadPattern);
    }
    let mut matches = Vec::new();
    for d in glob_with_limit(&dir, depth + 1)? {
        matches = glob_in(&d, file, matches)?;
    }
    Ok(matches)
}

fn glob_in(dir: &str, pattern: &str, mut matches: Vec<String>) -> Result<Vec<String>, BadPattern> {
    if !fs::metadata(dir).is_ok_and(|m| m.is_dir()) {
        return Ok(matches);
    }
    let Ok(entries) = fs::read_dir(dir) else {
        return Ok(matches);
    };
    let mut names: Vec<String> = entries
        .filter_map(Result::ok)
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .collect();
    names.sort();
    for name in names {
        if match_pattern(pattern, &name)? {
            matches.push(join(&[dir, &name]));
        }
    }
    Ok(matches)
}

/// `filepath.Match`.
pub fn match_pattern(pattern: &str, name: &str) -> Result<bool, BadPattern> {
    let mut pattern = pattern.as_bytes();
    let mut name = name.as_bytes();
    'pattern: while !pattern.is_empty() {
        let (star, chunk, rest) = scan_chunk(pattern);
        pattern = rest;
        if star && chunk.is_empty() {
            return Ok(!name.contains(&b'/'));
        }
        let (t, ok, err) = match_chunk(chunk, name);
        if ok && (t.is_empty() || !pattern.is_empty()) {
            name = t;
            continue;
        }
        if err {
            return Err(BadPattern);
        }
        if star {
            let mut i = 0;
            while i < name.len() && name[i] != b'/' {
                let (t, ok, err) = match_chunk(chunk, &name[i + 1..]);
                if ok {
                    if pattern.is_empty() && !t.is_empty() {
                        i += 1;
                        continue;
                    }
                    name = t;
                    continue 'pattern;
                }
                if err {
                    return Err(BadPattern);
                }
                i += 1;
            }
        }
        while !pattern.is_empty() {
            let (_, chunk, rest) = scan_chunk(pattern);
            pattern = rest;
            if match_chunk(chunk, b"").2 {
                return Err(BadPattern);
            }
        }
        return Ok(false);
    }
    Ok(name.is_empty())
}

fn scan_chunk(mut pattern: &[u8]) -> (bool, &[u8], &[u8]) {
    let mut star = false;
    while pattern.first() == Some(&b'*') {
        pattern = &pattern[1..];
        star = true;
    }
    let mut inrange = false;
    let mut i = 0;
    while i < pattern.len() {
        match pattern[i] {
            b'\\' => {
                if i + 1 < pattern.len() {
                    i += 1;
                }
            }
            b'[' => inrange = true,
            b']' => inrange = false,
            b'*' if !inrange => break,
            _ => {}
        }
        i += 1;
    }
    (star, &pattern[..i], &pattern[i..])
}

/// `utf8.DecodeRune`: an invalid byte decodes as U+FFFD of width one.
fn decode_rune(s: &[u8]) -> (u32, usize) {
    if s.is_empty() {
        return (0xfffd, 0);
    }
    let width = match s[0] {
        0x00..=0x7f => return (u32::from(s[0]), 1),
        0xc2..=0xdf => 2,
        0xe0..=0xef => 3,
        0xf0..=0xf4 => 4,
        _ => return (0xfffd, 1),
    };
    match s
        .get(..width)
        .and_then(|bytes| std::str::from_utf8(bytes).ok())
    {
        Some(text) => (text.chars().next().map_or(0xfffd, u32::from), width),
        None => (0xfffd, 1),
    }
}

/// Returns the rest of `s`, whether the chunk matched, and whether the
/// chunk is malformed.
fn match_chunk<'a>(mut chunk: &[u8], mut s: &'a [u8]) -> (&'a [u8], bool, bool) {
    let mut failed = false;
    while !chunk.is_empty() {
        if !failed && s.is_empty() {
            failed = true;
        }
        match chunk[0] {
            b'[' => {
                let mut r = 0;
                if !failed {
                    let (rune, n) = decode_rune(s);
                    r = rune;
                    s = &s[n..];
                }
                chunk = &chunk[1..];
                let mut negated = false;
                if chunk.first() == Some(&b'^') {
                    negated = true;
                    chunk = &chunk[1..];
                }
                let mut matched = false;
                let mut nrange = 0;
                loop {
                    if chunk.first() == Some(&b']') && nrange > 0 {
                        chunk = &chunk[1..];
                        break;
                    }
                    let Some((lo, rest)) = get_esc(chunk) else {
                        return (b"", false, true);
                    };
                    chunk = rest;
                    let mut hi = lo;
                    if chunk[0] == b'-' {
                        let Some((high, rest)) = get_esc(&chunk[1..]) else {
                            return (b"", false, true);
                        };
                        hi = high;
                        chunk = rest;
                    }
                    if lo <= r && r <= hi {
                        matched = true;
                    }
                    nrange += 1;
                }
                if matched == negated {
                    failed = true;
                }
            }
            b'?' => {
                if !failed {
                    if s[0] == b'/' {
                        failed = true;
                    }
                    let (_, n) = decode_rune(s);
                    s = &s[n..];
                }
                chunk = &chunk[1..];
            }
            c => {
                let literal = if c == b'\\' {
                    chunk = &chunk[1..];
                    match chunk.first() {
                        Some(escaped) => *escaped,
                        None => return (b"", false, true),
                    }
                } else {
                    c
                };
                if !failed {
                    if literal != s[0] {
                        failed = true;
                    }
                    s = &s[1..];
                }
                chunk = &chunk[1..];
            }
        }
    }
    if failed {
        return (b"", false, false);
    }
    (s, true, false)
}

fn get_esc(mut chunk: &[u8]) -> Option<(u32, &[u8])> {
    if chunk.is_empty() || chunk[0] == b'-' || chunk[0] == b']' {
        return None;
    }
    if chunk[0] == b'\\' {
        chunk = &chunk[1..];
        if chunk.is_empty() {
            return None;
        }
    }
    let (r, n) = decode_rune(chunk);
    if r == 0xfffd && n == 1 {
        return None;
    }
    let rest = &chunk[n..];
    if rest.is_empty() {
        return None;
    }
    Some((r, rest))
}

/// Go's `syscall.Errno.Error()` text: the C message with its first letter
/// lowered.
pub fn errno_text(err: &std::io::Error) -> String {
    crate::browser::errno_text(err)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn join_skips_empty_elements_and_cleans() {
        assert_eq!(join(&["", ""]), "");
        assert_eq!(join(&["", "a"]), "a");
        assert_eq!(join(&["a", "", "b/"]), "a/b");
        assert_eq!(join(&[".", "main.go"]), "main.go");
        assert_eq!(join(&["sub/", "../x"]), "x");
        assert_eq!(join(&["/", "src"]), "/src");
    }

    #[test]
    fn base_and_dir_follow_go() {
        assert_eq!(base(""), ".");
        assert_eq!(base("/"), "/");
        assert_eq!(base("a/b/"), "b");
        assert_eq!(dir("a/b"), "a");
        assert_eq!(dir("b"), ".");
        assert_eq!(dir("/venv/bin/pip"), "/venv/bin");
    }

    #[test]
    fn rel_goes_up_before_it_goes_down_and_refuses_mixed_roots() {
        assert_eq!(rel("a", "a/b/c").as_deref(), Some("b/c"));
        assert_eq!(rel(".", "src/x").as_deref(), Some("src/x"));
        assert_eq!(rel("/", "/src/x").as_deref(), Some("src/x"));
        assert_eq!(rel("a/b", "a/c/d").as_deref(), Some("../c/d"));
        assert_eq!(rel("a/b/c", "a").as_deref(), Some("../.."));
        assert_eq!(rel("a", "a").as_deref(), Some("."));
        assert_eq!(rel("/a", "b"), None);
        assert_eq!(rel("..", "a"), None);
    }

    #[test]
    fn match_follows_go_including_classes_escapes_and_bad_patterns() {
        let m = |p: &str, n: &str| match_pattern(p, n).ok();
        assert_eq!(m("*.csproj", "App.csproj"), Some(true));
        assert_eq!(m("*.csproj", ".csproj"), Some(true));
        assert_eq!(m("*.csproj", "a/b.csproj"), Some(false));
        assert_eq!(m("*App.swift", "MyAppApp.swift"), Some(true));
        assert_eq!(m("a[1]", "a1"), Some(true));
        assert_eq!(m("a[^1]", "a1"), Some(false));
        assert_eq!(m("a[0-9]b", "a5b"), Some(true));
        assert_eq!(m("a\\*", "a*"), Some(true));
        assert_eq!(m("a?c", "abc"), Some(true));
        assert_eq!(m("a?c", "a/c"), Some(false));
        assert_eq!(m("[", "x"), None);
        assert_eq!(m("a[", ""), None);
        assert_eq!(m("\\", "x"), None);
        assert_eq!(m("*x[", "abc"), None);
    }

    #[test]
    fn glob_treats_pattern_syntax_in_the_directory_as_pattern_syntax() {
        let root = tempfile::tempdir().unwrap();
        let top = root.path().to_str().unwrap();
        fs::create_dir_all(format!("{top}/x1")).unwrap();
        fs::write(format!("{top}/x1/b.gemspec"), "").unwrap();
        fs::write(format!("{top}/x1/a.gemspec"), "").unwrap();
        assert_eq!(
            glob(&format!("{top}/x[1]/*.gemspec")),
            vec![format!("{top}/x1/a.gemspec"), format!("{top}/x1/b.gemspec")]
        );
        assert!(glob(&format!("{top}/x[/*.gemspec")).is_empty());
        assert_eq!(glob(&format!("{top}/x1")), vec![format!("{top}/x1")]);
    }

    #[test]
    fn walk_is_lexical_does_not_follow_symlinks_and_honours_skips() {
        let root = tempfile::tempdir().unwrap();
        let top = root.path().to_str().unwrap();
        for file in ["b/2", "a/1", "a/bin/3", "c"] {
            let path = format!("{top}/{file}");
            fs::create_dir_all(dir(&path)).unwrap();
            fs::write(&path, "").unwrap();
        }
        std::os::unix::fs::symlink(format!("{top}/a"), format!("{top}/link")).unwrap();
        let mut seen = Vec::new();
        walk_dir(top, &mut |path, entry| {
            seen.push(rel(top, path).unwrap());
            if entry.is_dir && entry.name == "bin" {
                return Walk::SkipDir;
            }
            if entry.name == "c" {
                return Walk::SkipAll;
            }
            Walk::Continue
        });
        assert_eq!(seen, [".", "a", "a/1", "a/bin", "b", "b/2", "c"]);
    }
}
