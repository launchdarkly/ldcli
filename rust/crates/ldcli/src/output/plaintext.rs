//! The plaintext forms for a resource and for an error.
//!
//! A port of `internal/output/plaintext_fns.go`.

use super::columns::default_format;
use serde_json::{Map, Value};

fn text(map: &Map<String, Value>, key: &str) -> Option<String> {
    match map.get(key) {
        None | Some(Value::Null) => None,
        Some(value) => Some(default_format(Some(value))),
    }
}

/// A resource named by whichever pair of fields it has.
pub fn singular(resource: &Map<String, Value>) -> String {
    let email = text(resource, "email");
    let id = text(resource, "_id");
    let key = text(resource, "key");
    let name = text(resource, "name");

    match (&name, &key, &email, &id) {
        (Some(name), Some(key), _, _) => format!("{name} ({key})"),
        (_, _, Some(email), Some(id)) => format!("{email} ({id})"),
        (Some(name), _, _, Some(id)) => format!("{name} ({id})"),
        (_, Some(key), _, _) => key.clone(),
        (_, _, Some(email), _) => email.clone(),
        (_, _, _, Some(id)) => id.clone(),
        (Some(name), _, _, _) => name.clone(),
        _ => "cannot read resource".to_string(),
    }
}

pub fn multiple(resource: &Map<String, Value>) -> String {
    format!("* {}", singular(resource))
}

/// An error body may carry a code, a message, both, or neither.
pub fn error(resource: &Map<String, Value>) -> String {
    let code = resource.get("code").filter(|v| !v.is_null());
    let message = resource.get("message").filter(|v| !v.is_null());
    let message_is_empty = matches!(message, Some(Value::String(s)) if s.is_empty());

    let mut out = match (code, message) {
        (None, None) => "unknown error occurred".to_string(),
        (None, Some(_)) if message_is_empty => "unknown error occurred".to_string(),
        (None, Some(message)) => default_format(Some(message)),
        (Some(code), _) if message_is_empty => {
            format!("an error occurred (code: {})", default_format(Some(code)))
        }
        (Some(code), message) => format!(
            "{} (code: {})",
            default_format(message),
            default_format(Some(code))
        ),
    };

    if let Some(suggestion) = resource.get("suggestion") {
        let rendered = default_format(Some(suggestion));
        if !suggestion.is_null() && !rendered.is_empty() {
            out.push_str(&format!("\nSuggestion: {rendered}"));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn map(value: Value) -> Map<String, Value> {
        value.as_object().cloned().unwrap()
    }

    #[test]
    fn a_resource_is_named_by_the_fields_it_has() {
        assert_eq!(singular(&map(json!({"name": "n", "key": "k"}))), "n (k)");
        assert_eq!(singular(&map(json!({"email": "e", "_id": "i"}))), "e (i)");
        assert_eq!(singular(&map(json!({"name": "n", "_id": "i"}))), "n (i)");
        assert_eq!(singular(&map(json!({"key": "k"}))), "k");
        assert_eq!(singular(&map(json!({"email": "e"}))), "e");
        assert_eq!(singular(&map(json!({"_id": "i"}))), "i");
        assert_eq!(singular(&map(json!({"name": "n"}))), "n");
        assert_eq!(
            singular(&map(json!({"color": "blue"}))),
            "cannot read resource"
        );
        assert_eq!(multiple(&map(json!({"key": "k"}))), "* k");
    }

    #[test]
    fn an_error_reads_differently_for_each_combination_of_code_and_message() {
        assert_eq!(error(&map(json!({}))), "unknown error occurred");
        assert_eq!(
            error(&map(json!({"message": ""}))),
            "unknown error occurred"
        );
        assert_eq!(error(&map(json!({"message": "boom"}))), "boom");
        assert_eq!(
            error(&map(json!({"code": "conflict", "message": ""}))),
            "an error occurred (code: conflict)"
        );
        assert_eq!(
            error(&map(
                json!({"code": "not_found", "message": "unknown flag"})
            )),
            "unknown flag (code: not_found)"
        );
    }

    #[test]
    fn a_suggestion_is_appended_on_its_own_line_when_it_has_content() {
        assert_eq!(
            error(&map(
                json!({"code": "c", "message": "m", "suggestion": "Try this."})
            )),
            "m (code: c)\nSuggestion: Try this."
        );
        assert_eq!(error(&map(json!({"message": "m", "suggestion": ""}))), "m");
        assert_eq!(
            error(&map(json!({"message": "m", "suggestion": null}))),
            "m"
        );
    }
}
