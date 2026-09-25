//! Go's `json.Unmarshal` into a flat struct of string, int, and
//! `map[string]string` fields, with the error text Go returns.
//!
//! Go validates the whole document before it decodes any of it, so a syntax
//! error anywhere wins over a type mismatch, and the syntax error names the
//! offending byte and what the scanner expected there. A type mismatch does
//! not stop the decode: the first one is returned once the rest is done.
//! Keys match field names exactly or, failing that, case-insensitively, and a
//! repeated key overwrites the earlier value.

use crate::gostr::quote_byte;

const MAX_NESTING_DEPTH: usize = 10000;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    String,
    Int,
    /// `map[string]string`. Only the keys are kept.
    StringMap,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Field {
    Str(String),
    Int(i64),
    /// A map's keys, in the order they were first set.
    Map(Vec<String>),
}

impl Field {
    fn zero(kind: Kind) -> Self {
        match kind {
            Kind::String => Self::Str(String::new()),
            Kind::Int => Self::Int(0),
            Kind::StringMap => Self::Map(Vec::new()),
        }
    }

    pub fn as_str(&self) -> &str {
        match self {
            Self::Str(s) => s,
            Self::Int(_) | Self::Map(_) => "",
        }
    }

    pub fn keys(&self) -> &[String] {
        match self {
            Self::Map(keys) => keys,
            Self::Str(_) | Self::Int(_) => &[],
        }
    }
}

/// The Go struct being decoded into.
pub struct Struct<'a> {
    /// `reflect.Type.Name()`, as in `DeviceAuthorization`.
    pub name: &'a str,
    /// `reflect.Type.String()`, as in `login.DeviceAuthorization`.
    pub type_string: &'a str,
    /// JSON field names and their Go kinds, in declaration order.
    pub fields: &'a [(&'a str, Kind)],
}

/// Decode `data` into `target`. The fields come back in declaration order,
/// zero-valued where the document did not set them.
pub fn unmarshal(data: &[u8], target: &Struct<'_>) -> Result<Vec<Field>, String> {
    check_valid(data)?;
    let mut parser = Parser { data, pos: 0 };
    let value = parser.value();
    let mut fields: Vec<Field> = target
        .fields
        .iter()
        .map(|(_, kind)| Field::zero(*kind))
        .collect();
    let mut first_error = None;
    let mut save = |err: String| {
        first_error.get_or_insert(err);
    };
    match value {
        Json::Object(members) => {
            for (key, value) in members {
                let Some(index) = field_index(target, &key) else {
                    continue;
                };
                let (name, kind) = target.fields[index];
                let mismatch = |found: String, of: &str| {
                    format!(
                        "json: cannot unmarshal {found} into Go struct field {}.{name} of type {of}",
                        target.name
                    )
                };
                if let (Json::Object(members), Field::Map(keys)) = (&value, &mut fields[index]) {
                    merge_string_map(members, keys, |found| save(mismatch(found, "string")));
                    continue;
                }
                match store(&value, kind) {
                    Ok(Some(field)) => fields[index] = field,
                    Ok(None) => {}
                    Err(found) => save(mismatch(found, kind_name(kind))),
                }
            }
        }
        Json::Null => {}
        other => save(format!(
            "json: cannot unmarshal {} into Go value of type {}",
            other.describe(),
            target.type_string
        )),
    }
    match first_error {
        Some(err) => Err(err),
        None => Ok(fields),
    }
}

fn kind_name(kind: Kind) -> &'static str {
    match kind {
        Kind::String => "string",
        Kind::Int => "int",
        Kind::StringMap => "map[string]string",
    }
}

/// Go decodes an object into the map already there, so a repeated key merges
/// into it rather than replacing it. An element that is not a string is a
/// mismatch, but its key is still set, to the zero value.
fn merge_string_map(
    members: &[(String, Json)],
    keys: &mut Vec<String>,
    mut mismatch: impl FnMut(String),
) {
    for (key, value) in members {
        match value {
            Json::String(_) | Json::Null => {}
            other => mismatch(other.describe().to_string()),
        }
        if !keys.contains(key) {
            keys.push(key.clone());
        }
    }
}

fn field_index(target: &Struct<'_>, key: &str) -> Option<usize> {
    target
        .fields
        .iter()
        .position(|(name, _)| *name == key)
        .or_else(|| {
            let folded = key.to_lowercase();
            target
                .fields
                .iter()
                .position(|(name, _)| name.to_lowercase() == folded)
        })
}

/// `literalStore` and its object and array siblings for one field. `None`
/// leaves the field as it was, which is what `null` does.
fn store(value: &Json, kind: Kind) -> Result<Option<Field>, String> {
    match (value, kind) {
        (Json::Null, _) => Ok(None),
        (Json::String(s), Kind::String) => Ok(Some(Field::Str(s.clone()))),
        (Json::Number(text), Kind::Int) => match text.parse::<i64>() {
            Ok(n) => Ok(Some(Field::Int(n))),
            Err(_) => Err(format!("number {text}")),
        },
        (other, _) => Err(other.describe().to_string()),
    }
}

enum Json {
    Null,
    Bool,
    Number(String),
    String(String),
    Array,
    Object(Vec<(String, Json)>),
}

impl Json {
    fn describe(&self) -> &'static str {
        match self {
            Self::Null => "null",
            Self::Bool => "bool",
            Self::Number(_) => "number",
            Self::String(_) => "string",
            Self::Array => "array",
            Self::Object(_) => "object",
        }
    }
}

/// Reads a document `check_valid` has already accepted.
struct Parser<'a> {
    data: &'a [u8],
    pos: usize,
}

impl Parser<'_> {
    fn peek(&self) -> u8 {
        self.data.get(self.pos).copied().unwrap_or(0)
    }

    fn skip_space(&mut self) {
        while matches!(self.peek(), b' ' | b'\t' | b'\r' | b'\n') {
            self.pos += 1;
        }
    }

    fn value(&mut self) -> Json {
        self.skip_space();
        match self.peek() {
            b'{' => {
                self.pos += 1;
                let mut members = Vec::new();
                loop {
                    self.skip_space();
                    if self.peek() == b'}' {
                        self.pos += 1;
                        break;
                    }
                    if self.peek() == b',' {
                        self.pos += 1;
                        self.skip_space();
                    }
                    let key = self.string();
                    self.skip_space();
                    self.pos += 1; // ':'
                    let value = self.value();
                    members.push((key, value));
                }
                Json::Object(members)
            }
            b'[' => {
                self.pos += 1;
                loop {
                    self.skip_space();
                    match self.peek() {
                        b']' => {
                            self.pos += 1;
                            break;
                        }
                        b',' => self.pos += 1,
                        _ => {
                            self.value();
                        }
                    }
                }
                Json::Array
            }
            b'"' => Json::String(self.string()),
            b't' => {
                self.pos += 4;
                Json::Bool
            }
            b'f' => {
                self.pos += 5;
                Json::Bool
            }
            b'n' => {
                self.pos += 4;
                Json::Null
            }
            _ => {
                let start = self.pos;
                while matches!(self.peek(), b'-' | b'+' | b'.' | b'e' | b'E' | b'0'..=b'9') {
                    self.pos += 1;
                }
                Json::Number(String::from_utf8_lossy(&self.data[start..self.pos]).into_owned())
            }
        }
    }

    /// `unquoteBytes`: an invalid UTF-8 byte or an unpaired surrogate becomes
    /// U+FFFD, one per byte.
    fn string(&mut self) -> String {
        self.pos += 1; // opening quote
        let mut out = String::new();
        loop {
            match self.peek() {
                b'"' => {
                    self.pos += 1;
                    return out;
                }
                b'\\' => {
                    self.pos += 1;
                    let escape = self.peek();
                    self.pos += 1;
                    match escape {
                        b'b' => out.push('\u{8}'),
                        b'f' => out.push('\u{c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let unit = self.hex4();
                            if (0xd800..0xdc00).contains(&unit)
                                && self.data.get(self.pos) == Some(&b'\\')
                                && self.data.get(self.pos + 1) == Some(&b'u')
                            {
                                let save = self.pos;
                                self.pos += 2;
                                let low = self.hex4();
                                if (0xdc00..0xe000).contains(&low) {
                                    let c = 0x10000 + ((unit - 0xd800) << 10) + (low - 0xdc00);
                                    out.push(char::from_u32(c).unwrap_or('\u{fffd}'));
                                    continue;
                                }
                                self.pos = save;
                            }
                            out.push(char::from_u32(unit).unwrap_or('\u{fffd}'));
                        }
                        other => out.push(char::from(other)),
                    }
                }
                _ => {
                    let rest = &self.data[self.pos..];
                    let end = rest
                        .iter()
                        .position(|b| *b == b'"' || *b == b'\\')
                        .unwrap_or(rest.len());
                    push_lossy_per_byte(&mut out, &rest[..end]);
                    self.pos += end;
                }
            }
        }
    }

    fn hex4(&mut self) -> u32 {
        let digits = &self.data[self.pos..self.pos + 4];
        self.pos += 4;
        u32::from_str_radix(std::str::from_utf8(digits).unwrap_or("fffd"), 16).unwrap_or(0xfffd)
    }
}

/// Go's `utf8.DecodeRune` loop: each byte that does not start a valid
/// sequence is its own U+FFFD, where `from_utf8_lossy` would merge a
/// truncated sequence into one.
fn push_lossy_per_byte(out: &mut String, mut bytes: &[u8]) {
    while !bytes.is_empty() {
        match std::str::from_utf8(bytes) {
            Ok(text) => {
                out.push_str(text);
                return;
            }
            Err(err) => {
                let valid = err.valid_up_to();
                out.push_str(std::str::from_utf8(&bytes[..valid]).unwrap_or_default());
                out.push('\u{fffd}');
                bytes = &bytes[valid + 1..];
            }
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Step {
    BeginValueOrEmpty,
    BeginValue,
    BeginStringOrEmpty,
    BeginString,
    EndValue,
    EndTop,
    InString,
    InStringEsc,
    /// Hex digits of a `\u` escape still to come.
    InStringEscU(u8),
    Neg,
    One,
    Zero,
    Dot,
    Dot0,
    E,
    ESign,
    E0,
    /// Inside `true`, `false`, or `null`, expecting the byte at this index.
    Literal(&'static str, usize),
    Error,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Parse {
    ObjectKey,
    ObjectValue,
    ArrayValue,
}

/// The `encoding/json` scanner, reduced to accept or reject.
struct Scanner {
    step: Step,
    parse: Vec<Parse>,
    end_top: bool,
    err: Option<String>,
}

/// `checkValid`: the first syntax error in `data`, in Go's words.
pub fn check_valid(data: &[u8]) -> Result<(), String> {
    let mut scanner = Scanner {
        step: Step::BeginValue,
        parse: Vec::new(),
        end_top: false,
        err: None,
    };
    for &c in data {
        scanner.step(c);
        if let Some(err) = scanner.err.take() {
            return Err(err);
        }
    }
    if scanner.end_top {
        return Ok(());
    }
    scanner.step(b' ');
    if let Some(err) = scanner.err.take() {
        return Err(err);
    }
    if scanner.end_top {
        return Ok(());
    }
    Err("unexpected end of JSON input".to_string())
}

fn is_space(c: u8) -> bool {
    matches!(c, b' ' | b'\t' | b'\r' | b'\n')
}

fn is_hex(c: u8) -> bool {
    c.is_ascii_hexdigit()
}

impl Scanner {
    fn error(&mut self, c: u8, context: &str) {
        self.step = Step::Error;
        self.err = Some(format!("invalid character {} {context}", quote_byte(c)));
    }

    fn push(&mut self, c: u8, state: Parse) {
        self.parse.push(state);
        if self.parse.len() > MAX_NESTING_DEPTH {
            self.error(c, "exceeded max depth");
        }
    }

    fn pop(&mut self) {
        self.parse.pop();
        if self.parse.is_empty() {
            self.step = Step::EndTop;
            self.end_top = true;
        } else {
            self.step = Step::EndValue;
        }
    }

    fn step(&mut self, c: u8) {
        match self.step {
            Step::BeginValueOrEmpty => {
                if is_space(c) {
                } else if c == b']' {
                    self.end_value(c);
                } else {
                    self.begin_value(c);
                }
            }
            Step::BeginValue => self.begin_value(c),
            Step::BeginStringOrEmpty => {
                if is_space(c) {
                } else if c == b'}' {
                    if let Some(last) = self.parse.last_mut() {
                        *last = Parse::ObjectValue;
                    }
                    self.end_value(c);
                } else {
                    self.begin_string(c);
                }
            }
            Step::BeginString => self.begin_string(c),
            Step::EndValue => self.end_value(c),
            Step::EndTop => {
                if !is_space(c) {
                    self.error(c, "after top-level value");
                }
            }
            Step::InString => {
                if c == b'"' {
                    self.step = Step::EndValue;
                } else if c == b'\\' {
                    self.step = Step::InStringEsc;
                } else if c < 0x20 {
                    self.error(c, "in string literal");
                }
            }
            Step::InStringEsc => match c {
                b'b' | b'f' | b'n' | b'r' | b't' | b'\\' | b'/' | b'"' => {
                    self.step = Step::InString;
                }
                b'u' => self.step = Step::InStringEscU(4),
                _ => self.error(c, "in string escape code"),
            },
            Step::InStringEscU(left) => {
                if is_hex(c) {
                    self.step = if left == 1 {
                        Step::InString
                    } else {
                        Step::InStringEscU(left - 1)
                    };
                } else {
                    self.error(c, "in \\u hexadecimal character escape");
                }
            }
            Step::Neg => match c {
                b'0' => self.step = Step::Zero,
                b'1'..=b'9' => self.step = Step::One,
                _ => self.error(c, "in numeric literal"),
            },
            Step::One => {
                if c.is_ascii_digit() {
                } else {
                    self.zero(c);
                }
            }
            Step::Zero => self.zero(c),
            Step::Dot => {
                if c.is_ascii_digit() {
                    self.step = Step::Dot0;
                } else {
                    self.error(c, "after decimal point in numeric literal");
                }
            }
            Step::Dot0 => {
                if c.is_ascii_digit() {
                } else if c == b'e' || c == b'E' {
                    self.step = Step::E;
                } else {
                    self.end_value(c);
                }
            }
            Step::E => {
                if c == b'+' || c == b'-' {
                    self.step = Step::ESign;
                } else {
                    self.e_sign(c);
                }
            }
            Step::ESign => self.e_sign(c),
            Step::E0 => {
                if !c.is_ascii_digit() {
                    self.end_value(c);
                }
            }
            Step::Literal(word, index) => {
                let expected = word.as_bytes()[index];
                if c == expected {
                    self.step = if index + 1 == word.len() {
                        Step::EndValue
                    } else {
                        Step::Literal(word, index + 1)
                    };
                } else {
                    self.error(
                        c,
                        &format!("in literal {word} (expecting '{}')", char::from(expected)),
                    );
                }
            }
            Step::Error => {}
        }
    }

    fn begin_value(&mut self, c: u8) {
        if is_space(c) {
            return;
        }
        match c {
            b'{' => {
                self.step = Step::BeginStringOrEmpty;
                self.push(c, Parse::ObjectKey);
            }
            b'[' => {
                self.step = Step::BeginValueOrEmpty;
                self.push(c, Parse::ArrayValue);
            }
            b'"' => self.step = Step::InString,
            b'-' => self.step = Step::Neg,
            b'0' => self.step = Step::Zero,
            b't' => self.step = Step::Literal("true", 1),
            b'f' => self.step = Step::Literal("false", 1),
            b'n' => self.step = Step::Literal("null", 1),
            b'1'..=b'9' => self.step = Step::One,
            _ => self.error(c, "looking for beginning of value"),
        }
    }

    fn begin_string(&mut self, c: u8) {
        if is_space(c) {
            return;
        }
        if c == b'"' {
            self.step = Step::InString;
        } else {
            self.error(c, "looking for beginning of object key string");
        }
    }

    fn zero(&mut self, c: u8) {
        match c {
            b'.' => self.step = Step::Dot,
            b'e' | b'E' => self.step = Step::E,
            _ => self.end_value(c),
        }
    }

    fn e_sign(&mut self, c: u8) {
        if c.is_ascii_digit() {
            self.step = Step::E0;
        } else {
            self.error(c, "in exponent of numeric literal");
        }
    }

    fn end_value(&mut self, c: u8) {
        let Some(&state) = self.parse.last() else {
            self.step = Step::EndTop;
            self.end_top = true;
            if !is_space(c) {
                self.error(c, "after top-level value");
            }
            return;
        };
        if is_space(c) {
            self.step = Step::EndValue;
            return;
        }
        match state {
            Parse::ObjectKey => {
                if c == b':' {
                    *self.parse.last_mut().expect("parse state") = Parse::ObjectValue;
                    self.step = Step::BeginValue;
                } else {
                    self.error(c, "after object key");
                }
            }
            Parse::ObjectValue => {
                if c == b',' {
                    *self.parse.last_mut().expect("parse state") = Parse::ObjectKey;
                    self.step = Step::BeginString;
                } else if c == b'}' {
                    self.pop();
                } else {
                    self.error(c, "after object key:value pair");
                }
            }
            Parse::ArrayValue => {
                if c == b',' {
                    self.step = Step::BeginValue;
                } else if c == b']' {
                    self.pop();
                } else {
                    self.error(c, "after array element");
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const DEVICE: Struct<'static> = Struct {
        name: "DeviceAuthorization",
        type_string: "login.DeviceAuthorization",
        fields: &[
            ("deviceCode", Kind::String),
            ("expiresIn", Kind::Int),
            ("userCode", Kind::String),
        ],
    };

    fn syntax(input: &str) -> String {
        check_valid(input.as_bytes()).unwrap_err()
    }

    #[test]
    fn syntax_errors_name_the_byte_and_what_was_expected() {
        assert_eq!(syntax(""), "unexpected end of JSON input");
        assert_eq!(syntax("  {\"a\": 1"), "unexpected end of JSON input");
        assert_eq!(
            syntax("<html>"),
            "invalid character '<' looking for beginning of value"
        );
        assert_eq!(
            syntax("not json"),
            "invalid character 'o' in literal null (expecting 'u')"
        );
        assert_eq!(
            syntax("{\"a\" 1}"),
            "invalid character '1' after object key"
        );
        assert_eq!(
            syntax("{\"a\": 1 \"b\"}"),
            "invalid character '\"' after object key:value pair"
        );
        assert_eq!(
            syntax("{'a': 1}"),
            r"invalid character '\'' looking for beginning of object key string"
        );
        assert_eq!(
            syntax("[1,]"),
            "invalid character ']' looking for beginning of value"
        );
        assert_eq!(
            syntax("{} x"),
            "invalid character 'x' after top-level value"
        );
        assert_eq!(
            syntax("1.x"),
            "invalid character 'x' after decimal point in numeric literal"
        );
        assert_eq!(
            syntax("\"a\nb\""),
            r"invalid character '\n' in string literal"
        );
        assert_eq!(
            syntax("\"\\q\""),
            "invalid character 'q' in string escape code"
        );
    }

    #[test]
    fn a_truncated_literal_is_reported_at_the_implied_trailing_space() {
        assert_eq!(
            syntax("tru"),
            "invalid character ' ' in literal true (expecting 'e')"
        );
        assert!(check_valid(b"12").is_ok());
        assert_eq!(syntax("-"), "invalid character ' ' in numeric literal");
    }

    #[test]
    fn keys_match_exactly_or_case_insensitively_and_the_last_one_wins() {
        let fields = unmarshal(
            br#"{"DEVICECODE": "a", "deviceCode": "b", "userCode": "u", "extra": [1, {"x": 2}]}"#,
            &DEVICE,
        )
        .unwrap();
        assert_eq!(
            fields,
            vec![
                Field::Str("b".into()),
                Field::Int(0),
                Field::Str("u".into())
            ]
        );
    }

    #[test]
    fn the_first_type_mismatch_is_reported_with_the_struct_and_field() {
        assert_eq!(
            unmarshal(br#"{"expiresIn": "soon", "userCode": 42}"#, &DEVICE).unwrap_err(),
            "json: cannot unmarshal string into Go struct field DeviceAuthorization.expiresIn of type int"
        );
        assert_eq!(
            unmarshal(br#"{"expiresIn": 1.5}"#, &DEVICE).unwrap_err(),
            "json: cannot unmarshal number 1.5 into Go struct field DeviceAuthorization.expiresIn of type int"
        );
        assert_eq!(
            unmarshal(br#"{"userCode": {"a": 1}}"#, &DEVICE).unwrap_err(),
            "json: cannot unmarshal object into Go struct field DeviceAuthorization.userCode of type string"
        );
    }

    #[test]
    fn a_document_that_is_not_an_object_names_the_go_type() {
        assert_eq!(
            unmarshal(b"[1]", &DEVICE).unwrap_err(),
            "json: cannot unmarshal array into Go value of type login.DeviceAuthorization"
        );
        assert_eq!(
            unmarshal(b"null", &DEVICE).unwrap(),
            vec![
                Field::Str(String::new()),
                Field::Int(0),
                Field::Str(String::new())
            ]
        );
    }

    const PACKAGE: Struct<'static> = Struct {
        name: "",
        type_string: "struct { ... }",
        fields: &[("dependencies", Kind::StringMap)],
    };

    #[test]
    fn a_repeated_map_merges_and_a_non_string_element_is_a_mismatch_that_still_sets_its_key() {
        assert_eq!(
            unmarshal(
                br#"{"dependencies": {"a": "1"}, "Dependencies": {"b": null, "a": "2"}}"#,
                &PACKAGE
            )
            .unwrap(),
            vec![Field::Map(vec!["a".into(), "b".into()])]
        );
        assert_eq!(
            unmarshal(br#"{"dependencies": {"a": 1, "b": "x"}}"#, &PACKAGE).unwrap_err(),
            "json: cannot unmarshal number into Go struct field .dependencies of type string"
        );
        assert_eq!(
            unmarshal(br#"{"dependencies": [1]}"#, &PACKAGE).unwrap_err(),
            "json: cannot unmarshal array into Go struct field .dependencies of type map[string]string"
        );
        assert_eq!(
            unmarshal(b"null", &PACKAGE).unwrap(),
            vec![Field::Map(vec![])]
        );
    }

    #[test]
    fn escapes_and_invalid_utf8_decode_like_go() {
        let fields = unmarshal(
            b"{\"deviceCode\": \"a\\u00e9\\ud83d\\ude00\\ud800x\xe2\x82\"}",
            &DEVICE,
        )
        .unwrap();
        assert_eq!(
            fields[0],
            Field::Str("aé😀\u{fffd}x\u{fffd}\u{fffd}".into())
        );
    }
}
