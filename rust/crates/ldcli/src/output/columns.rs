//! Which columns each resource shows, and how each cell is formatted.
//!
//! A port of the registries in `internal/output/table.go`. A resource that is
//! not listed here has no table or key-value view, and falls back to the
//! name-and-key plaintext form.

use serde_json::{Map, Value};

pub type Formatter = fn(Option<&Value>) -> String;

pub struct ColumnDef {
    pub header: &'static str,
    pub field: &'static str,
    pub format: Formatter,
}

/// `fmt.Sprint` of the value, and an empty string for a missing one.
pub fn default_format(value: Option<&Value>) -> String {
    match value {
        None | Some(Value::Null) => String::new(),
        Some(Value::String(s)) => s.clone(),
        Some(Value::Bool(b)) => b.to_string(),
        // A decoded number is a float64 by the time Go prints it, so this is
        // %g rather than the JSON form.
        Some(Value::Number(n)) => match n.as_f64() {
            Some(f) => super::gojson::sprint_float(f),
            None => n.to_string(),
        },
        // Go prints a slice as `[a b c]` and a map as `map[k:v]`. Only the
        // registered fields reach a formatter, and none of them are
        // containers, so this stays a readable fallback rather than a port of
        // Go's reflection-based printer.
        Some(other) => super::gojson::marshal(other),
    }
}

fn bool_yes_no(value: Option<&Value>) -> String {
    match value {
        Some(Value::Bool(true)) => "yes".to_string(),
        Some(Value::Bool(false)) => "no".to_string(),
        other => default_format(other),
    }
}

fn count_list(value: Option<&Value>) -> String {
    match value {
        Some(Value::Array(items)) => items.len().to_string(),
        other => default_format(other),
    }
}

fn truncated_list(value: Option<&Value>, max: usize) -> String {
    let Some(Value::Array(items)) = value else {
        return default_format(value);
    };
    let rendered: Vec<String> = items
        .iter()
        .map(|item| default_format(Some(item)))
        .collect();
    if rendered.len() <= max {
        return rendered.join(", ");
    }
    format!("{}, ...", rendered[..max].join(", "))
}

fn truncated_list_3(value: Option<&Value>) -> String {
    truncated_list(value, 3)
}

fn truncated_list_10(value: Option<&Value>) -> String {
    truncated_list(value, 10)
}

/// Go renders a millisecond timestamp as RFC 3339 in UTC.
fn format_timestamp(value: Option<&Value>) -> String {
    let Some(Value::Number(n)) = value else {
        return default_format(value);
    };
    let Some(millis) = n.as_f64() else {
        return default_format(value);
    };
    rfc3339_utc(millis as i64)
}

/// Days-since-epoch arithmetic, so the crate needs no date dependency.
fn rfc3339_utc(millis: i64) -> String {
    let seconds = millis.div_euclid(1000);
    let days = seconds.div_euclid(86_400);
    let secs_of_day = seconds.rem_euclid(86_400);
    let (hour, minute, second) = (
        secs_of_day / 3600,
        (secs_of_day % 3600) / 60,
        secs_of_day % 60,
    );
    let (year, month, day) = civil_from_days(days);
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second:02}Z")
}

/// Howard Hinnant's civil-from-days algorithm.
fn civil_from_days(days: i64) -> (i64, u32, u32) {
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if m <= 2 { y + 1 } else { y }, m, d)
}

macro_rules! columns {
    ($($header:expr => $field:expr $(, $format:expr)?);* $(;)?) => {
        &[$(ColumnDef {
            header: $header,
            field: $field,
            format: columns!(@format $($format)?),
        }),*]
    };
    (@format) => { default_format };
    (@format $format:expr) => { $format };
}

pub fn list_columns(resource: &str) -> Option<&'static [ColumnDef]> {
    let columns: &'static [ColumnDef] = match resource {
        "flags" => columns! {
            "KEY" => "key";
            "NAME" => "name";
            "KIND" => "kind";
            "TEMPORARY" => "temporary", bool_yes_no;
            "TAGS" => "tags", truncated_list_3;
        },
        "projects" => columns! {
            "KEY" => "key";
            "NAME" => "name";
            "TAG COUNT" => "tags", count_list;
        },
        "environments" => columns! {
            "KEY" => "key";
            "NAME" => "name";
            "COLOR" => "color";
        },
        "members" => columns! {
            "EMAIL" => "email";
            "ROLE" => "role";
            "LAST NAME" => "lastName";
            "FIRST NAME" => "firstName";
        },
        "segments" => columns! {
            "KEY" => "key";
            "NAME" => "name";
            "CREATED" => "creationDate", format_timestamp;
        },
        _ => return None,
    };
    Some(columns)
}

pub fn singular_columns(resource: &str) -> Option<&'static [ColumnDef]> {
    let columns: &'static [ColumnDef] = match resource {
        "flags" => columns! {
            "Key" => "key";
            "Name" => "name";
            "Kind" => "kind";
            "Temporary" => "temporary", bool_yes_no;
            "Created" => "creationDate", format_timestamp;
            "Tags" => "tags", truncated_list_10;
        },
        "projects" => columns! {
            "Key" => "key";
            "Name" => "name";
            "Tag Count" => "tags", count_list;
        },
        "environments" => columns! {
            "Key" => "key";
            "Name" => "name";
            "Color" => "color";
        },
        "members" => columns! {
            "Email" => "email";
            "Role" => "role";
            "Last Name" => "lastName";
            "First Name" => "firstName";
        },
        "segments" => columns! {
            "Key" => "key";
            "Name" => "name";
            "Created" => "creationDate", format_timestamp;
        },
        _ => return None,
    };
    Some(columns)
}

/// A cell's text. Tabs would break the table's alignment, so they become
/// spaces first.
pub fn column_value(resource: Option<&Map<String, Value>>, column: &ColumnDef) -> String {
    let value = resource.and_then(|map| map.get(column.field));
    (column.format)(value).replace('\t', " ")
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn a_millisecond_timestamp_becomes_rfc3339_in_utc() {
        assert_eq!(
            format_timestamp(Some(&json!(1418684722483i64))),
            "2014-12-15T23:05:22Z"
        );
        assert_eq!(format_timestamp(Some(&json!(0))), "1970-01-01T00:00:00Z");
        assert_eq!(
            format_timestamp(Some(&json!(1618684722483i64))),
            "2021-04-17T18:38:42Z"
        );
        // A leap day, and a value that is not a number at all.
        assert_eq!(
            format_timestamp(Some(&json!(1583020800000i64))),
            "2020-03-01T00:00:00Z"
        );
        assert_eq!(format_timestamp(Some(&json!("nope"))), "nope");
        assert_eq!(format_timestamp(None), "");
    }

    #[test]
    fn booleans_read_as_yes_and_no_but_other_types_pass_through() {
        assert_eq!(bool_yes_no(Some(&json!(true))), "yes");
        assert_eq!(bool_yes_no(Some(&json!(false))), "no");
        assert_eq!(bool_yes_no(Some(&json!("maybe"))), "maybe");
        assert_eq!(bool_yes_no(None), "");
    }

    #[test]
    fn a_long_list_is_truncated_with_an_ellipsis() {
        let tags = json!(["a", "b", "c", "d"]);
        assert_eq!(truncated_list(Some(&tags), 3), "a, b, c, ...");
        assert_eq!(truncated_list(Some(&tags), 4), "a, b, c, d");
        assert_eq!(truncated_list(Some(&json!([])), 3), "");
        assert_eq!(count_list(Some(&tags)), "4");
    }

    #[test]
    fn only_the_registered_resources_have_columns() {
        assert!(list_columns("flags").is_some());
        assert!(list_columns("webhooks").is_none());
        assert!(singular_columns("segments").is_some());
        assert_eq!(list_columns("members").unwrap()[0].header, "EMAIL");
    }
}
