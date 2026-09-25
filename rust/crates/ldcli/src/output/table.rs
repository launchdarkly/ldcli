//! Aligned plaintext tables and key-value blocks.
//!
//! Go builds the table with `text/tabwriter` at padding 3 and a space pad
//! character. Two details of that writer are visible in the output: a cell's
//! width is counted in runes, and the last cell on a line is not padded,
//! because padding is applied to tab-terminated cells only.

use super::columns::{column_value, ColumnDef};
use serde_json::{Map, Value};

const PADDING: usize = 3;

/// Align rows into columns. Every row is expected to have one cell per column,
/// which is how `TableOutput` calls into tabwriter.
pub fn align(rows: &[Vec<String>]) -> String {
    let column_count = rows.iter().map(Vec::len).max().unwrap_or(0);
    let mut widths = vec![0usize; column_count];
    for row in rows {
        // The final cell of a row has no tab after it, so it never sets a width.
        for (index, cell) in row.iter().enumerate().take(row.len().saturating_sub(1)) {
            widths[index] = widths[index].max(cell.chars().count() + PADDING);
        }
    }

    rows.iter()
        .map(|row| {
            let mut line = String::new();
            for (index, cell) in row.iter().enumerate() {
                line.push_str(cell);
                if index + 1 < row.len() {
                    let pad = widths[index].saturating_sub(cell.chars().count());
                    line.push_str(&" ".repeat(pad));
                }
            }
            line
        })
        .collect::<Vec<_>>()
        .join("\n")
}

pub fn table_output(items: &[Map<String, Value>], columns: &[ColumnDef]) -> String {
    let mut rows = Vec::with_capacity(items.len() + 1);
    rows.push(columns.iter().map(|c| c.header.to_string()).collect());
    for item in items {
        rows.push(
            columns
                .iter()
                .map(|column| column_value(Some(item), column))
                .collect(),
        );
    }
    align(&rows).trim_end_matches('\n').to_string()
}

/// `Header:` padded to the longest header, then two spaces, then the value.
pub fn key_value_output(resource: &Map<String, Value>, columns: &[ColumnDef]) -> String {
    let max_len = columns
        .iter()
        .map(|column| column.header.len())
        .max()
        .unwrap_or(0);
    columns
        .iter()
        .map(|column| {
            let padding = " ".repeat(max_len - column.header.len());
            format!(
                "{}:{padding}  {}",
                column.header,
                column_value(Some(resource), column)
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rows(raw: &[&[&str]]) -> Vec<Vec<String>> {
        raw.iter()
            .map(|row| row.iter().map(|cell| (*cell).to_string()).collect())
            .collect()
    }

    #[test]
    fn columns_are_padded_to_the_widest_cell_plus_three() {
        let out = align(&rows(&[
            &["KEY", "NAME"],
            &["alt-page", "Alternate page"],
            &["a", "b"],
        ]));
        assert_eq!(
            out,
            "KEY        NAME\nalt-page   Alternate page\na          b"
        );
    }

    #[test]
    fn the_last_cell_is_never_padded_but_an_empty_one_leaves_the_gap() {
        let out = align(&rows(&[&["A", "B", "C"], &["longer", "x", ""]]));
        // The middle column is padded on both rows; the final column is not,
        // so the second line keeps its trailing spaces and nothing more.
        assert_eq!(out, "A        B   C\nlonger   x   ");
    }

    #[test]
    fn width_is_counted_in_characters_not_bytes() {
        let out = align(&rows(&[&["é", "x"], &["ab", "y"]]));
        assert_eq!(out, "é    x\nab   y");
    }

    #[test]
    fn key_value_lines_align_on_the_longest_header() {
        let columns = super::super::columns::singular_columns("projects").unwrap();
        let mut resource = Map::new();
        resource.insert("key".into(), Value::String("default".into()));
        resource.insert("name".into(), Value::String("Default".into()));
        resource.insert("tags".into(), Value::Array(vec![Value::String("a".into())]));
        assert_eq!(
            key_value_output(&resource, columns),
            "Key:        default\nName:       Default\nTag Count:  1"
        );
    }
}
