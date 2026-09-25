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
use install::InstallResult;
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

    fn boolean_omitempty(self, name: &str, value: bool) -> Self {
        if !value {
            return self;
        }
        self.boolean(name, value)
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

pub struct InstallContext<'a> {
    pub path: &'a str,
    pub sdk_id: &'a str,
    pub package_manager: &'a str,
    pub dry_run: bool,
    pub json: bool,
    pub machine: &'a Machine<'a>,
}

pub fn run_install(ctx: &InstallContext<'_>, stderr: &dyn Fn(&str)) -> Outcome {
    let dir = match ctx.machine.project_dir(ctx.path) {
        Ok(dir) => dir,
        Err(err) => return Outcome::Failure(format!("{err}\n")),
    };
    let mut package_manager = ctx.package_manager.to_string();
    if package_manager.is_empty() {
        let choice = detect::package_manager_choice_for(ctx.machine, &dir, ctx.sdk_id);
        if choice.confidence == AMBIGUOUS {
            let names: Vec<String> = choice
                .candidates
                .iter()
                .map(|c| {
                    if c.installed {
                        c.name.clone()
                    } else {
                        format!("{} (not installed)", c.name)
                    }
                })
                .collect();
            return Outcome::Failure(format!(
                "cannot tell which package manager to use: {}\npass --package-manager with one of: {}\n",
                choice.reason,
                names.join(", ")
            ));
        }
        package_manager = choice.name;
        stderr(&format!(
            "note: --package-manager was not given, so setup read the project and chose {}. \
             This previously defaulted to npm or pip. Pass --package-manager to pin it.\n",
            crate::gostr::quote(&package_manager)
        ));
    }
    let result = if ctx.dry_run {
        let (args, package) =
            install::install_args(ctx.machine, &dir, ctx.sdk_id, &package_manager);
        InstallResult {
            sdk_id: ctx.sdk_id.to_string(),
            package,
            command: args.join(" "),
            dry_run: true,
            ..InstallResult::default()
        }
    } else {
        match install::install(ctx.machine, &dir, ctx.sdk_id, &package_manager) {
            Ok(result) => result,
            Err(err) => return Outcome::Failure(format!("{err}\n")),
        }
    };
    if ctx.json {
        let payload = JsonObject::default()
            .string("sdk_id", &result.sdk_id)
            .string("package", &result.package)
            .string("version", "")
            .string("command", &result.command)
            .boolean_omitempty("dry_run", result.dry_run)
            .boolean_omitempty("already_installed", result.already_installed)
            .boolean_omitempty("failed", result.failed)
            .string_omitempty("failure_reason", &result.failure_reason)
            .string_omitempty("warning", &result.warning)
            .boolean("success", result.success);
        return Outcome::Stdout(format!("{}\n", payload.finish()));
    }
    let mut out = format!("SDK: {}\nPackage: {}\n", result.sdk_id, result.package);
    if result.already_installed {
        out.push_str("Already installed — skipping install.\n");
        return Outcome::Stdout(out);
    }
    if !result.command.is_empty() {
        out.push_str(&format!("Command: {}\n", result.command));
    }
    if result.dry_run {
        out.push_str("Dry run: command not executed\n");
        return Outcome::Stdout(out);
    }
    out.push_str(&format!("Success: {}\n", result.success));
    if !result.warning.is_empty() {
        out.push_str(&format!("Warning: {}\n", result.warning));
    }
    if !result.failure_reason.is_empty() {
        out.push_str(&format!("Reason: {}\n", result.failure_reason));
    } else if !result.success && install::requires_manual_install(&result.sdk_id) {
        out.push_str(&format!(
            "Reason: {} has no automated install command; add {} to your build configuration by hand.\n",
            result.sdk_id, result.package
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

    fn install_in(files: &[&str], tools: &[&str], package_manager: &str, dry_run: bool) -> Outcome {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let bin = tempfile::tempdir().unwrap();
        for file in files {
            let path = dir.path().join(file);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, "").unwrap();
        }
        for tool in tools {
            let path = bin.path().join(tool);
            fs::write(&path, "#!/bin/sh\n").unwrap();
            fs::set_permissions(&path, fs::Permissions::from_mode(0o755)).unwrap();
        }
        let machine = Machine {
            path: Some(bin.path().as_os_str()),
            virtual_env: None,
            pwd: None,
        };
        run_install(
            &InstallContext {
                path: dir.path().to_str().unwrap(),
                sdk_id: if files.iter().any(|f| f.ends_with(".csproj")) {
                    "dotnet-server-sdk"
                } else {
                    "node-server"
                },
                package_manager,
                dry_run,
                json: true,
                machine: &machine,
            },
            &|_| {},
        )
    }

    #[test]
    fn install_json_keeps_go_field_order_and_omits_false_flags() {
        assert_eq!(
            install_in(&[], &[], "yarn", true),
            Outcome::Stdout(concat!(
                r#"{"sdk_id":"node-server","package":"@launchdarkly/node-server-sdk","version":"","#,
                r#""command":"yarn add @launchdarkly/node-server-sdk","dry_run":true,"success":false}"#,
                "\n"
            ).into())
        );
        assert_eq!(
            install_in(&["src/A/A.csproj", "src/B/B.csproj"], &["dotnet"], "dotnet", false),
            Outcome::Stdout(concat!(
                r#"{"sdk_id":"dotnet-server-sdk","package":"LaunchDarkly.ServerSdk","version":"","#,
                r#""command":"","failed":true,"#,
                r#""failure_reason":"found 2 projects in this solution; run `dotnet add package LaunchDarkly.ServerSdk --project \u003cpath\u003e` for the one that needs the SDK","#,
                r#""success":false}"#,
                "\n"
            ).into())
        );
    }

    #[test]
    fn install_runs_the_tool_and_reports_success() {
        assert_eq!(
            install_in(&["App.csproj"], &["dotnet"], "dotnet", false),
            Outcome::Stdout(concat!(
                r#"{"sdk_id":"dotnet-server-sdk","package":"LaunchDarkly.ServerSdk","version":"","#,
                r#""command":"dotnet add package LaunchDarkly.ServerSdk","success":true}"#,
                "\n"
            ).into())
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
