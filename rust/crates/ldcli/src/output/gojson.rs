//! Decode and serialize JSON the way Go's `encoding/json` does.
//!
//! Three details of `json.Marshal` are visible in CLI output and none of them
//! are serde's defaults: `<`, `>`, and `&` are escaped as `\u003c`, `\u003e`,
//! and `\u0026`; map keys come out sorted; and a float that happens to be
//! integral prints without a fractional part. The `--fields` path and the JSON
//! error path both go through here.

use serde_json::{Map, Value};

/// Decode the way Go decodes into `interface{}`: every number becomes an
/// `f64`. That is lossy past 2^53, and the loss is visible when the value is
/// written back out, so the port has to decode the same way to match.
pub fn decode(input: &str) -> Result<Value, serde_json::Error> {
    Ok(as_float64(serde_json::from_str(input)?))
}

fn as_float64(value: Value) -> Value {
    match value {
        Value::Number(n) => n
            .as_f64()
            .and_then(serde_json::Number::from_f64)
            .map(Value::Number)
            .unwrap_or(Value::Null),
        Value::Array(items) => Value::Array(items.into_iter().map(as_float64).collect()),
        Value::Object(map) => Value::Object(
            map.into_iter()
                .map(|(key, value)| (key, as_float64(value)))
                .collect(),
        ),
        other => other,
    }
}

/// `fmt.Sprint` of a float64, which is `%g` with the shortest digits that
/// round-trip. This is what the plaintext and markdown paths print, and it is
/// not the same as the JSON form: 1418684722483 reads as 1.418684722483e+12.
pub fn sprint_float(f: f64) -> String {
    if f == 0.0 {
        return if f.is_sign_negative() { "-0" } else { "0" }.to_string();
    }
    if f.is_nan() {
        return "NaN".to_string();
    }
    if f.is_infinite() {
        return if f > 0.0 { "+Inf" } else { "-Inf" }.to_string();
    }

    // Rust's LowerExp is the same shortest round-trip form Go starts from.
    let scientific = format!("{f:e}");
    let (mantissa, exponent) = scientific
        .split_once('e')
        .unwrap_or((scientific.as_str(), "0"));
    let exponent: i32 = exponent.parse().unwrap_or(0);

    // Go switches to exponent form outside this range when no precision was
    // requested, and always writes at least two exponent digits.
    if !(-4..6).contains(&exponent) {
        let sign = if exponent < 0 { '-' } else { '+' };
        return format!("{mantissa}e{sign}{:02}", exponent.abs());
    }
    format!("{f}")
}

/// `json.Marshal`: no whitespace.
pub fn marshal(value: &Value) -> String {
    let mut out = String::new();
    write_value(&mut out, value, "", None);
    out
}

/// `json.MarshalIndent(v, "", indent)`.
pub fn marshal_indent(value: &Value, indent: &str) -> String {
    let mut out = String::new();
    write_value(&mut out, value, "", Some(indent));
    out
}

fn write_value(out: &mut String, value: &Value, current: &str, indent: Option<&str>) {
    match value {
        Value::Null => out.push_str("null"),
        Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
        Value::Number(n) => out.push_str(&number(n)),
        Value::String(s) => write_string(out, s),
        Value::Array(items) => write_array(out, items, current, indent),
        Value::Object(map) => write_object(out, map, current, indent),
    }
}

fn write_array(out: &mut String, items: &[Value], current: &str, indent: Option<&str>) {
    if items.is_empty() {
        out.push_str("[]");
        return;
    }
    out.push('[');
    let inner = push_indent(current, indent);
    for (i, item) in items.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        newline(out, &inner, indent);
        write_value(out, item, &inner, indent);
    }
    newline(out, current, indent);
    out.push(']');
}

fn write_object(out: &mut String, map: &Map<String, Value>, current: &str, indent: Option<&str>) {
    if map.is_empty() {
        out.push_str("{}");
        return;
    }
    // Go sorts map keys. serde_json's default map is already ordered, but
    // sorting here keeps that independent of its feature flags.
    let mut keys: Vec<&String> = map.keys().collect();
    keys.sort();

    out.push('{');
    let inner = push_indent(current, indent);
    for (i, key) in keys.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        newline(out, &inner, indent);
        write_string(out, key);
        out.push(':');
        if indent.is_some() {
            out.push(' ');
        }
        if let Some(child) = map.get(*key) {
            write_value(out, child, &inner, indent);
        }
    }
    newline(out, current, indent);
    out.push('}');
}

fn push_indent(current: &str, indent: Option<&str>) -> String {
    match indent {
        Some(unit) => format!("{current}{unit}"),
        None => String::new(),
    }
}

fn newline(out: &mut String, current: &str, indent: Option<&str>) {
    if indent.is_some() {
        out.push('\n');
        out.push_str(current);
    }
}

fn write_string(out: &mut String, s: &str) {
    out.push('"');
    for ch in s.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            // Go escapes these so the output is safe to embed in HTML.
            '<' => out.push_str("\\u003c"),
            '>' => out.push_str("\\u003e"),
            '&' => out.push_str("\\u0026"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
}

/// Go writes a float with the shortest representation that round-trips, and
/// switches to exponent form outside `[1e-6, 1e21)`.
fn number(n: &serde_json::Number) -> String {
    if let Some(i) = n.as_i64() {
        return i.to_string();
    }
    if let Some(u) = n.as_u64() {
        return u.to_string();
    }
    let Some(f) = n.as_f64() else {
        return n.to_string();
    };
    let abs = f.abs();
    if abs != 0.0 && !(1e-6..1e21).contains(&abs) {
        let formatted = format!("{f:e}");
        // Rust writes `1e21`; Go writes `1e+21`.
        return match formatted.split_once('e') {
            Some((mantissa, exponent)) if !exponent.starts_with('-') => {
                format!("{mantissa}e+{exponent}")
            }
            _ => formatted,
        };
    }
    // Display already gives the shortest round-trip form, and prints an
    // integral float without a fractional part.
    format!("{f}")
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn ampersands_and_angle_brackets_are_escaped_like_go() {
        let value = json!({"href": "/api/v2/flags?limit=2&offset=0", "tag": "<b>"});
        assert_eq!(
            marshal(&value),
            r#"{"href":"/api/v2/flags?limit=2\u0026offset=0","tag":"\u003cb\u003e"}"#
        );
    }

    #[test]
    fn keys_are_sorted_and_indentation_matches_two_spaces() {
        let value = json!({"b": 1, "a": {"d": [1, 2], "c": true}});
        assert_eq!(
            marshal_indent(&value, "  "),
            "{\n  \"a\": {\n    \"c\": true,\n    \"d\": [\n      1,\n      2\n    ]\n  },\n  \"b\": 1\n}"
        );
    }

    #[test]
    fn empty_containers_stay_on_one_line() {
        assert_eq!(
            marshal_indent(&json!({"a": [], "b": {}}), "  "),
            "{\n  \"a\": [],\n  \"b\": {}\n}"
        );
        assert_eq!(marshal(&json!(null)), "null");
    }

    #[test]
    fn an_integral_float_prints_without_a_fraction() {
        let value: Value =
            serde_json::from_str("{\"a\":1.0,\"b\":1.5,\"c\":1418684722483}").unwrap();
        assert_eq!(marshal(&value), r#"{"a":1,"b":1.5,"c":1418684722483}"#);
    }

    #[test]
    fn very_large_and_very_small_numbers_use_exponent_form() {
        // Go writes a positive exponent with a sign and strips the leading
        // zero from a negative one, so 1e-07 comes out as 1e-7.
        let value: Value = serde_json::from_str("[1e21,1e-7,1e-9,1.5e-7,1e-15]").unwrap();
        assert_eq!(marshal(&value), "[1e+21,1e-7,1e-9,1.5e-7,1e-15]");
    }

    #[test]
    fn decoding_loses_precision_past_two_to_the_fifty_third_like_go() {
        let value = decode("9007199254740993").unwrap();
        assert_eq!(marshal(&value), "9007199254740992");
    }
}
