//! Go's `strconv.Quote`, which is what `%q` prints and what several Go error
//! texts embed.

/// `strconv.Quote`: a double-quoted Go string literal.
pub fn quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        push_escaped(&mut out, c, '"');
    }
    out.push('"');
    out
}

/// `quoteChar` in `encoding/json`: one byte, read as the rune with that code
/// point, in single quotes. A byte above 0x7f therefore names a Latin-1
/// character rather than a UTF-8 sequence, as Go's does.
pub fn quote_byte(c: u8) -> String {
    match c {
        b'\'' => r"'\''".to_string(),
        b'"' => "'\"'".to_string(),
        _ => {
            let mut out = String::from("'");
            push_escaped(&mut out, char::from(c), '"');
            out.push('\'');
            out
        }
    }
}

fn push_escaped(out: &mut String, c: char, delimiter: char) {
    if c == delimiter || c == '\\' {
        out.push('\\');
        out.push(c);
        return;
    }
    if is_print(c) {
        out.push(c);
        return;
    }
    match c {
        '\u{7}' => out.push_str(r"\a"),
        '\u{8}' => out.push_str(r"\b"),
        '\u{c}' => out.push_str(r"\f"),
        '\n' => out.push_str(r"\n"),
        '\r' => out.push_str(r"\r"),
        '\t' => out.push_str(r"\t"),
        '\u{b}' => out.push_str(r"\v"),
        c if (c as u32) < 0x20 || c == '\u{7f}' => out.push_str(&format!(r"\x{:02x}", c as u32)),
        c if (c as u32) < 0x10000 => out.push_str(&format!(r"\u{:04x}", c as u32)),
        c => out.push_str(&format!(r"\U{:08x}", c as u32)),
    }
}

/// `strconv.IsPrint`. Latin-1 is exact; above it, Go's tables are
/// approximated by "not a control character and not a space other than
/// U+0020", which is where the two agree for text a server plausibly sends.
fn is_print(c: char) -> bool {
    let r = c as u32;
    if r <= 0xff {
        return (0x20..=0x7e).contains(&r) || ((0xa1..=0xff).contains(&r) && r != 0xad);
    }
    !c.is_control() && !c.is_whitespace()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn quote_escapes_what_go_escapes() {
        assert_eq!(quote("plain"), "\"plain\"");
        assert_eq!(quote("a\"b\\c"), r#""a\"b\\c""#);
        assert_eq!(quote("tab\there\n"), r#""tab\there\n""#);
        assert_eq!(quote("\u{1}\u{7f}"), r#""\x01\x7f""#);
        assert_eq!(quote("caf\u{e9}\u{a0}\u{ad}"), r#""café\u00a0\u00ad""#);
    }

    #[test]
    fn a_quoted_byte_uses_single_quotes_and_reads_high_bytes_as_latin_1() {
        assert_eq!(quote_byte(b'<'), "'<'");
        assert_eq!(quote_byte(b'\''), r"'\''");
        assert_eq!(quote_byte(b'"'), "'\"'");
        assert_eq!(quote_byte(b'\n'), r"'\n'");
        assert_eq!(quote_byte(0xff), "'ÿ'");
        assert_eq!(quote_byte(0x80), r"'\u0080'");
    }
}
