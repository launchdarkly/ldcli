//! Compare every formatter rendering against what the Go formatters produced.
//!
//! The fixtures under `parity/fixtures/output` are written by
//! `make output-fixtures`, which runs the payloads through `internal/output`
//! itself. A failure here means the port diverged from the Go tree, not that
//! an expectation was written by hand.

use ldcli::output::{
    cmd_output, cmd_output_error, columns, gojson, CmdError, CmdOutputOpts, OutputKind,
};
use serde::Deserialize;
use std::path::PathBuf;

#[derive(Debug, Deserialize)]
struct RenderCase {
    name: String,
    action: String,
    kind: String,
    #[serde(default)]
    resource: String,
    #[serde(default)]
    fields: Vec<String>,
    input: String,
    stdout: String,
    #[serde(default)]
    stderr: String,
    #[serde(default)]
    error: String,
}

#[derive(Debug, Deserialize)]
struct ErrorCase {
    name: String,
    kind: String,
    source: String,
    #[serde(default)]
    body: String,
    output: String,
}

#[derive(Debug, Deserialize)]
struct KindCase {
    input: String,
    kind: String,
    #[serde(default)]
    error: String,
}

#[derive(Debug, Deserialize)]
struct ValueCase {
    input: String,
    output: String,
}

fn fixture<T: for<'de> Deserialize<'de>>(name: &str) -> Vec<T> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../../parity/fixtures/output")
        .join(name);
    let text = std::fs::read_to_string(&path).unwrap_or_else(|err| {
        panic!(
            "read {}: {err}. Run `make output-fixtures`.",
            path.display()
        )
    });
    let cases: Vec<T> = serde_json::from_str(&text).unwrap_or_else(|err| panic!("{name}: {err}"));
    assert!(!cases.is_empty(), "{name} has no cases");
    cases
}

fn assert_no_failures(failures: Vec<String>, total: usize, what: &str) {
    assert!(
        failures.is_empty(),
        "{} of {total} {what} differ:\n{}",
        failures.len(),
        failures.join("\n")
    );
}

#[test]
fn every_rendering_matches_the_go_formatter() {
    let cases: Vec<RenderCase> = fixture("cases.json");
    let mut failures = Vec::new();
    for case in &cases {
        let opts = CmdOutputOpts {
            fields: case.fields.clone(),
            resource_name: case.resource.clone(),
        };
        match cmd_output(&case.action, &case.kind, &case.input, &opts) {
            Ok(_) if !case.error.is_empty() => failures.push(format!(
                "{}: Go failed with {:?}, Rust succeeded",
                case.name, case.error
            )),
            Ok(rendered) => {
                if rendered.stdout != case.stdout {
                    failures.push(format!(
                        "{} stdout\n  go:   {:?}\n  rust: {:?}",
                        case.name, case.stdout, rendered.stdout
                    ));
                }
                if rendered.stderr != case.stderr {
                    failures.push(format!(
                        "{} stderr\n  go:   {:?}\n  rust: {:?}",
                        case.name, case.stderr, rendered.stderr
                    ));
                }
            }
            Err(message) if case.error.is_empty() => {
                failures.push(format!("{}: Rust failed with {message:?}", case.name))
            }
            Err(_) => {}
        }
    }
    assert_no_failures(failures, cases.len(), "renderings");
}

#[test]
fn every_error_rendering_matches_the_go_formatter() {
    let cases: Vec<ErrorCase> = fixture("errors.json");
    let mut failures = Vec::new();
    for case in &cases {
        let error = match case.source.as_str() {
            "unauthorized-401" => CmdError::unauthorized(),
            "plain-error" => CmdError::Other("dial tcp: connection refused".to_string()),
            "cli-error" => CmdError::Cli(ldcli::output::INVALID_OUTPUT_KIND.to_string()),
            "json-unmarshal-type-error" => CmdError::InvalidJson,
            _ => CmdError::ApiBody(case.body.clone()),
        };
        let rendered = cmd_output_error(&case.kind, &error);
        if rendered != case.output {
            failures.push(format!(
                "{}\n  go:   {:?}\n  rust: {:?}",
                case.name, case.output, rendered
            ));
        }
    }
    assert_no_failures(failures, cases.len(), "error renderings");
}

#[test]
fn the_output_kind_accepts_and_rejects_the_same_values_as_go() {
    let cases: Vec<KindCase> = fixture("kinds.json");
    for case in &cases {
        match OutputKind::parse(&case.input) {
            Ok(kind) => {
                assert!(
                    case.error.is_empty(),
                    "{:?}: Go rejected it with {:?}",
                    case.input,
                    case.error
                );
                assert_eq!(kind.as_str(), case.kind, "for input {:?}", case.input);
            }
            Err(message) => {
                assert_eq!(message, case.error, "for input {:?}", case.input);
                assert!(case.kind.is_empty());
            }
        }
    }
}

#[test]
fn re_marshaling_a_decoded_value_matches_encoding_json() {
    let cases: Vec<ValueCase> = fixture("numbers.json");
    let failures: Vec<String> = cases
        .iter()
        .filter_map(|case| {
            let rendered = gojson::marshal(&gojson::decode(&case.input).expect("decode"));
            (rendered != case.output).then(|| {
                format!(
                    "{}\n  go:   {}\n  rust: {}",
                    case.input, case.output, rendered
                )
            })
        })
        .collect();
    assert_no_failures(failures, cases.len(), "re-marshaled values");
}

#[test]
fn printing_a_decoded_number_matches_fmt_sprint() {
    let cases: Vec<ValueCase> = fixture("sprint.json");
    let failures: Vec<String> = cases
        .iter()
        .filter_map(|case| {
            let value = gojson::decode(&case.input).expect("decode");
            let rendered = columns::default_format(Some(&value));
            (rendered != case.output).then(|| {
                format!(
                    "{}\n  go:   {}\n  rust: {}",
                    case.input, case.output, rendered
                )
            })
        })
        .collect();
    assert_no_failures(failures, cases.len(), "printed numbers");
}
