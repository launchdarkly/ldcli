//! `ldcli setup`'s non-interactive subcommands: detect, install, and init.
//!
//! The guided wizard that `ldcli setup` itself runs is not ported.

pub mod detect;
pub mod install;

use crate::cli::Outcome;
use crate::flags::{Flag, FlagKind};
use crate::gopath;
use crate::help::help_flag;
use crate::output::gojson;
use detect::{DetectResult, AMBIGUOUS};
use std::ffi::OsStr;

/// What setup reads from the machine rather than the project.
pub struct Machine<'a> {
    pub path: Option<&'a OsStr>,
    pub virtual_env: Option<&'a str>,
    /// `$PWD`, which `os.Getwd` prefers when it names the current directory.
    pub pwd: Option<&'a str>,
}

impl Machine<'_> {
    /// `exec.LookPath` succeeded, which it does not for a match found
    /// through a relative PATH entry.
    pub fn on_path(&self, name: &str) -> bool {
        crate::browser::look_path(name, self.path).is_ok()
    }

    /// `--path`, or the current directory when it is empty.
    fn project_dir(&self, path: &str) -> Result<String, String> {
        if path.is_empty() {
            gopath::getwd(self.pwd)
        } else {
            Ok(path.to_string())
        }
    }
}

/// A Go struct marshalled in declaration order, the way `json.Marshal`
/// writes one.
#[derive(Default)]
struct JsonObject(Vec<String>);

impl JsonObject {
    fn raw(mut self, name: &str, value: String) -> Self {
        self.0.push(format!("{}:{value}", gojson::quote(name)));
        self
    }

    fn string(self, name: &str, value: &str) -> Self {
        self.raw(name, gojson::quote(value))
    }

    fn string_omitempty(self, name: &str, value: &str) -> Self {
        if value.is_empty() {
            return self;
        }
        self.string(name, value)
    }

    fn boolean(self, name: &str, value: bool) -> Self {
        self.raw(name, value.to_string())
    }

    fn finish(self) -> String {
        format!("{{{}}}", self.0.join(","))
    }
}

fn detect_json(result: &DetectResult) -> JsonObject {
    JsonObject::default()
        .string("language", &result.language)
        .string_omitempty("framework", &result.framework)
        .string("package_manager", &result.package_manager)
        .string("sdk_id", &result.sdk_id)
        .string("entry_point", &result.entry_point)
        .boolean("entry_point_exists", result.entry_point_exists)
        .string_omitempty(
            "package_manager_confidence",
            &result.package_manager_confidence,
        )
        .string_omitempty("package_manager_reason", &result.package_manager_reason)
}

pub struct DetectContext<'a> {
    pub path: &'a str,
    pub json: bool,
    pub machine: &'a Machine<'a>,
}

pub fn run_detect(ctx: &DetectContext<'_>) -> Outcome {
    let dir = match ctx.machine.project_dir(ctx.path) {
        Ok(dir) => dir,
        Err(err) => return Outcome::Failure(format!("{err}\n")),
    };
    let result = match detect::detect(ctx.machine, &dir) {
        Ok(result) => result,
        Err(err) => return Outcome::Failure(format!("{err}\n")),
    };
    let ambiguous = result.package_manager_confidence == AMBIGUOUS;
    if ctx.json {
        // Candidates describe the machine, not the project, so only this
        // output adds them.
        let mut payload = detect_json(&result);
        if ambiguous {
            let candidates =
                detect::package_manager_choice_for(ctx.machine, &dir, &result.sdk_id).candidates;
            if !candidates.is_empty() {
                let items: Vec<String> = candidates
                    .iter()
                    .map(|c| {
                        JsonObject::default()
                            .string("name", &c.name)
                            .boolean("installed", c.installed)
                            .string("command", &c.command)
                            .finish()
                    })
                    .collect();
                payload = payload.raw(
                    "package_manager_candidates",
                    format!("[{}]", items.join(",")),
                );
            }
        }
        return Outcome::Stdout(format!("{}\n", payload.finish()));
    }
    let mut out = format!("Language: {}\n", result.language);
    if !result.framework.is_empty() {
        out.push_str(&format!("Framework: {}\n", result.framework));
    }
    if ambiguous {
        out.push_str(&format!(
            "Package Manager: {} (uncertain — {})\n",
            result.package_manager, result.package_manager_reason
        ));
    } else {
        out.push_str(&format!("Package Manager: {}\n", result.package_manager));
    }
    out.push_str(&format!("Recommended SDK: {}\n", result.sdk_id));
    if result.entry_point_exists {
        out.push_str(&format!("Entry Point: {}\n", result.entry_point));
    } else {
        out.push_str(&format!(
            "Entry Point: {} (suggested, does not exist)\n",
            result.entry_point
        ));
    }
    Outcome::Stdout(out)
}

fn string_flag(name: &'static str, usage: &'static str) -> Flag {
    Flag {
        name,
        shorthand: None,
        usage,
        kind: FlagKind::Str { default: "" },
    }
}

const PATH_USAGE: &str = "Path to the project directory (defaults to current directory)";

pub fn detect_flags() -> Vec<Flag> {
    vec![
        help_flag("help for detect"),
        string_flag("path", PATH_USAGE),
    ]
}

pub fn install_flags() -> Vec<Flag> {
    vec![
        help_flag("help for install"),
        string_flag("path", PATH_USAGE),
        string_flag(
            "sdk-id",
            "SDK identifier to install (e.g. node-server, react-client-sdk)",
        ),
        string_flag(
            "package-manager",
            "Package manager to use (e.g. npm, pip, go)",
        ),
        Flag {
            name: "dry-run",
            shorthand: None,
            usage: "Print the install command that would run without executing it",
            kind: FlagKind::Bool,
        },
    ]
}

pub fn init_flags() -> Vec<Flag> {
    vec![
        help_flag("help for init"),
        string_flag(
            "sdk-id",
            "SDK identifier (e.g. node-server, react-client-sdk)",
        ),
        string_flag("file", "Target file to inject initialization code into"),
        string_flag("sdk-key", "Server-side SDK key"),
        string_flag("client-side-id", "Client-side environment ID"),
        string_flag("mobile-key", "Mobile SDK key"),
        string_flag(
            "flag-key",
            "Feature flag key to use in the initialization example",
        ),
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn detect_in(files: &[&str], json: bool) -> String {
        let dir = tempfile::tempdir().unwrap();
        for file in files {
            fs::write(dir.path().join(file), "").unwrap();
        }
        let machine = Machine {
            path: Some(OsStr::new("")),
            virtual_env: None,
            pwd: None,
        };
        let path = dir.path().to_str().unwrap();
        let out = match run_detect(&DetectContext {
            path,
            json,
            machine: &machine,
        }) {
            Outcome::Stdout(out) => out,
            _ => panic!("detect failed"),
        };
        out.replace(path, "DIR")
    }

    #[test]
    fn detect_json_keeps_go_field_order_and_appends_candidates_when_ambiguous() {
        assert_eq!(
            detect_in(&["requirements.txt", "poetry.lock", "uv.lock"], true),
            concat!(
                r#"{"language":"Python","package_manager":"uv","sdk_id":"python-server-sdk","#,
                r#""entry_point":"DIR/main.py","entry_point_exists":false,"#,
                r#""package_manager_confidence":"ambiguous","#,
                r#""package_manager_reason":"this project is set up for more than one manager (uv, poetry)","#,
                r#""package_manager_candidates":["#,
                r#"{"name":"pip","installed":false,"command":"pip install launchdarkly-server-sdk"},"#,
                r#"{"name":"uv","installed":false,"command":"uv add launchdarkly-server-sdk"},"#,
                r#"{"name":"poetry","installed":false,"command":"poetry add launchdarkly-server-sdk"},"#,
                r#"{"name":"pipenv","installed":false,"command":"pipenv install launchdarkly-server-sdk"},"#,
                r#"{"name":"pdm","installed":false,"command":"pdm add launchdarkly-server-sdk"}]}"#,
                "\n"
            )
        );
    }

    #[test]
    fn a_definite_result_has_no_reason_or_candidates() {
        assert_eq!(
            detect_in(&["go.mod"], true),
            concat!(
                r#"{"language":"Go","package_manager":"go","sdk_id":"go-server-sdk","#,
                r#""entry_point":"DIR/main.go","entry_point_exists":false,"#,
                r#""package_manager_confidence":"definite"}"#,
                "\n"
            )
        );
    }
}
