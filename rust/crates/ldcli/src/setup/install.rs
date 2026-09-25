//! `internal/setup/installer.go`: the command that installs each SDK.

use super::Machine;
use crate::gopath;

/// `InstallArgs`: the argv that installs `sdk_id` and the package it names.
/// SDKs installed by hand get no argv, only the package to add.
pub fn install_args(
    machine: &Machine<'_>,
    dir: &str,
    sdk_id: &str,
    package_manager: &str,
) -> (Vec<String>, String) {
    let node = |pkg: &str| {
        (
            node_install_cmd(resolve_node_pm(package_manager), pkg),
            pkg.into(),
        )
    };
    let argv = |args: &[&str]| args.iter().map(|a| (*a).to_string()).collect::<Vec<_>>();
    match sdk_id {
        "react-client-sdk" => node("launchdarkly-react-client-sdk"),
        "react-native" => node("@launchdarkly/react-native-client-sdk"),
        "node-server" => node("@launchdarkly/node-server-sdk"),
        "js-client-sdk" => node("launchdarkly-js-client-sdk"),
        "python-server-sdk" => {
            let pkg = "launchdarkly-server-sdk";
            (
                python_install_cmd(machine, dir, package_manager, pkg),
                pkg.into(),
            )
        }
        "go-server-sdk" => {
            let pkg = "github.com/launchdarkly/go-server-sdk/v7";
            (argv(&["go", "get", pkg]), pkg.into())
        }
        "ruby-server-sdk" => {
            let pkg = "launchdarkly-server-sdk";
            if package_manager == "bundle" {
                (argv(&["bundle", "add", pkg]), pkg.into())
            } else {
                (argv(&["gem", "install", pkg]), pkg.into())
            }
        }
        "dotnet-server-sdk" => {
            let pkg = "LaunchDarkly.ServerSdk";
            (argv(&["dotnet", "add", "package", pkg]), pkg.into())
        }
        "java-server-sdk" => (
            Vec::new(),
            "com.launchdarkly:launchdarkly-java-server-sdk".into(),
        ),
        "android" | "android-client-sdk" => (
            Vec::new(),
            "com.launchdarkly:launchdarkly-android-client-sdk".into(),
        ),
        "swift-client-sdk" | "ios-client-sdk" => (Vec::new(), "LaunchDarkly".into()),
        other => (Vec::new(), other.into()),
    }
}

fn node_install_cmd(pm: &str, pkg: &str) -> Vec<String> {
    let (tool, verb) = match pm {
        "yarn" => ("yarn", "add"),
        "pnpm" => ("pnpm", "add"),
        "bun" => ("bun", "add"),
        _ => ("npm", "install"),
    };
    vec![tool.into(), verb.into(), pkg.into()]
}

fn resolve_node_pm(pm: &str) -> &str {
    match pm {
        "yarn" | "pnpm" | "bun" => pm,
        _ => "npm",
    }
}

fn python_install_cmd(machine: &Machine<'_>, dir: &str, pm: &str, pkg: &str) -> Vec<String> {
    let (tool, verb) = match pm {
        "poetry" => ("poetry", "add"),
        "uv" => ("uv", "add"),
        "pipenv" => ("pipenv", "install"),
        "pdm" => ("pdm", "add"),
        _ => return pip_install_cmd(machine, dir, pkg),
    };
    vec![tool.into(), verb.into(), pkg.into()]
}

/// A virtualenv's pip wins, then pip3 or pip from PATH. With neither, the
/// bare `pip` is still shown, so there is a command to read.
fn pip_install_cmd(machine: &Machine<'_>, dir: &str, pkg: &str) -> Vec<String> {
    let (venv_pip, _) = venv_pip_target(machine, dir);
    if !venv_pip.is_empty() {
        return vec![venv_pip, "install".into(), pkg.into()];
    }
    for bin in ["pip3", "pip"] {
        if machine.on_path(bin) {
            return vec![bin.into(), "install".into(), pkg.into()];
        }
    }
    vec!["pip".into(), "install".into(), pkg.into()]
}

fn abs_or_as_given(machine: &Machine<'_>, path: &str) -> String {
    gopath::abs(path, machine.pwd).unwrap_or_else(|_| path.to_string())
}

/// The active virtualenv first, then the project's `.venv` and `venv`, all
/// absolute. An empty `dir` names no project, so only the active one counts.
fn venv_roots(machine: &Machine<'_>, dir: &str) -> Vec<String> {
    let mut roots = Vec::new();
    if let Some(active) = machine.virtual_env.filter(|v| !v.is_empty()) {
        roots.push(active.to_string());
    }
    if !dir.is_empty() {
        roots.push(gopath::join(&[dir, ".venv"]));
        roots.push(gopath::join(&[dir, "venv"]));
    }
    roots
        .into_iter()
        .map(|root| abs_or_as_given(machine, &root))
        .collect()
}

/// Unix keeps pip in `bin/pip`; Windows in `Scripts\pip.exe`, which Go
/// still looks for second on Unix.
const VENV_PIP_LAYOUTS: [&str; 2] = ["bin/pip", "Scripts/pip.exe"];

fn venv_pip_in(machine: &Machine<'_>, root: &str) -> String {
    for rel in VENV_PIP_LAYOUTS {
        let candidate = gopath::join(&[root, rel]);
        if gopath::is_file(&candidate) {
            return abs_or_as_given(machine, &candidate);
        }
    }
    String::new()
}

/// The project virtualenv's pip and whether it is there. A virtualenv
/// without pip, such as one `uv venv` made, still names where pip would be.
pub(super) fn venv_pip_target(machine: &Machine<'_>, dir: &str) -> (String, bool) {
    for root in venv_roots(machine, dir) {
        let pip = venv_pip_in(machine, &root);
        if !pip.is_empty() {
            return (pip, true);
        }
        if gopath::exists(&gopath::join(&[&root, "pyvenv.cfg"])) {
            let target = gopath::join(&[&root, VENV_PIP_LAYOUTS[0]]);
            return (abs_or_as_given(machine, &target), false);
        }
    }
    (String::new(), false)
}
