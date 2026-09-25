//! Go's `url.Parse`, `url.JoinPath`, and `URL.String`, byte for byte.
//!
//! The CLI builds browser URLs with `url.JoinPath`, which cleans the joined
//! path with `path.Join` and re-escapes it, so `?` and `#` in a joined element
//! come out percent-encoded instead of starting a query or fragment. The parse
//! errors are Go's, because signup prints them.

use crate::gostr::quote;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Encoding {
    Path,
    Host,
    Zone,
    UserPassword,
    Fragment,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
struct Userinfo {
    username: Vec<u8>,
    password: Option<Vec<u8>>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Url {
    scheme: String,
    opaque: String,
    user: Option<Userinfo>,
    host: Vec<u8>,
    path: Vec<u8>,
    raw_path: Vec<u8>,
    omit_host: bool,
    force_query: bool,
    raw_query: String,
    fragment: Vec<u8>,
    raw_fragment: Vec<u8>,
}

/// `url.JoinPath(base, elem...)`: an unparsable base is an error, and nothing
/// about the elements is.
pub fn join_path(base: &str, elems: &[&str]) -> Result<String, String> {
    Ok(parse(base)?.join_path(elems).string())
}

/// `url.Parse`, with the `*url.Error` text Go returns.
pub fn parse(raw: &str) -> Result<Url, String> {
    let (without_fragment, fragment) = match raw.split_once('#') {
        Some((before, after)) => (before, Some(after)),
        None => (raw, None),
    };
    let mut url = parse_reference(without_fragment)
        .map_err(|err| format!("parse {}: {err}", quote(without_fragment)))?;
    if let Some(fragment) = fragment.filter(|f| !f.is_empty()) {
        url.set_fragment(fragment.as_bytes())
            .map_err(|err| format!("parse {}: {err}", quote(raw)))?;
    }
    Ok(url)
}

fn parse_reference(raw: &str) -> Result<Url, String> {
    if raw.bytes().any(|b| b < b' ' || b == 0x7f) {
        return Err("net/url: invalid control character in URL".to_string());
    }
    let mut url = Url::default();
    if raw == "*" {
        url.path = b"*".to_vec();
        return Ok(url);
    }
    let (scheme, rest) = get_scheme(raw)?;
    url.scheme = scheme.to_lowercase();
    let mut rest = rest.to_string();
    if rest.ends_with('?') && rest.matches('?').count() == 1 {
        url.force_query = true;
        rest.pop();
    } else if let Some((before, query)) = rest.split_once('?') {
        url.raw_query = query.to_string();
        rest = before.to_string();
    }

    if !rest.starts_with('/') {
        if !url.scheme.is_empty() {
            url.opaque = rest;
            return Ok(url);
        }
        let segment = rest.split('/').next().unwrap_or("");
        if segment.contains(':') {
            return Err("first path segment in URL cannot contain colon".to_string());
        }
    }

    if (!url.scheme.is_empty() || !rest.starts_with("///")) && rest.starts_with("//") {
        let after = &rest[2..];
        let (authority, path) = match after.find('/') {
            Some(i) => (&after[..i], &after[i..]),
            None => (after, ""),
        };
        let (user, host) = parse_authority(authority)?;
        url.user = user;
        url.host = host;
        rest = path.to_string();
    } else if !url.scheme.is_empty() && rest.starts_with('/') {
        url.omit_host = true;
    }
    url.set_path(rest.as_bytes())?;
    Ok(url)
}

fn get_scheme(raw: &str) -> Result<(&str, &str), String> {
    for (i, c) in raw.bytes().enumerate() {
        match c {
            b'a'..=b'z' | b'A'..=b'Z' => {}
            b'0'..=b'9' | b'+' | b'-' | b'.' => {
                if i == 0 {
                    return Ok(("", raw));
                }
            }
            b':' => {
                if i == 0 {
                    return Err("missing protocol scheme".to_string());
                }
                return Ok((&raw[..i], &raw[i + 1..]));
            }
            _ => return Ok(("", raw)),
        }
    }
    Ok(("", raw))
}

fn parse_authority(authority: &str) -> Result<(Option<Userinfo>, Vec<u8>), String> {
    let at = authority.rfind('@');
    let host = parse_host(match at {
        Some(i) => &authority[i + 1..],
        None => authority,
    })?;
    let Some(i) = at else {
        return Ok((None, host));
    };
    let userinfo = &authority[..i];
    if !valid_userinfo(userinfo) {
        return Err("net/url: invalid userinfo".to_string());
    }
    let user = match userinfo.split_once(':') {
        None => Userinfo {
            username: unescape(userinfo.as_bytes(), Encoding::UserPassword)?,
            password: None,
        },
        Some((username, password)) => Userinfo {
            username: unescape(username.as_bytes(), Encoding::UserPassword)?,
            password: Some(unescape(password.as_bytes(), Encoding::UserPassword)?),
        },
    };
    Ok((Some(user), host))
}

fn parse_host(host: &str) -> Result<Vec<u8>, String> {
    match host.rfind('[') {
        Some(open) if open > 0 => return Err("invalid IP-literal".to_string()),
        Some(_) => {
            let close = host
                .rfind(']')
                .ok_or_else(|| "missing ']' in host".to_string())?;
            let colon_port = &host[close + 1..];
            if !valid_optional_port(colon_port) {
                return Err(format!("invalid port {} after host", quote(colon_port)));
            }
            let colon_port = unescape(colon_port.as_bytes(), Encoding::Host)?;
            let hostname = &host[1..close];
            let unescaped = match hostname.find("%25") {
                Some(zone) => {
                    let mut part = unescape(&hostname.as_bytes()[..zone], Encoding::Host)?;
                    part.extend(unescape(&hostname.as_bytes()[zone..], Encoding::Zone)?);
                    part
                }
                None => unescape(hostname.as_bytes(), Encoding::Host)?,
            };
            let text = String::from_utf8_lossy(&unescaped).into_owned();
            // netip.ParseAddr's own wording varies with the defect; only the
            // prefix is Go's.
            let address = text.split('%').next().unwrap_or("");
            match address.parse::<std::net::IpAddr>() {
                Ok(std::net::IpAddr::V4(_)) => return Err("invalid IP-literal".to_string()),
                Ok(std::net::IpAddr::V6(_)) => {}
                Err(_) => {
                    return Err(format!(
                        "invalid host: ParseAddr({}): unable to parse IP",
                        quote(&text)
                    ))
                }
            }
            let mut out = b"[".to_vec();
            out.extend(unescaped);
            out.push(b']');
            out.extend(colon_port);
            return Ok(out);
        }
        None => {
            if let Some(i) = host.rfind(':') {
                let colon_port = &host[i..];
                if !valid_optional_port(colon_port) {
                    return Err(format!("invalid port {} after host", quote(colon_port)));
                }
            }
        }
    }
    unescape(host.as_bytes(), Encoding::Host)
}

fn valid_optional_port(port: &str) -> bool {
    if port.is_empty() {
        return true;
    }
    match port.strip_prefix(':') {
        Some(digits) => digits.bytes().all(|b| b.is_ascii_digit()),
        None => false,
    }
}

fn valid_userinfo(s: &str) -> bool {
    s.chars().all(|r| {
        r.is_ascii_alphanumeric()
            || matches!(
                r,
                '-' | '.'
                    | '_'
                    | ':'
                    | '~'
                    | '!'
                    | '$'
                    | '&'
                    | '\''
                    | '('
                    | ')'
                    | '*'
                    | '+'
                    | ','
                    | ';'
                    | '='
                    | '%'
                    | '@'
            )
    })
}

impl Url {
    /// `URL.JoinPath`. Go discards the error from `setPath` here, so an
    /// element with a bad escape leaves the path as it was.
    pub fn join_path(&self, elems: &[&str]) -> Url {
        let mut all: Vec<Vec<u8>> = vec![self.escaped_path()];
        all.extend(elems.iter().map(|e| e.as_bytes().to_vec()));
        let joined = if !all[0].starts_with(b"/") {
            let mut first = b"/".to_vec();
            first.extend(&all[0]);
            all[0] = first;
            path_join(&all)[1..].to_vec()
        } else {
            path_join(&all)
        };
        let mut joined = joined;
        if all.last().is_some_and(|e| e.ends_with(b"/")) && !joined.ends_with(b"/") {
            joined.push(b'/');
        }
        let mut url = self.clone();
        let _ = url.set_path(&joined);
        url
    }

    fn set_path(&mut self, p: &[u8]) -> Result<(), String> {
        let path = unescape(p, Encoding::Path)?;
        self.raw_path = if escape(&path, Encoding::Path) == p {
            Vec::new()
        } else {
            p.to_vec()
        };
        self.path = path;
        Ok(())
    }

    fn set_fragment(&mut self, f: &[u8]) -> Result<(), String> {
        let fragment = unescape(f, Encoding::Fragment)?;
        self.raw_fragment = if escape(&fragment, Encoding::Fragment) == f {
            Vec::new()
        } else {
            f.to_vec()
        };
        self.fragment = fragment;
        Ok(())
    }

    fn escaped_path(&self) -> Vec<u8> {
        if !self.raw_path.is_empty() && valid_encoded(&self.raw_path, Encoding::Path) {
            if let Ok(p) = unescape(&self.raw_path, Encoding::Path) {
                if p == self.path {
                    return self.raw_path.clone();
                }
            }
        }
        if self.path == b"*" {
            return b"*".to_vec();
        }
        escape(&self.path, Encoding::Path)
    }

    fn escaped_fragment(&self) -> Vec<u8> {
        if !self.raw_fragment.is_empty() && valid_encoded(&self.raw_fragment, Encoding::Fragment) {
            if let Ok(f) = unescape(&self.raw_fragment, Encoding::Fragment) {
                if f == self.fragment {
                    return self.raw_fragment.clone();
                }
            }
        }
        escape(&self.fragment, Encoding::Fragment)
    }

    /// `URL.String`.
    pub fn string(&self) -> String {
        let mut buf: Vec<u8> = Vec::new();
        if !self.scheme.is_empty() {
            buf.extend(self.scheme.as_bytes());
            buf.push(b':');
        }
        if !self.opaque.is_empty() {
            buf.extend(self.opaque.as_bytes());
        } else {
            if !self.scheme.is_empty() || !self.host.is_empty() || self.user.is_some() {
                if self.omit_host && self.host.is_empty() && self.user.is_none() {
                    // Go leaves the authority out entirely here.
                } else {
                    if !self.host.is_empty() || !self.path.is_empty() || self.user.is_some() {
                        buf.extend(b"//");
                    }
                    if let Some(user) = &self.user {
                        buf.extend(escape(&user.username, Encoding::UserPassword));
                        if let Some(password) = &user.password {
                            buf.push(b':');
                            buf.extend(escape(password, Encoding::UserPassword));
                        }
                        buf.push(b'@');
                    }
                    if !self.host.is_empty() {
                        buf.extend(escape(&self.host, Encoding::Host));
                    }
                }
            }
            let path = self.escaped_path();
            if !path.is_empty() && path[0] != b'/' && !self.host.is_empty() {
                buf.push(b'/');
            }
            if buf.is_empty() {
                let segment = path.split(|b| *b == b'/').next().unwrap_or(&[]);
                if segment.contains(&b':') {
                    buf.extend(b"./");
                }
            }
            buf.extend(path);
        }
        if self.force_query || !self.raw_query.is_empty() {
            buf.push(b'?');
            buf.extend(self.raw_query.as_bytes());
        }
        if !self.fragment.is_empty() {
            buf.push(b'#');
            buf.extend(self.escaped_fragment());
        }
        String::from_utf8_lossy(&buf).into_owned()
    }
}

fn should_escape(c: u8, mode: Encoding) -> bool {
    if c.is_ascii_alphanumeric() {
        return false;
    }
    if matches!(mode, Encoding::Host | Encoding::Zone)
        && matches!(
            c,
            b'!' | b'$'
                | b'&'
                | b'\''
                | b'('
                | b')'
                | b'*'
                | b'+'
                | b','
                | b';'
                | b'='
                | b':'
                | b'['
                | b']'
                | b'<'
                | b'>'
                | b'"'
        )
    {
        return false;
    }
    match c {
        b'-' | b'_' | b'.' | b'~' => return false,
        b'$' | b'&' | b'+' | b',' | b'/' | b':' | b';' | b'=' | b'?' | b'@' => match mode {
            Encoding::Path => return c == b'?',
            Encoding::UserPassword => return matches!(c, b'@' | b'/' | b'?' | b':'),
            Encoding::Fragment => return false,
            Encoding::Host | Encoding::Zone => {}
        },
        _ => {}
    }
    if mode == Encoding::Fragment && matches!(c, b'!' | b'(' | b')' | b'*') {
        return false;
    }
    true
}

fn unhex(c: u8) -> u8 {
    match c {
        b'0'..=b'9' => c - b'0',
        b'a'..=b'f' => c - b'a' + 10,
        _ => c - b'A' + 10,
    }
}

fn unescape(s: &[u8], mode: Encoding) -> Result<Vec<u8>, String> {
    let mut i = 0;
    while i < s.len() {
        match s[i] {
            b'%' => {
                if i + 2 >= s.len()
                    || !s[i + 1].is_ascii_hexdigit()
                    || !s[i + 2].is_ascii_hexdigit()
                {
                    let end = (i + 3).min(s.len());
                    return Err(escape_error(&s[i..end]));
                }
                if mode == Encoding::Host && unhex(s[i + 1]) < 8 && &s[i..i + 3] != b"%25" {
                    return Err(escape_error(&s[i..i + 3]));
                }
                if mode == Encoding::Zone {
                    let v = (unhex(s[i + 1]) << 4) | unhex(s[i + 2]);
                    if &s[i..i + 3] != b"%25" && v != b' ' && should_escape(v, Encoding::Host) {
                        return Err(escape_error(&s[i..i + 3]));
                    }
                }
                i += 3;
            }
            c => {
                if matches!(mode, Encoding::Host | Encoding::Zone)
                    && c < 0x80
                    && should_escape(c, mode)
                {
                    return Err(format!(
                        "invalid character {} in host name",
                        quote(&String::from_utf8_lossy(&s[i..i + 1]))
                    ));
                }
                i += 1;
            }
        }
    }
    let mut out = Vec::with_capacity(s.len());
    let mut i = 0;
    while i < s.len() {
        if s[i] == b'%' {
            out.push((unhex(s[i + 1]) << 4) | unhex(s[i + 2]));
            i += 3;
        } else {
            out.push(s[i]);
            i += 1;
        }
    }
    Ok(out)
}

fn escape_error(s: &[u8]) -> String {
    format!("invalid URL escape {}", quote(&String::from_utf8_lossy(s)))
}

fn escape(s: &[u8], mode: Encoding) -> Vec<u8> {
    const UPPER_HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut out = Vec::with_capacity(s.len());
    for &c in s {
        if should_escape(c, mode) {
            out.push(b'%');
            out.push(UPPER_HEX[(c >> 4) as usize]);
            out.push(UPPER_HEX[(c & 15) as usize]);
        } else {
            out.push(c);
        }
    }
    out
}

fn valid_encoded(s: &[u8], mode: Encoding) -> bool {
    s.iter().all(|&c| {
        matches!(
            c,
            b'!' | b'$'
                | b'&'
                | b'\''
                | b'('
                | b')'
                | b'*'
                | b'+'
                | b','
                | b';'
                | b'='
                | b':'
                | b'@'
                | b'['
                | b']'
                | b'%'
        ) || !should_escape(c, mode)
    })
}

/// `path.Join`: the non-empty elements joined with `/`, then cleaned.
fn path_join(elems: &[Vec<u8>]) -> Vec<u8> {
    let mut buf = Vec::new();
    for e in elems {
        if !buf.is_empty() || !e.is_empty() {
            if !buf.is_empty() {
                buf.push(b'/');
            }
            buf.extend(e);
        }
    }
    if buf.is_empty() {
        return buf;
    }
    path_clean(&buf)
}

/// `path.Clean`.
fn path_clean(path: &[u8]) -> Vec<u8> {
    if path.is_empty() {
        return b".".to_vec();
    }
    let rooted = path[0] == b'/';
    let n = path.len();
    let mut out: Vec<u8> = Vec::with_capacity(n);
    let (mut r, mut dotdot) = (0, 0);
    if rooted {
        out.push(b'/');
        r = 1;
        dotdot = 1;
    }
    while r < n {
        let empty_or_dot =
            path[r] == b'/' || (path[r] == b'.' && (r + 1 == n || path[r + 1] == b'/'));
        if empty_or_dot {
            r += 1;
        } else if path[r] == b'.' && path[r + 1] == b'.' && (r + 2 == n || path[r + 2] == b'/') {
            r += 2;
            if out.len() > dotdot {
                let mut removed = out.pop();
                while out.len() > dotdot && removed != Some(b'/') {
                    removed = out.pop();
                }
            } else if !rooted {
                if !out.is_empty() {
                    out.push(b'/');
                }
                out.extend(b"..");
                dotdot = out.len();
            }
        } else {
            if (rooted && out.len() != 1) || (!rooted && !out.is_empty()) {
                out.push(b'/');
            }
            while r < n && path[r] != b'/' {
                out.push(path[r]);
                r += 1;
            }
        }
    }
    if out.is_empty() {
        return b".".to_vec();
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn join(base: &str, elem: &str) -> String {
        join_path(base, &[elem]).unwrap()
    }

    #[test]
    fn a_relative_element_joins_under_the_base_path() {
        assert_eq!(
            join("https://app.launchdarkly.com", "/signup"),
            "https://app.launchdarkly.com/signup"
        );
        assert_eq!(
            join("http://example.test/x/", "/signup"),
            "http://example.test/x/signup"
        );
        assert_eq!(
            join("http://h:1", "/confirm-auth/abc/"),
            "http://h:1/confirm-auth/abc/"
        );
        assert_eq!(join("http://h:1", ""), "http://h:1");
    }

    #[test]
    fn a_joined_element_is_cleaned_and_its_query_is_escaped_into_the_path() {
        assert_eq!(
            join(
                "http://h:1",
                "https://example.test/confirm auth/../ok?from=cli#done"
            ),
            "http://h:1/https:/example.test/ok%3Ffrom=cli%23done"
        );
        assert_eq!(join("http://h:1/a/b", "../../../c"), "http://h:1/c");
    }

    #[test]
    fn the_base_query_and_fragment_survive_a_join() {
        assert_eq!(join("http://h/p?q=1#frag", "x"), "http://h/p/x?q=1#frag");
    }

    #[test]
    fn a_relative_base_stays_relative() {
        assert_eq!(
            join("app.launchdarkly.com", "signup"),
            "app.launchdarkly.com/signup"
        );
        assert_eq!(join("", "/signup"), "signup");
    }

    #[test]
    fn a_bad_escape_in_an_element_leaves_the_path_alone() {
        assert_eq!(join("http://h/p", "%zz"), "http://h/p");
    }

    #[test]
    fn parse_errors_are_worded_and_quoted_like_go() {
        assert_eq!(
            join_path("http://h:abc", &["x"]).unwrap_err(),
            r#"parse "http://h:abc": invalid port ":abc" after host"#
        );
        assert_eq!(
            join_path(":nope", &["x"]).unwrap_err(),
            r#"parse ":nope": missing protocol scheme"#
        );
        assert_eq!(
            join_path("http://h\tx", &["x"]).unwrap_err(),
            r#"parse "http://h\tx": net/url: invalid control character in URL"#
        );
        assert_eq!(
            join_path("http://a b", &["x"]).unwrap_err(),
            r#"parse "http://a b": invalid character " " in host name"#
        );
        assert_eq!(
            join_path("http://h/%zz", &["x"]).unwrap_err(),
            r#"parse "http://h/%zz": invalid URL escape "%zz""#
        );
        assert_eq!(
            join_path("a:b/c", &["x"]).map(|_| ()),
            Ok(()),
            "a scheme makes the rest opaque"
        );
        assert_eq!(
            join_path("1a:b", &["x"]).unwrap_err(),
            r#"parse "1a:b": first path segment in URL cannot contain colon"#
        );
    }

    #[test]
    fn an_opaque_base_ignores_the_joined_path() {
        assert_eq!(join("mailto:someone", "x"), "mailto:someone");
    }

    #[test]
    fn clean_matches_go() {
        for (input, want) in [
            ("", "."),
            ("/", "/"),
            ("a/../..", ".."),
            ("/../a", "/a"),
            ("a//b/./c/", "a/b/c"),
            ("../../a", "../../a"),
        ] {
            assert_eq!(
                String::from_utf8(path_clean(input.as_bytes())).unwrap(),
                want,
                "{input}"
            );
        }
    }
}
