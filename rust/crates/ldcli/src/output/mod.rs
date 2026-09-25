//! Rendering a command's response.
//!
//! A port of `internal/output`. The shape is the same: JSON output is the
//! response body passed through untouched, plaintext and markdown are built
//! from the decoded body, and `--fields` filters only the JSON form.
//!
//! One thing deliberately does not match. When the body is not JSON at all,
//! Go returns the error text from `encoding/json`, which is internal to that
//! package. This returns its own message. The text surfaces only through
//! `cmd_output_error`, and no fixture depends on it.

pub mod columns;
pub mod gojson;
pub mod markdown;
pub mod plaintext;
pub mod table;

use serde_json::{Map, Value};

pub const INVALID_OUTPUT_KIND: &str = "output is invalid. Use 'json', 'plaintext', or 'markdown'";

/// The note Go writes to stderr when `--fields` is given without JSON output.
pub const FIELDS_IGNORED_NOTE: &str = "note: --fields is only supported with JSON output; ignoring";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OutputKind {
    Json,
    Plaintext,
    Markdown,
}

impl OutputKind {
    /// `NewOutputKind`: an exact match, so `JSON` and a padded value are both
    /// rejected.
    pub fn parse(value: &str) -> Result<Self, &'static str> {
        match value {
            "json" => Ok(Self::Json),
            "plaintext" => Ok(Self::Plaintext),
            "markdown" => Ok(Self::Markdown),
            _ => Err(INVALID_OUTPUT_KIND),
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Json => "json",
            Self::Plaintext => "plaintext",
            Self::Markdown => "markdown",
        }
    }
}

#[derive(Debug, Default, Clone)]
pub struct CmdOutputOpts {
    pub fields: Vec<String>,
    pub resource_name: String,
}

/// What a render produced: the text, plus the note that belongs on stderr.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Rendered {
    pub stdout: String,
    pub stderr: String,
}

/// A decoded response: one resource, or a list of them.
struct Decoded {
    resource: Map<String, Value>,
    items: Vec<Map<String, Value>>,
    links: Map<String, Value>,
    total_count: i64,
    is_multiple: bool,
}

/// Render a response body.
///
/// `kind` is the raw flag value rather than an `OutputKind`, because
/// `CmdOutput` does not validate it: anything that is not `json` or
/// `markdown` renders as plaintext. `OutputKind::parse` is what rejects a bad
/// value, at the point the flag is read.
pub fn cmd_output(
    action: &str,
    kind: &str,
    input: &str,
    opts: &CmdOutputOpts,
) -> Result<Rendered, String> {
    let fields: Vec<&str> = opts
        .fields
        .iter()
        .map(|field| field.trim())
        .filter(|field| !field.is_empty())
        .collect();

    if kind == "json" {
        let stdout = if fields.is_empty() {
            input.to_string()
        } else {
            // A body that will not parse is passed through unfiltered.
            filter_fields(input, &fields).unwrap_or_else(|| input.to_string())
        };
        return Ok(Rendered {
            stdout,
            stderr: String::new(),
        });
    }

    let stderr = if fields.is_empty() {
        String::new()
    } else {
        format!("{FIELDS_IGNORED_NOTE}\n")
    };

    let decoded = decode(input)?;
    let success_message = match action {
        "create" => "Successfully created",
        "delete" => "Successfully deleted",
        "update" => "Successfully updated",
        _ => "",
    };

    let stdout = if kind == "markdown" {
        markdown_output(&decoded, &opts.resource_name, success_message)
    } else {
        plaintext_output(&decoded, &opts.resource_name, success_message)
    };
    Ok(Rendered { stdout, stderr })
}

/// A body is a single resource, a list of resources, or a list of scalars.
fn decode(input: &str) -> Result<Decoded, String> {
    let parsed = gojson::decode(input).map_err(|_| "response is not valid JSON".to_string())?;
    let Some(object) = parsed.as_object() else {
        return Err("response is not a JSON object".to_string());
    };

    if !object.contains_key("items") {
        return Ok(Decoded {
            resource: object.clone(),
            items: Vec::new(),
            links: Map::new(),
            total_count: 0,
            is_multiple: false,
        });
    }

    let links = match object.get("_links") {
        Some(Value::Object(links)) => links.clone(),
        _ => Map::new(),
    };
    let total_count = object
        .get("totalCount")
        .and_then(Value::as_f64)
        .map(|n| n as i64)
        .unwrap_or_default();

    let mut items = Vec::new();
    if let Some(Value::Array(raw)) = object.get("items") {
        for entry in raw {
            match entry {
                Value::Object(map) => items.push(map.clone()),
                // A list of scalars becomes a list of resources keyed by value.
                scalar => {
                    let mut map = Map::new();
                    map.insert("key".to_string(), scalar.clone());
                    items.push(map);
                }
            }
        }
    }

    Ok(Decoded {
        resource: object.clone(),
        items,
        links,
        total_count,
        is_multiple: true,
    })
}

fn plaintext_output(decoded: &Decoded, resource_name: &str, success_message: &str) -> String {
    if !decoded.is_multiple {
        if let Some(cols) = columns::singular_columns(resource_name) {
            let body = table::key_value_output(&decoded.resource, cols);
            return if success_message.is_empty() {
                body
            } else {
                format!("{success_message}\n\n{body}")
            };
        }
        // Note the trailing space: the singular form is prefixed inline.
        return prefix(
            &plaintext::singular(&decoded.resource),
            &format!("{success_message} "),
        );
    }

    if decoded.items.is_empty() {
        return "No items found".to_string();
    }

    let body = match columns::list_columns(resource_name) {
        Some(cols) => table::table_output(&decoded.items, cols),
        None => decoded
            .items
            .iter()
            .map(plaintext::multiple)
            .collect::<Vec<_>>()
            .join("\n"),
    };

    let message = if success_message.is_empty() {
        String::new()
    } else {
        format!("{success_message}\n")
    };
    format!(
        "{}{}",
        prefix(&body, &message),
        pagination_suffix(&decoded.links, decoded.total_count)
    )
}

fn markdown_output(decoded: &Decoded, resource_name: &str, success_message: &str) -> String {
    if !decoded.is_multiple {
        let body = markdown::singular_output(&decoded.resource, resource_name);
        return if success_message.is_empty() {
            body
        } else {
            format!("{success_message}\n\n{body}")
        };
    }
    if decoded.items.is_empty() {
        return "No items found".to_string();
    }
    let body = markdown::multiple_output(&decoded.items, resource_name);
    let pagination = pagination_suffix(&decoded.links, decoded.total_count);
    if success_message.is_empty() {
        format!("{body}{pagination}")
    } else {
        format!("{success_message}\n\n{body}{pagination}")
    }
}

/// Go only prefixes when the message has content beyond whitespace.
fn prefix(body: &str, success_message: &str) -> String {
    if success_message.trim().is_empty() {
        body.to_string()
    } else {
        format!("{success_message}{body}")
    }
}

fn pagination_suffix(links: &Map<String, Value>, total_count: i64) -> String {
    let Some(Value::Object(self_link)) = links.get("self") else {
        return String::new();
    };
    if total_count <= 0 {
        return String::new();
    }
    let href = match self_link.get("href") {
        Some(Value::String(href)) => href.as_str(),
        _ => "",
    };
    let limit = query_int(href, "limit");
    let offset = query_int(href, "offset");
    let mut max_results = (offset + limit).min(total_count);
    if max_results == 0 {
        max_results = total_count;
    }
    let mut pagination = format!(
        "\nShowing results {} - {max_results} of {total_count}.",
        offset + 1
    );
    if max_results < total_count {
        pagination.push_str(&format!(
            " Use --offset {} for additional results.",
            offset + limit
        ));
    }
    pagination
}

/// Go parses the link and reads one query parameter; a missing or unparsable
/// value is zero.
fn query_int(href: &str, name: &str) -> i64 {
    let Some((_, query)) = href.split_once('?') else {
        return 0;
    };
    query
        .split('&')
        .filter_map(|pair| pair.split_once('='))
        .find(|(key, _)| *key == name)
        .and_then(|(_, value)| value.parse::<i64>().ok())
        .unwrap_or(0)
}

fn filter_fields(input: &str, fields: &[&str]) -> Option<String> {
    // Go decodes into interface{} before re-marshaling, so numbers make the
    // same float64 round trip here.
    let parsed = gojson::decode(input).ok()?;
    let object = parsed.as_object()?;

    if let Some(Value::Array(items)) = object.get("items") {
        let filtered: Vec<Value> = items
            .iter()
            .map(|item| match item.as_object() {
                Some(map) => Value::Object(filter_map(map, fields)),
                None => item.clone(),
            })
            .collect();
        let mut result = Map::new();
        result.insert("items".to_string(), Value::Array(filtered));
        for key in ["totalCount", "_links"] {
            if let Some(value) = object.get(key) {
                result.insert(key.to_string(), value.clone());
            }
        }
        return Some(gojson::marshal_indent(&Value::Object(result), "  "));
    }

    Some(gojson::marshal_indent(
        &Value::Object(filter_map(object, fields)),
        "  ",
    ))
}

fn filter_map(map: &Map<String, Value>, fields: &[&str]) -> Map<String, Value> {
    map.iter()
        .filter(|(key, _)| fields.contains(&key.as_str()))
        .map(|(key, value)| (key.clone(), value.clone()))
        .collect()
}

/// Where an error came from, which decides how it is rendered.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CmdError {
    /// A response body from the API, already JSON.
    ApiBody(String),
    /// An error the CLI raised itself, carrying a plain sentence.
    Cli(String),
    /// A body that could not be decoded into the expected shape.
    InvalidJson,
    /// Anything else, such as a transport failure.
    Other(String),
}

impl CmdError {
    /// `NewLDAPIError`: a 401 has no body, so the client makes one.
    pub fn unauthorized() -> Self {
        Self::ApiBody(
            r#"{"code":"unauthorized","message":"You do not have access to perform this action"}"#
                .to_string(),
        )
    }
}

/// Render an error. Plaintext and markdown share one form.
pub fn cmd_output_error(kind: &str, error: &CmdError) -> String {
    let text = match error {
        CmdError::InvalidJson => err_json("invalid JSON"),
        CmdError::ApiBody(body) => body.clone(),
        CmdError::Cli(message) => message.clone(),
        CmdError::Other(message) => err_json(message),
    };

    // Go decodes whatever it built; text that is not an object leaves a nil
    // map behind, which marshals back to `null`.
    let decoded: Option<Map<String, Value>> = gojson::decode(&text)
        .ok()
        .and_then(|value| value.as_object().cloned());

    if kind == "json" {
        return match &decoded {
            Some(map) => gojson::marshal(&Value::Object(map.clone())),
            None => "null".to_string(),
        };
    }
    plaintext::error(&decoded.unwrap_or_default())
}

fn err_json(message: &str) -> String {
    format!(
        "{{\n\t\t\t\"message\": {}\n\t\t}}",
        gojson::marshal(&Value::String(message.to_string()))
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_kind_is_matched_exactly() {
        assert_eq!(OutputKind::parse("json"), Ok(OutputKind::Json));
        assert_eq!(OutputKind::parse("markdown"), Ok(OutputKind::Markdown));
        assert_eq!(OutputKind::parse("JSON"), Err(INVALID_OUTPUT_KIND));
        assert_eq!(OutputKind::parse(" json"), Err(INVALID_OUTPUT_KIND));
        assert_eq!(OutputKind::parse(""), Err(INVALID_OUTPUT_KIND));
    }

    #[test]
    fn json_output_is_the_body_untouched() {
        let input = "{\n  \"key\":   \"value\"\n}";
        let rendered = cmd_output("get", "json", input, &CmdOutputOpts::default()).unwrap();
        assert_eq!(rendered.stdout, input);
        assert!(rendered.stderr.is_empty());
    }

    #[test]
    fn fields_filter_json_and_only_note_on_the_other_kinds() {
        let opts = CmdOutputOpts {
            fields: vec!["key".to_string()],
            resource_name: String::new(),
        };
        let input = r#"{"items":[{"key":"a","name":"n"}],"totalCount":1}"#;
        let json = cmd_output("list", "json", input, &opts).unwrap();
        assert_eq!(
            json.stdout,
            "{\n  \"items\": [\n    {\n      \"key\": \"a\"\n    }\n  ],\n  \"totalCount\": 1\n}"
        );
        assert!(json.stderr.is_empty());

        let plaintext = cmd_output("list", "plaintext", input, &opts).unwrap();
        assert_eq!(plaintext.stderr, format!("{FIELDS_IGNORED_NOTE}\n"));
        assert_eq!(plaintext.stdout, "* n (a)");
    }

    #[test]
    fn a_blank_field_list_is_the_same_as_none() {
        let opts = CmdOutputOpts {
            fields: vec!["".to_string(), "   ".to_string()],
            resource_name: String::new(),
        };
        let input = r#"{"items":[{"key":"a"}]}"#;
        let rendered = cmd_output("list", "json", input, &opts).unwrap();
        assert_eq!(rendered.stdout, input);
        assert!(rendered.stderr.is_empty());
    }

    #[test]
    fn an_unknown_kind_renders_as_plaintext_because_cmd_output_does_not_validate() {
        let rendered = cmd_output(
            "list",
            "yaml",
            r#"{"items":[{"key":"a"}]}"#,
            &CmdOutputOpts::default(),
        )
        .unwrap();
        assert_eq!(rendered.stdout, "* a");
    }

    #[test]
    fn pagination_appears_only_with_a_self_link_and_a_total() {
        let with_link = r#"{"_links":{"self":{"href":"/x?limit=5&offset=5"}},"items":[{"key":"a"}],"totalCount":100}"#;
        let rendered =
            cmd_output("list", "plaintext", with_link, &CmdOutputOpts::default()).unwrap();
        assert_eq!(
            rendered.stdout,
            "* a\nShowing results 6 - 10 of 100. Use --offset 10 for additional results."
        );

        let no_total =
            r#"{"_links":{"self":{"href":"/x?limit=5&offset=0"}},"items":[{"key":"a"}]}"#;
        let rendered =
            cmd_output("list", "plaintext", no_total, &CmdOutputOpts::default()).unwrap();
        assert_eq!(rendered.stdout, "* a");
    }

    #[test]
    fn an_empty_list_says_so_and_a_body_that_is_not_json_is_an_error() {
        let rendered = cmd_output(
            "list",
            "plaintext",
            r#"{"items":[]}"#,
            &CmdOutputOpts::default(),
        )
        .unwrap();
        assert_eq!(rendered.stdout, "No items found");
        assert!(cmd_output("list", "plaintext", "not json", &CmdOutputOpts::default()).is_err());
    }

    #[test]
    fn an_error_without_a_json_body_marshals_to_null_in_json() {
        let error = CmdError::Cli(INVALID_OUTPUT_KIND.into());
        assert_eq!(cmd_output_error("json", &error), "null");
        assert_eq!(
            cmd_output_error("plaintext", &error),
            "unknown error occurred"
        );
    }

    #[test]
    fn a_401_is_normalized_into_a_body_the_cli_wrote() {
        let error = CmdError::unauthorized();
        assert_eq!(
            cmd_output_error("json", &error),
            r#"{"code":"unauthorized","message":"You do not have access to perform this action"}"#
        );
        assert_eq!(
            cmd_output_error("plaintext", &error),
            "You do not have access to perform this action (code: unauthorized)"
        );
    }
}
