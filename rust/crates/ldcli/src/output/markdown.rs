//! Markdown rendering.
//!
//! A port of `internal/output/markdown.go`. Flags get a hand-written view with
//! an environment table; every other resource uses the column registry or a
//! bare heading.

use super::columns::{column_value, default_format, list_columns, singular_columns, ColumnDef};
use super::plaintext;
use serde_json::{Map, Value};

fn escape_pipe(text: &str) -> String {
    text.replace('|', "\\|")
}

pub fn table_output(items: &[Map<String, Value>], columns: &[ColumnDef]) -> String {
    let headers: Vec<&str> = columns.iter().map(|c| c.header).collect();
    let separators = vec!["---"; columns.len()];
    let mut out = format!(
        "| {} |\n| {} |",
        headers.join(" | "),
        separators.join(" | ")
    );
    for item in items {
        let cells: Vec<String> = columns
            .iter()
            .map(|column| escape_pipe(&column_value(Some(item), column)))
            .collect();
        out.push_str(&format!("\n| {} |", cells.join(" | ")));
    }
    out
}

pub fn key_value_output(resource: &Map<String, Value>, columns: &[ColumnDef]) -> String {
    columns
        .iter()
        .map(|column| {
            format!(
                "- **{}:** {}",
                column.header,
                column_value(Some(resource), column)
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

pub fn singular_output(resource: &Map<String, Value>, resource_name: &str) -> String {
    if resource_name == "flags" {
        return flag_output(resource);
    }
    let heading = heading(resource);
    match singular_columns(resource_name) {
        Some(columns) => format!("{heading}\n\n{}", key_value_output(resource, columns)),
        None => heading,
    }
}

pub fn multiple_output(items: &[Map<String, Value>], resource_name: &str) -> String {
    if let Some(columns) = list_columns(resource_name) {
        return table_output(items, columns);
    }
    items
        .iter()
        .map(|item| format!("- {}", plaintext::singular(item)))
        .collect::<Vec<_>>()
        .join("\n")
}

fn flag_output(resource: &Map<String, Value>) -> String {
    let mut out = format!("## {}", default_format(resource.get("key")));

    if let Some(description) = resource.get("description") {
        let text = default_format(Some(description));
        if !description.is_null() && !text.is_empty() {
            out.push_str(&format!("\n\n{text}"));
        }
    }

    let environments = environment_table(resource);
    if !environments.is_empty() {
        out.push_str(&format!("\n\n{environments}"));
    }

    let metadata = flag_metadata(resource);
    if !metadata.is_empty() {
        out.push_str(&format!("\n\n{metadata}"));
    }
    out
}

fn environment_table(resource: &Map<String, Value>) -> String {
    let Some(Value::Object(environments)) = resource.get("environments") else {
        return String::new();
    };
    if environments.is_empty() {
        return String::new();
    }
    let variations = variations(resource);

    let mut keys: Vec<&String> = environments.keys().collect();
    keys.sort();

    let mut out =
        String::from("| Environment | Status | Fallthrough | Rules |\n| --- | --- | --- | --- |");
    for key in keys {
        let Some(Value::Object(environment)) = environments.get(key) else {
            continue;
        };
        let status = match environment.get("on") {
            Some(Value::Bool(true)) => "ON",
            _ => "OFF",
        };
        let rules = match environment.get("rules") {
            Some(Value::Array(rules)) => rules.len(),
            _ => 0,
        };
        out.push_str(&format!(
            "\n| {} | {status} | {} | {rules} |",
            escape_pipe(key),
            escape_pipe(&resolve_fallthrough(environment, &variations))
        ));
    }
    out
}

struct Variation {
    name: String,
    value: Value,
}

fn variations(resource: &Map<String, Value>) -> Vec<Variation> {
    let Some(Value::Array(raw)) = resource.get("variations") else {
        return Vec::new();
    };
    raw.iter()
        .filter_map(|entry| entry.as_object())
        .map(|entry| Variation {
            name: match entry.get("name") {
                Some(Value::String(name)) => name.clone(),
                _ => String::new(),
            },
            value: entry.get("value").cloned().unwrap_or(Value::Null),
        })
        .collect()
}

fn resolve_fallthrough(environment: &Map<String, Value>, variations: &[Variation]) -> String {
    let Some(Value::Object(fallthrough)) = environment.get("fallthrough") else {
        return String::new();
    };
    let Some(index) = fallthrough.get("variation").and_then(Value::as_f64) else {
        return String::new();
    };
    let index = index as i64;
    if index < 0 || index as usize >= variations.len() {
        return format!("variation {index}");
    }
    let variation = &variations[index as usize];
    let value = go_print(&variation.value);
    if variation.name.is_empty() {
        value
    } else {
        format!("{} ({value})", variation.name)
    }
}

/// `%v` of a decoded JSON value, which is what the Go template interpolates.
fn go_print(value: &Value) -> String {
    match value {
        Value::Null => "<nil>".to_string(),
        other => default_format(Some(other)),
    }
}

fn flag_metadata(resource: &Map<String, Value>) -> String {
    let mut lines = Vec::new();
    if let Some(kind) = resource.get("kind").filter(|kind| !kind.is_null()) {
        lines.push(format!("- **Kind:** {}", default_format(Some(kind))));
    }
    if let Some(Value::Bool(temporary)) = resource.get("temporary") {
        let text = if *temporary { "yes" } else { "no" };
        lines.push(format!("- **Temporary:** {text}"));
    }
    if let Some(Value::Array(tags)) = resource.get("tags") {
        if !tags.is_empty() {
            let rendered: Vec<String> = tags.iter().map(|t| default_format(Some(t))).collect();
            lines.push(format!("- **Tags:** {}", rendered.join(", ")));
        }
    }
    let maintainer = maintainer(resource);
    if !maintainer.is_empty() {
        lines.push(format!("- **Maintainer:** {maintainer}"));
    }
    lines.join("\n")
}

fn maintainer(resource: &Map<String, Value>) -> String {
    let Some(Value::Object(maintainer)) = resource.get("_maintainer") else {
        return String::new();
    };
    for field in ["name", "email"] {
        if let Some(Value::String(value)) = maintainer.get(field) {
            if !value.is_empty() {
                return value.clone();
            }
        }
    }
    String::new()
}

fn heading(resource: &Map<String, Value>) -> String {
    let key = resource.get("key").filter(|v| !v.is_null());
    let name = resource.get("name").filter(|v| !v.is_null());
    match (name, key) {
        (Some(name), Some(key)) => format!(
            "## {} ({})",
            default_format(Some(name)),
            default_format(Some(key))
        ),
        (Some(name), None) => format!("## {}", default_format(Some(name))),
        (None, Some(key)) => format!("## {}", default_format(Some(key))),
        (None, None) => "## (unknown)".to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn map(value: Value) -> Map<String, Value> {
        value.as_object().cloned().unwrap()
    }

    #[test]
    fn a_pipe_in_a_cell_is_escaped() {
        let items = vec![map(json!({"key": "a|b", "name": "n", "color": "c"}))];
        let table = table_output(&items, list_columns("environments").unwrap());
        assert!(table.contains("| a\\|b |"), "{table}");
    }

    #[test]
    fn a_heading_uses_whichever_of_name_and_key_is_present() {
        assert_eq!(heading(&map(json!({"name": "n", "key": "k"}))), "## n (k)");
        assert_eq!(heading(&map(json!({"name": "n"}))), "## n");
        assert_eq!(heading(&map(json!({"key": "k"}))), "## k");
        assert_eq!(heading(&map(json!({}))), "## (unknown)");
    }

    #[test]
    fn a_fallthrough_names_its_variation_when_the_variation_has_a_name() {
        let flag = map(json!({
            "variations": [{"name": "on", "value": true}, {"value": false}],
            "environments": {
                "production": {"on": true, "fallthrough": {"variation": 0}, "rules": [{}, {}]},
                "test": {"on": false, "fallthrough": {"variation": 1}, "rules": []},
                "staging": {"on": true, "fallthrough": {"variation": 9}}
            }
        }));
        let table = environment_table(&flag);
        assert!(
            table.contains("| production | ON | on (true) | 2 |"),
            "{table}"
        );
        assert!(table.contains("| test | OFF | false | 0 |"), "{table}");
        // An index past the end is reported rather than resolved.
        assert!(
            table.contains("| staging | ON | variation 9 | 0 |"),
            "{table}"
        );
    }

    #[test]
    fn metadata_lists_only_the_fields_that_are_present() {
        let flag = map(json!({"kind": "boolean", "temporary": false}));
        assert_eq!(
            flag_metadata(&flag),
            "- **Kind:** boolean\n- **Temporary:** no"
        );
        let with_maintainer = map(json!({"_maintainer": {"email": "ada@example.com"}}));
        assert_eq!(
            flag_metadata(&with_maintainer),
            "- **Maintainer:** ada@example.com"
        );
    }
}
