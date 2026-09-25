//! `internal/setup/installer.go`: the command that installs each SDK.

use super::Machine;
use crate::gopath::{self, Walk};
use std::io::Read;
use std::os::unix::process::CommandExt;
use std::process::{Command, Stdio};

#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct InstallResult {
    pub sdk_id: String,
    pub package: String,
    pub command: String,
    pub dry_run: bool,
    pub already_installed: bool,
    pub failed: bool,
    pub failure_reason: String,
    pub warning: String,
    pub success: bool,
}

const MANUAL_INSTALL_SDKS: [&str; 5] = [
    "java-server-sdk",
    "android",
    "android-client-sdk",
    "swift-client-sdk",
    "ios-client-sdk",
];

pub fn requires_manual_install(sdk_id: &str) -> bool {
    MANUAL_INSTALL_SDKS.contains(&sdk_id)
}

/// `PackageInstaller.Install`. Every refusal it can explain comes back as a
/// failed result; only an unknown SDK and a command that fails for a reason
/// it does not recognise are errors.
pub fn install(
    machine: &Machine<'_>,
    dir: &str,
    sdk_id: &str,
    package_manager: &str,
) -> Result<InstallResult, String> {
    let (mut args, package) = install_args(machine, dir, sdk_id, package_manager);
    let result = InstallResult {
        sdk_id: sdk_id.to_string(),
        package: package.clone(),
        ..InstallResult::default()
    };
    let failed = |reason: String| InstallResult {
        failed: true,
        failure_reason: reason,
        ..result.clone()
    };
    if args.is_empty() {
        if !requires_manual_install(sdk_id) {
            return Err(format!(
                "unknown SDK {}: no install command available; specify a supported --sdk-id",
                crate::gostr::quote(sdk_id)
            ));
        }
        return Ok(result);
    }
    if is_installed(machine, dir, sdk_id) {
        return Ok(InstallResult {
            already_installed: true,
            success: true,
            ..result
        });
    }
    if let Some(reason) = pip_less_venv_reason(machine, dir, &package, &args) {
        return Ok(failed(reason));
    }
    if let Some(reason) = missing_tool_reason(machine, &args[0]) {
        return Ok(failed(reason));
    }
    if sdk_id == "dotnet-server-sdk" {
        match dotnet_project_arg(dir) {
            Ok(target) => args.extend(target),
            Err(reason) => return Ok(failed(reason)),
        }
    }
    let (out, outcome) = run(machine, dir, &args);
    let command = args.join(" ");
    if let Err(err) = outcome {
        if let Some(reason) = package_manager_spec_reason(&out) {
            return Ok(InstallResult {
                command,
                ..failed(reason)
            });
        }
        if let Some(reason) = externally_managed_reason(dir, &out) {
            return Ok(failed(reason));
        }
        let out = String::from_utf8_lossy(&out);
        return Err(format!("{command}: {err}\n{}", out.trim()));
    }
    Ok(InstallResult {
        command,
        warning: unrecorded_dependency_warning(dir, &package, &args),
        success: true,
        ..result
    })
}

/// `execRun`: stdout and stderr share one pipe, as `CombinedOutput` has
/// them, and the child is told its directory through `PWD` the way
/// `exec.Cmd` does when it sets `Dir` on an inherited environment.
fn run(machine: &Machine<'_>, dir: &str, args: &[String]) -> (Vec<u8>, Result<(), String>) {
    let program = if args[0].contains('/') {
        std::path::PathBuf::from(&args[0])
    } else {
        match crate::browser::look_path(&args[0], machine.path) {
            Ok(program) => program,
            Err(err) => return (Vec::new(), Err(err)),
        }
    };
    if let Err(err) = std::fs::metadata(dir) {
        return (
            Vec::new(),
            Err(format!("chdir {dir}: {}", gopath::errno_text(&err))),
        );
    }
    let (mut reader, writer) = match std::io::pipe() {
        Ok(pipe) => pipe,
        Err(err) => return (Vec::new(), Err(gopath::errno_text(&err))),
    };
    let mut command = Command::new(&program);
    command
        .arg0(&args[0])
        .args(&args[1..])
        .current_dir(dir)
        .stdin(Stdio::null());
    if let Ok(pwd) = gopath::abs(dir, machine.pwd) {
        command.env("PWD", pwd);
    }
    let spawned = match writer.try_clone() {
        Ok(stdout) => command.stdout(stdout).stderr(writer).spawn(),
        Err(err) => Err(err),
    };
    drop(command);
    let mut child = match spawned {
        Ok(child) => child,
        Err(err) => {
            return (
                Vec::new(),
                Err(format!(
                    "fork/exec {}: {}",
                    program.display(),
                    gopath::errno_text(&err)
                )),
            )
        }
    };
    let mut out = Vec::new();
    let _ = reader.read_to_end(&mut out);
    let outcome = match child.wait() {
        Ok(status) if status.success() => Ok(()),
        Ok(status) => Err(crate::browser::exit_text(status)),
        Err(err) => Err(gopath::errno_text(&err)),
    };
    (out, outcome)
}

/// `dotnet add package` alone needs exactly one project file in the
/// directory it runs in; a solution with its projects below needs one named.
fn dotnet_project_arg(dir: &str) -> Result<Vec<String>, String> {
    if gopath::glob(&gopath::join(&[dir, "*.csproj"])).len() == 1 {
        return Ok(Vec::new());
    }
    let projects = csproj_files(dir);
    match projects.len() {
        0 => Err(
            "no .csproj file found; add LaunchDarkly.ServerSdk to your project manually".into(),
        ),
        1 => {
            let rel = gopath::rel(dir, &projects[0]).unwrap_or_else(|| projects[0].clone());
            Ok(vec!["--project".into(), rel])
        }
        n => Err(format!(
            "found {n} projects in this solution; run `dotnet add package LaunchDarkly.ServerSdk --project <path>` for the one that needs the SDK"
        )),
    }
}

/// Corepack refuses a packageManager field without one exact version. The
/// output has to name package.json, so an unrelated failure that mentions
/// a version keeps its own error.
fn package_manager_spec_reason(out: &[u8]) -> Option<String> {
    if !contains(out, b"package.json") {
        return None;
    }
    let bad_spec = contains(out, b"No version specified")
        || contains(out, b"expected a semver version")
        || contains(out, b"Invalid package manager specification");
    if !bad_spec {
        return None;
    }
    Some(
        "the packageManager field in package.json is not a specification your package \
         manager accepts: it needs one exact version, so a missing version or a range such as \
         \"pnpm@^11.13.0\" is refused. Pin it (for example \"pnpm@11.13.0\") or remove the field, \
         then run setup again."
            .into(),
    )
}

fn externally_managed_reason(dir: &str, out: &[u8]) -> Option<String> {
    if !contains(out, b"externally-managed-environment") {
        return None;
    }
    let target = if dir.is_empty() { "your project" } else { dir };
    Some(format!(
        "this Python is managed by your operating system, so pip will not install into it. \
         Create a virtual environment in {target} and run setup again:\n  \
         python3 -m venv .venv\n  \
         source .venv/bin/activate\n\
         Setup uses .venv automatically once it exists."
    ))
}

/// Fires only when the command is the project virtualenv's own pip and that
/// pip is missing; uv, poetry, and pipenv own their environments.
fn pip_less_venv_reason(
    machine: &Machine<'_>,
    dir: &str,
    pkg: &str,
    args: &[String],
) -> Option<String> {
    let (target, exists) = venv_pip_target(machine, dir);
    if target.is_empty() || exists || args.first() != Some(&target) {
        return None;
    }
    Some(format!(
        "the virtual environment at {} has no pip, which is how `uv venv` creates one. \
         Install into it with `uv pip install {pkg}`, or recreate it with \
         `python3 -m venv .venv`, then run setup again.",
        gopath::dir(&gopath::dir(&target))
    ))
}

fn unrecorded_dependency_warning(dir: &str, pkg: &str, args: &[String]) -> String {
    let Some(tool) = args.first().map(|a| gopath::base(a)) else {
        return String::new();
    };
    if !["pip", "pip3", "pip.exe"].contains(&tool.as_str()) {
        return String::new();
    }
    let Some(manifest) = ["requirements.txt", "requirements/base.txt"]
        .into_iter()
        .find(|name| gopath::exists(&gopath::join(&[dir, name])))
    else {
        return String::new();
    };
    if file_mentions_package(&gopath::join(&[dir, manifest]), pkg) {
        return String::new();
    }
    format!(
        "pip installed {pkg} but did not record it in {manifest}, so a fresh checkout and CI will not have it. \
         Add a line for {pkg} to {manifest}."
    )
}

fn install_hint(tool: &str) -> Option<&'static str> {
    Some(match tool {
        "pip" | "pip3" => {
            "install Python from https://www.python.org/downloads or your package manager"
        }
        "poetry" => "see https://python-poetry.org/docs/#installation",
        "uv" => "see https://docs.astral.sh/uv/getting-started/installation",
        "pipenv" => "see https://pipenv.pypa.io/en/latest/installation.html",
        "pdm" => "see https://pdm-project.org/en/latest/#installation",
        "npm" => "install Node.js from https://nodejs.org",
        "yarn" => "see https://yarnpkg.com/getting-started/install",
        "pnpm" => "see https://pnpm.io/installation",
        "bun" => "see https://bun.sh/docs/installation",
        "bundle" => "run `gem install bundler`",
        "gem" => "install Ruby from https://www.ruby-lang.org/en/documentation/installation",
        "go" => "install Go from https://go.dev/dl",
        "dotnet" => "install the .NET SDK from https://dotnet.microsoft.com/download",
        _ => return None,
    })
}

fn missing_tool_reason(machine: &Machine<'_>, tool: &str) -> Option<String> {
    if machine.on_path(tool) {
        return None;
    }
    Some(match install_hint(tool) {
        Some(hint) => format!("{tool} is not installed or not on your PATH — {hint}"),
        None => format!("{tool} is not installed or not on your PATH"),
    })
}

/// `IsInstalled`: the SDK's package is named in one of the manifests its
/// language keeps dependencies in.
fn is_installed(machine: &Machine<'_>, dir: &str, sdk_id: &str) -> bool {
    let (_, pkg) = install_args(machine, dir, sdk_id, "");
    if pkg.is_empty() {
        return false;
    }
    let manifests: &[&str] = match sdk_id {
        "react-client-sdk" | "react-native" | "node-server" | "js-client-sdk" => &["package.json"],
        "go-server-sdk" => &["go.mod", "go.sum"],
        "python-server-sdk" => &[
            "requirements.txt",
            "pyproject.toml",
            "setup.py",
            "Pipfile",
            "uv.lock",
        ],
        "ruby-server-sdk" => &["Gemfile", "Gemfile.lock"],
        "dotnet-server-sdk" => {
            return csproj_files(dir)
                .iter()
                .any(|file| file_mentions_package(file, &pkg))
        }
        _ => return false,
    };
    manifests
        .iter()
        .any(|manifest| file_mentions_package(&gopath::join(&[dir, manifest]), &pkg))
}

fn file_mentions_package(path: &str, pkg: &str) -> bool {
    gopath::read(path).is_some_and(|content| mentions_package(&content, pkg.as_bytes()))
}

/// `pkg` counts only as a whole name: a quote, whitespace, or operator on
/// each side, so `@launchdarkly/node-server-sdk-redis` does not count as the
/// SDK. Go tests the neighbouring byte, not the neighbouring character.
fn mentions_package(content: &[u8], pkg: &[u8]) -> bool {
    let mut from = 0;
    while let Some(found) = find(&content[from..], pkg) {
        let at = from + found;
        let end = at + pkg.len();
        let before_ok = at == 0 || !is_package_name_byte(content[at - 1]);
        let after_ok = end == content.len() || !is_package_name_byte(content[end]);
        if before_ok && after_ok {
            return true;
        }
        from = at + 1;
    }
    false
}

fn is_package_name_byte(b: u8) -> bool {
    b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.' | b'/' | b'@')
}

fn find(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    if needle.is_empty() {
        return Some(0);
    }
    haystack
        .windows(needle.len())
        .position(|window| window == needle)
}

fn contains(haystack: &[u8], needle: &[u8]) -> bool {
    find(haystack, needle).is_some()
}

/// The project files a .NET install considers: those in `dir`, or failing
/// that every one below it outside build output.
fn csproj_files(dir: &str) -> Vec<String> {
    let matches = gopath::glob(&gopath::join(&[dir, "*.csproj"]));
    if !matches.is_empty() {
        return matches;
    }
    let mut found = Vec::new();
    gopath::walk_dir(dir, &mut |path, entry| {
        if entry.is_dir {
            if matches!(entry.name.as_str(), "bin" | "obj" | ".git") {
                return Walk::SkipDir;
            }
            return Walk::Continue;
        }
        if entry.name.ends_with(".csproj") {
            found.push(path.to_string());
        }
        Walk::Continue
    });
    found.sort();
    found
}

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_dependency_counts_only_as_a_whole_name() {
        let pkg = b"@launchdarkly/node-server-sdk";
        assert!(mentions_package(
            br#"{"@launchdarkly/node-server-sdk": "9"}"#,
            pkg
        ));
        assert!(mentions_package(b"@launchdarkly/node-server-sdk", pkg));
        assert!(!mentions_package(
            br#""@launchdarkly/node-server-sdk-redis""#,
            pkg
        ));
        assert!(mentions_package(
            br#""@launchdarkly/node-server-sdk-redis", "@launchdarkly/node-server-sdk""#,
            pkg
        ));
        assert!(mentions_package(
            b"launchdarkly-server-sdk>=9",
            b"launchdarkly-server-sdk"
        ));
        assert!(!mentions_package(
            b"xlaunchdarkly-server-sdk",
            b"launchdarkly-server-sdk"
        ));
        assert!(!mentions_package(
            b"launchdarkly-server",
            b"launchdarkly-server-sdk"
        ));
    }

    #[test]
    fn a_non_ascii_neighbour_is_a_delimiter_because_go_reads_one_byte() {
        assert!(mentions_package(
            "élaunchdarkly-server-sdk".as_bytes(),
            b"launchdarkly-server-sdk"
        ));
        assert!(mentions_package(
            "launchdarkly-server-sdké".as_bytes(),
            b"launchdarkly-server-sdk"
        ));
    }
}
