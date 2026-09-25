//! `ldcli config`: list, set, and unset settings in the config file.
//!
//! A port of `cmd/config/config.go`. Its output kind is Viper's `output`
//! setting and nothing else, so `--json` has no effect here even though other
//! commands honor it.

use crate::cli::Outcome;
use crate::config::{self, Config};
use crate::output::{self, CmdError, CmdOutputOpts};
use serde_json::{Map, Value};
use std::path::Path;

pub struct ConfigArgs {
    pub list: bool,
    pub set: bool,
    pub unset: Option<String>,
    pub args: Vec<String>,
}

pub struct ConfigContext<'a> {
    pub path: &'a Path,
    /// The resolved `output` setting, unvalidated.
    pub output: &'a str,
    pub base_uri: &'a str,
    pub version: &'a str,
    /// Printed when no action flag is given.
    pub help: String,
}

pub fn run(args: &ConfigArgs, ctx: &ConfigContext<'_>) -> Outcome {
    let result = if args.list {
        list(ctx)
    } else if args.set {
        set(&args.args, ctx)
    } else if let Some(key) = &args.unset {
        unset(key, ctx)
    } else {
        return Outcome::Stdout(ctx.help.clone());
    };
    match result {
        Ok(text) => Outcome::Stdout(format!("{text}\n")),
        Err(message) => Outcome::Failure(format!("{}\n", error_output(ctx.output, &message))),
    }
}

/// `newErr`: the message is wrapped as JSON and rendered in the output kind.
fn error_output(kind: &str, message: &str) -> String {
    let body = format!(
        "{{\"message\": {}}}",
        output::gojson::marshal(&Value::String(message.to_string()))
    );
    output::cmd_output_error(kind, &CmdError::Cli(body))
}

fn list(ctx: &ConfigContext<'_>) -> Result<String, String> {
    let conf = config::load_strict(ctx.path)?;
    let fields = conf.redacted().to_json_map();
    // CmdOutputSingular, unlike CmdOutput, rejects an unknown kind.
    match output::OutputKind::parse(ctx.output)? {
        output::OutputKind::Json => Ok(output::gojson::marshal(&Value::Object(fields))),
        output::OutputKind::Plaintext | output::OutputKind::Markdown => {
            Ok(config_plaintext(&fields))
        }
    }
}

/// `ConfigPlaintextOutputFn`: `key: value` lines in key order.
fn config_plaintext(fields: &Map<String, Value>) -> String {
    let mut keys: Vec<&String> = fields.keys().collect();
    keys.sort();
    keys.iter()
        .map(|key| {
            format!(
                "{key}: {}",
                output::columns::default_format(fields.get(key.as_str()))
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn set(kvs: &[String], ctx: &ConfigContext<'_>) -> Result<String, String> {
    let conf = config::load_strict(ctx.path)?;
    let (updated, keys) = conf.update(kvs)?;
    if keys.iter().any(|key| key == "access-token") {
        let token = updated.access_token.clone().unwrap_or_default();
        if !crate::http::verify_access_token(&token, ctx.base_uri, ctx.version) {
            return Err(format!(
                "access-token is invalid. Go to {} to create an access token.",
                crate::http::join_path(ctx.base_uri, "settings/authorization")
            ));
        }
    }
    write(ctx.path, &updated, None)?;

    let items = Value::Array(keys.into_iter().map(Value::String).collect());
    let body = output::gojson::marshal(&Value::Object(Map::from_iter([(
        "items".to_string(),
        items,
    )])));
    render("update", &body, ctx)
}

fn unset(key: &str, ctx: &ConfigContext<'_>) -> Result<String, String> {
    let conf = config::load_strict(ctx.path)?;
    Config::validate_removal(key)?;
    write(ctx.path, &conf, Some(key))?;

    let body = output::gojson::marshal(&Value::Object(Map::from_iter([(
        "key".to_string(),
        Value::String(key.to_string()),
    )])));
    render("delete", &body, ctx)
}

fn write(path: &Path, conf: &Config, skip: Option<&str>) -> Result<(), String> {
    config::write_atomic(path, &conf.to_yaml(skip)).map_err(|err| err.to_string())
}

fn render(action: &str, body: &str, ctx: &ConfigContext<'_>) -> Result<String, String> {
    output::cmd_output(action, ctx.output, body, &CmdOutputOpts::default())
        .map(|rendered| rendered.stdout)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn context<'a>(path: &'a Path, output: &'a str) -> ConfigContext<'a> {
        ConfigContext {
            path,
            output,
            base_uri: "http://127.0.0.1:1",
            version: "test",
            help: "HELP".to_string(),
        }
    }

    fn args(list: bool, set: bool, unset: Option<&str>, rest: &[&str]) -> ConfigArgs {
        ConfigArgs {
            list,
            set,
            unset: unset.map(str::to_string),
            args: rest.iter().map(|s| s.to_string()).collect(),
        }
    }

    fn file_with(contents: &str) -> (tempfile::TempDir, std::path::PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.yml");
        std::fs::write(&path, contents).unwrap();
        (dir, path)
    }

    #[test]
    fn list_redacts_the_token_in_every_kind() {
        let (_dir, path) = file_with("output: markdown\naccess-token: api-secret\n");
        let plaintext = run(&args(true, false, None, &[]), &context(&path, "plaintext"));
        assert_eq!(
            plaintext,
            Outcome::Stdout("access-token: [REDACTED]\noutput: markdown\n".into())
        );
        let json = run(&args(true, false, None, &[]), &context(&path, "json"));
        assert_eq!(
            json,
            Outcome::Stdout("{\"access-token\":\"[REDACTED]\",\"output\":\"markdown\"}\n".into())
        );
    }

    #[test]
    fn list_rejects_an_unknown_kind_in_plaintext_because_the_kind_is_not_json() {
        let (_dir, path) = file_with("{}\n");
        assert_eq!(
            run(&args(true, false, None, &[]), &context(&path, "yaml")),
            Outcome::Failure(format!("{}\n", output::INVALID_OUTPUT_KIND))
        );
    }

    #[test]
    fn set_rewrites_the_document_and_reports_the_keys() {
        let (_dir, path) = file_with("project: old\nport: 9000\n");
        let outcome = run(
            &args(false, true, None, &["project", "p2", "environment", "prod"]),
            &context(&path, "plaintext"),
        );
        assert_eq!(
            outcome,
            Outcome::Stdout("Successfully updated\n* project\n* environment\n".into())
        );
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "environment: prod\nproject: p2\n"
        );
    }

    #[test]
    fn a_rejected_value_is_a_json_error_and_leaves_the_file_alone() {
        let (_dir, path) = file_with("project: old\n");
        let outcome = run(
            &args(false, true, None, &["analytics-opt-out", "maybe"]),
            &context(&path, "json"),
        );
        assert_eq!(
            outcome,
            Outcome::Failure("{\"message\":\"analytics-opt-out must be true or false\"}\n".into())
        );
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "project: old\n");
    }

    #[test]
    fn a_token_that_does_not_verify_is_not_stored() {
        let (_dir, path) = file_with("{}\n");
        let outcome = run(
            &args(false, true, None, &["access-token", "api-parity"]),
            &context(&path, "json"),
        );
        match outcome {
            Outcome::Failure(message) => assert!(
                message.contains(
                    "Go to http://127.0.0.1:1/settings/authorization to create an access token."
                ),
                "{message}"
            ),
            other => panic!("{other:?}"),
        }
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{}\n");
    }

    #[test]
    fn unset_removes_the_key_and_reports_it_in_the_output_kind() {
        let (_dir, path) = file_with("output: markdown\nproject: proj\n");
        let outcome = run(
            &args(false, false, Some("project"), &[]),
            &context(&path, "markdown"),
        );
        assert_eq!(
            outcome,
            Outcome::Stdout("Successfully deleted\n\n## project\n".into())
        );
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "output: markdown\n"
        );
    }

    #[test]
    fn invalid_yaml_fails_every_action_without_touching_the_file() {
        let (_dir, path) = file_with("output: [unclosed\n");
        for action in [
            args(true, false, None, &[]),
            args(false, true, None, &["output", "json"]),
            args(false, false, Some("output"), &[]),
        ] {
            assert_eq!(
                run(&action, &context(&path, "json")),
                Outcome::Failure("{\"message\":\"config file is invalid yaml\"}\n".into())
            );
        }
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            "output: [unclosed\n"
        );
    }

    #[test]
    fn no_action_prints_help() {
        let (_dir, path) = file_with("{}\n");
        assert_eq!(
            run(
                &args(false, false, None, &["stray"]),
                &context(&path, "json")
            ),
            Outcome::Stdout("HELP".into())
        );
    }
}
