//! `internal/setup/detector.go`: what a project directory is written in and
//! how it installs packages.

use super::install::install_args;
use super::Machine;
use crate::godecode::{self, Kind, Struct};
use crate::gopath::{self, Walk};

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DetectResult {
    pub language: String,
    pub framework: String,
    pub package_manager: String,
    pub sdk_id: String,
    pub entry_point: String,
    pub entry_point_exists: bool,
    pub package_manager_confidence: String,
    pub package_manager_reason: String,
}

pub const DEFINITE: &str = "definite";
pub const AMBIGUOUS: &str = "ambiguous";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Candidate {
    pub name: String,
    pub installed: bool,
    pub command: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Choice {
    pub name: String,
    pub confidence: &'static str,
    pub reason: String,
    pub candidates: Vec<Candidate>,
}

pub const NOT_DETECTED: &str =
    "could not detect project language from directory; try specifying --sdk-id manually";

/// A root package.json is often only build tooling, so the backend
/// manifests are tried first and Node claims only a project with nothing
/// else.
pub fn detect(machine: &Machine<'_>, dir: &str) -> Result<DetectResult, String> {
    let detectors: [fn(&str) -> Option<DetectResult>; 7] = [
        detect_go,
        detect_python,
        detect_ruby,
        detect_java,
        detect_swift,
        detect_dotnet,
        detect_node,
    ];
    for detector in detectors {
        if let Some(mut result) = detector(dir) {
            let choice = package_manager_choice_for(machine, dir, &result.sdk_id);
            if !choice.name.is_empty() {
                result.package_manager = choice.name;
            }
            result.package_manager_confidence = choice.confidence.into();
            result.package_manager_reason = choice.reason;
            return Ok(result);
        }
    }
    Err(NOT_DETECTED.into())
}

fn result(
    language: &str,
    framework: &str,
    package_manager: &str,
    sdk_id: &str,
    (entry_point, entry_point_exists): (String, bool),
) -> Option<DetectResult> {
    Some(DetectResult {
        language: language.into(),
        framework: framework.into(),
        package_manager: package_manager.into(),
        sdk_id: sdk_id.into(),
        entry_point,
        entry_point_exists,
        ..DetectResult::default()
    })
}

fn at(dir: &str, file: &str) -> String {
    gopath::join(&[dir, file])
}

const PACKAGE_DEPENDENCIES: Struct<'static> = Struct {
    name: "",
    type_string: "struct { Dependencies map[string]string \"json:\\\"dependencies\\\"\"; DevDependencies map[string]string \"json:\\\"devDependencies\\\"\" }",
    fields: &[
        ("dependencies", Kind::StringMap),
        ("devDependencies", Kind::StringMap),
    ],
};

fn detect_node(dir: &str) -> Option<DetectResult> {
    let bytes = gopath::read(&at(dir, "package.json"))?;
    let fields = godecode::unmarshal(&bytes, &PACKAGE_DEPENDENCIES).ok()?;
    let has = |dep: &str| {
        fields
            .iter()
            .any(|field| field.keys().iter().any(|k| k == dep))
    };
    let pm = node_pm_signals(dir).best("npm");

    // Only the instrumentation hook is sure to stay out of the browser
    // bundle, so a Next.js app's server SDK goes there.
    if has("next") {
        let ep = entry_point(
            dir,
            "instrumentation.ts",
            &[
                "instrumentation.ts",
                "instrumentation.js",
                "src/instrumentation.ts",
                "src/instrumentation.js",
            ],
        );
        return result("JavaScript", "Next.js", &pm, "node-server", ep);
    }
    if has("react-native") {
        let ep = entry_point(
            dir,
            "index.js",
            &[
                "src/App.tsx",
                "src/App.jsx",
                "src/App.js",
                "src/index.tsx",
                "src/index.jsx",
                "src/index.js",
                "App.tsx",
                "App.js",
                "index.js",
            ],
        );
        return result("JavaScript", "React Native", &pm, "react-native", ep);
    }
    if has("react") {
        let ep = entry_point(
            dir,
            "src/App.tsx",
            &[
                "src/App.tsx",
                "src/App.jsx",
                "src/App.js",
                "src/main.tsx",
                "src/main.jsx",
                "src/index.tsx",
                "src/index.jsx",
                "src/index.js",
                "index.js",
            ],
        );
        return result("JavaScript", "React", &pm, "react-client-sdk", ep);
    }
    let client_frameworks = [
        ("backbone", "Backbone"),
        ("svelte", "Svelte"),
        ("vue", "Vue"),
        ("@angular/core", "Angular"),
        ("ember-source", "Ember"),
        ("preact", "Preact"),
    ];
    for (dep, framework) in client_frameworks {
        if has(dep) {
            let ep = entry_point(
                dir,
                "src/main.ts",
                &[
                    "src/App.tsx",
                    "src/App.jsx",
                    "src/App.js",
                    "src/index.tsx",
                    "src/index.jsx",
                    "src/index.js",
                    "src/main.ts",
                    "src/main.js",
                    "index.js",
                ],
            );
            return result("JavaScript", framework, &pm, "js-client-sdk", ep);
        }
    }
    let ep = entry_point_for(dir, "node-server");
    result("JavaScript", "", &pm, "node-server", ep)
}

const PACKAGE_MANAGER_FIELD: Struct<'static> = Struct {
    name: "",
    type_string: "struct { PackageManager string \"json:\\\"packageManager\\\"\" }",
    fields: &[("packageManager", Kind::String)],
};

/// `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`. Go's `\d` is
/// ASCII only.
fn is_exact_semver(version: &str) -> bool {
    let tag = |s: &str| {
        !s.is_empty()
            && s.bytes()
                .all(|c| c.is_ascii_alphanumeric() || c == b'.' || c == b'-')
    };
    let (core, build) = match version.split_once('+') {
        Some((core, build)) => (core, Some(build)),
        None => (version, None),
    };
    if build.is_some_and(|b| !tag(b)) {
        return false;
    }
    let (numbers, pre) = match core.split_once('-') {
        Some((numbers, pre)) => (numbers, Some(pre)),
        None => (core, None),
    };
    if pre.is_some_and(|p| !tag(p)) {
        return false;
    }
    let parts: Vec<&str> = numbers.split('.').collect();
    parts.len() == 3
        && parts
            .iter()
            .all(|p| !p.is_empty() && p.bytes().all(|c| c.is_ascii_digit()))
}

/// Corepack runs only `<name>@<exact version>`, so anything else in the
/// packageManager field is no declaration at all.
fn corepack_pm(dir: &str) -> String {
    let Some(bytes) = gopath::read(&at(dir, "package.json")) else {
        return String::new();
    };
    let Ok(fields) = godecode::unmarshal(&bytes, &PACKAGE_MANAGER_FIELD) else {
        return String::new();
    };
    let spec = fields[0].as_str();
    let (name, version) = spec.split_once('@').unwrap_or((spec, ""));
    if !is_exact_semver(version) {
        return String::new();
    }
    match name {
        "npm" | "yarn" | "pnpm" | "bun" => name.into(),
        _ => String::new(),
    }
}

#[derive(Default)]
struct Signals {
    declared: String,
    locked: Vec<String>,
    unactionable: bool,
}

impl Signals {
    fn add_locked(&mut self, pm: &str) {
        if pm.is_empty() {
            self.unactionable = true;
        } else if !self.locked.iter().any(|existing| existing == pm) {
            self.locked.push(pm.into());
        }
    }

    fn best(&self, fallback: &str) -> String {
        if !self.declared.is_empty() {
            return self.declared.clone();
        }
        self.locked
            .first()
            .cloned()
            .unwrap_or_else(|| fallback.into())
    }

    fn choose(
        &self,
        options: &[&str],
        fallback: &str,
        argv_for: impl Fn(&str) -> Vec<String>,
        on_path: impl Fn(&str) -> bool,
    ) -> Choice {
        // Installed follows the executable the command runs, not the label:
        // "pip" may well run pip3.
        let candidates: Vec<Candidate> = options
            .iter()
            .map(|name| {
                let argv = argv_for(name);
                Candidate {
                    name: (*name).into(),
                    installed: argv.first().is_some_and(|tool| on_path(tool)),
                    command: argv.join(" "),
                }
            })
            .collect();
        let ambiguous = |name: &str, reason: String| Choice {
            name: name.into(),
            confidence: AMBIGUOUS,
            reason,
            candidates: candidates.clone(),
        };
        let definite = |name: &str| Choice {
            name: name.into(),
            confidence: DEFINITE,
            reason: String::new(),
            candidates: Vec::new(),
        };
        if !self.declared.is_empty() {
            return definite(&self.declared);
        }
        match self.locked.len() {
            1 => definite(&self.locked[0]),
            n if n > 1 => ambiguous(
                &self.locked[0],
                format!(
                    "this project is set up for more than one manager ({})",
                    self.locked.join(", ")
                ),
            ),
            _ if self.unactionable => ambiguous(
                fallback,
                "this project is managed by a tool that cannot add dependencies for us".into(),
            ),
            _ => ambiguous(
                fallback,
                "this project doesn't say which package manager it uses".into(),
            ),
        }
    }
}

fn node_pm_signals(dir: &str) -> Signals {
    let declared = corepack_pm(dir);
    if !declared.is_empty() {
        return Signals {
            declared,
            ..Signals::default()
        };
    }
    let mut signals = Signals::default();
    for (file, pm) in [
        ("pnpm-lock.yaml", "pnpm"),
        ("yarn.lock", "yarn"),
        ("bun.lock", "bun"),
        ("bun.lockb", "bun"),
        ("package-lock.json", "npm"),
        ("npm-shrinkwrap.json", "npm"),
    ] {
        if gopath::exists(&at(dir, file)) {
            signals.add_locked(pm);
        }
    }
    signals
}

fn detect_go(dir: &str) -> Option<DetectResult> {
    if !gopath::exists(&at(dir, "go.mod")) {
        return None;
    }
    let ep = entry_point(dir, "main.go", &["main.go", "cmd/main.go"]);
    result("Go", "", "go", "go-server-sdk", ep)
}

fn detect_python(dir: &str) -> Option<DetectResult> {
    ["requirements.txt", "pyproject.toml", "setup.py", "Pipfile"]
        .iter()
        .any(|indicator| gopath::exists(&at(dir, indicator)))
        .then(|| entry_point_for(dir, "python-server-sdk"))
        .and_then(|ep| result("Python", "", "", "python-server-sdk", ep))
}

fn python_pm_signals(dir: &str) -> Signals {
    let mut signals = Signals::default();
    for (file, pm) in [
        ("uv.lock", "uv"),
        ("poetry.lock", "poetry"),
        ("pdm.lock", "pdm"),
        ("Pipfile.lock", "pipenv"),
        ("Pipfile", "pipenv"),
    ] {
        if gopath::exists(&at(dir, file)) {
            signals.add_locked(pm);
        }
    }
    let tools = configured_tools(dir);
    // hatch cannot add a dependency, so it is recorded as a signal nothing
    // can act on, which keeps the project ambiguous.
    for (tool, pm) in [
        ("poetry", "poetry"),
        ("uv", "uv"),
        ("pdm", "pdm"),
        ("hatch", ""),
    ] {
        if tools.iter().any(|t| t == tool) {
            signals.add_locked(pm);
        }
    }
    signals
}

/// The `[tool.*]` tables pyproject.toml declares. A file that does not parse
/// declares none. go-toml falls back to a case-insensitive match for each
/// key, so `[TOOL.uv]` decodes into the same map as `[tool.poetry]`, and a
/// `tool` key that is not a table fails the whole decode.
fn configured_tools(dir: &str) -> Vec<String> {
    let Some(bytes) = gopath::read(&at(dir, "pyproject.toml")) else {
        return Vec::new();
    };
    let Ok(text) = String::from_utf8(bytes) else {
        return Vec::new();
    };
    let Ok(doc) = text.parse::<toml::Table>() else {
        return Vec::new();
    };
    let mut tools = Vec::new();
    for (key, value) in &doc {
        if key.to_lowercase() != "tool" {
            continue;
        }
        let toml::Value::Table(table) = value else {
            return Vec::new();
        };
        tools.extend(table.keys().cloned());
    }
    tools
}

fn detect_ruby(dir: &str) -> Option<DetectResult> {
    let found = ["Gemfile", "Gemfile.lock", "config.ru"]
        .iter()
        .any(|indicator| gopath::exists(&at(dir, indicator)));
    if !found && gopath::glob(&at(dir, "*.gemspec")).is_empty() {
        return None;
    }
    let ep = entry_point_for(dir, "ruby-server-sdk");
    let pm = ruby_pm_signals(dir).best("gem");
    result("Ruby", "", &pm, "ruby-server-sdk", ep)
}

/// A Gemfile is Bundler's own manifest; a gemspec alone could be developed
/// either way.
fn ruby_pm_signals(dir: &str) -> Signals {
    let mut signals = Signals::default();
    if gopath::exists(&at(dir, "Gemfile")) {
        signals.add_locked("bundle");
    }
    signals
}

fn detect_java(dir: &str) -> Option<DetectResult> {
    for indicator in ["pom.xml", "build.gradle", "build.gradle.kts"] {
        if !gopath::exists(&at(dir, indicator)) {
            continue;
        }
        let pm = if indicator == "pom.xml" {
            "mvn"
        } else {
            "gradle"
        };
        for manifest in [
            "app/src/main/AndroidManifest.xml",
            "src/main/AndroidManifest.xml",
        ] {
            if !gopath::exists(&at(dir, manifest)) {
                continue;
            }
            let src_root = manifest.trim_end_matches("/AndroidManifest.xml");
            let java = format!("{src_root}/java");
            let kotlin = format!("{src_root}/kotlin");
            let ep = entry_point(
                dir,
                &format!("{src_root}/java/MainActivity.kt"),
                &[
                    &find_file_under(dir, &java, &["MainActivity.kt", "MainActivity.java"]),
                    &find_file_under(dir, &kotlin, &["MainActivity.kt"]),
                ],
            );
            return result("Java", "", "gradle", "android", ep);
        }
        let ep = entry_point(
            dir,
            "src/main/java/Main.java",
            &[&find_file_under(
                dir,
                "src/main/java",
                &["Main.java", "Application.java", "App.java"],
            )],
        );
        return result("Java", "", pm, "java-server-sdk", ep);
    }
    None
}

fn detect_swift(dir: &str) -> Option<DetectResult> {
    let pm = if gopath::exists(&at(dir, "Podfile")) {
        "cocoapods"
    } else {
        "spm"
    };
    let swift_entry_point =
        |app_root: &str| entry_point(dir, "App.swift", &swift_entry_candidates(dir, app_root));
    if ["Package.swift", "Podfile"]
        .iter()
        .any(|f| gopath::exists(&at(dir, f)))
    {
        let ep = swift_entry_point(&xcode_app_root(dir));
        return result("Swift", "", pm, "swift-client-sdk", ep);
    }
    let app_root = xcode_app_root(dir);
    if app_root.is_empty() {
        return None;
    }
    let ep = swift_entry_point(&app_root);
    result("Swift", "", pm, "swift-client-sdk", ep)
}

/// Any-name matches are confined to a package with a single target: across
/// several there is no telling an executable's entry file from a library's.
fn swift_entry_candidates(dir: &str, app_root: &str) -> Vec<String> {
    let mut candidates: Vec<String> = ["App.swift", "ContentView.swift", "AppDelegate.swift"]
        .iter()
        .map(|c| (*c).to_string())
        .collect();
    candidates.push(find_file_under(
        dir,
        app_root,
        &["*App.swift", "ContentView.swift", "AppDelegate.swift"],
    ));
    let target = sole_subdir(dir, "Sources");
    if !target.is_empty() {
        let named = format!("{}.swift", gopath::base(&target));
        candidates.push(find_file_under(
            dir,
            &target,
            &["main.swift", &named, "*App.swift", "*.swift"],
        ));
    }
    candidates
}

/// `root`'s only subdirectory, relative to `dir`, or nothing when there is
/// not exactly one.
fn sole_subdir(dir: &str, root: &str) -> String {
    let path = at(dir, root);
    if std::fs::read_dir(&path).is_err() {
        return String::new();
    }
    let mut found = String::new();
    for entry in gopath::read_dir(&path) {
        if !entry.is_dir {
            continue;
        }
        if !found.is_empty() {
            return String::new();
        }
        found = gopath::join(&[root, &entry.name]);
    }
    found
}

/// Xcode keeps an app's code in a directory named after the project.
fn xcode_app_root(dir: &str) -> String {
    match gopath::glob(&at(dir, "*.xcodeproj")).first() {
        Some(project) => {
            let name = gopath::base(project);
            name.strip_suffix(".xcodeproj").unwrap_or(&name).to_string()
        }
        None => String::new(),
    }
}

fn detect_dotnet(dir: &str) -> Option<DetectResult> {
    for pattern in ["*.csproj", "*.sln"] {
        if !gopath::glob(&at(dir, pattern)).is_empty() {
            let ep = entry_point(
                dir,
                "Program.cs",
                &["Program.cs", "Startup.cs", "src/Program.cs"],
            );
            return result("C#", "", "dotnet", "dotnet-server-sdk", ep);
        }
    }
    None
}

/// Where each SDK that writes to a file looks for its entry point, and what
/// it suggests when none is there.
fn sdk_entry_points(sdk_id: &str) -> Option<(&'static str, &'static [&'static str])> {
    match sdk_id {
        "node-server" => Some((
            "index.js",
            &[
                "src/index.ts",
                "src/index.js",
                "src/main.ts",
                "src/main.js",
                "index.ts",
                "index.js",
                "server.ts",
                "server.js",
                "app.ts",
                "app.js",
            ],
        )),
        "python-server-sdk" => Some((
            "main.py",
            &["src/main.py", "manage.py", "app.py", "main.py"],
        )),
        "ruby-server-sdk" => Some(("main.rb", &["config.ru", "app.rb", "main.rb"])),
        _ => None,
    }
}

/// `EntryPointFor`.
pub fn entry_point_for(dir: &str, sdk_id: &str) -> (String, bool) {
    match sdk_entry_points(sdk_id) {
        Some((fallback, candidates)) => entry_point(dir, fallback, candidates),
        None => (String::new(), false),
    }
}

/// The first candidate that is a file under `dir`, or the fallback, joined
/// to `dir`. Empty candidates are lookups that found nothing.
fn entry_point<S: AsRef<str>>(dir: &str, fallback: &str, candidates: &[S]) -> (String, bool) {
    for candidate in candidates {
        let candidate = candidate.as_ref();
        if candidate.is_empty() {
            continue;
        }
        let path = at(dir, candidate);
        if gopath::is_file(&path) {
            return (path, true);
        }
    }
    (at(dir, fallback), false)
}

/// The first file under `root` whose name matches, trying each name in turn,
/// relative to `dir`. A leading `*` matches by suffix. An empty root matches
/// nothing rather than walking the whole project.
fn find_file_under(dir: &str, root: &str, names: &[&str]) -> String {
    if root.is_empty() {
        return String::new();
    }
    let matches = |base: &str, name: &str| match name.strip_prefix('*') {
        Some(suffix) => base.ends_with(suffix),
        None => base == name,
    };
    for name in names {
        let mut found = String::new();
        gopath::walk_dir(&at(dir, root), &mut |path, entry| {
            if !entry.is_dir && matches(&entry.name, name) {
                found = path.to_string();
                return Walk::SkipAll;
            }
            Walk::Continue
        });
        if !found.is_empty() {
            if let Some(rel) = gopath::rel(dir, &found) {
                return rel;
            }
        }
    }
    String::new()
}

/// `PackageManagerChoiceFor`. Go and .NET have one toolchain, and Java,
/// Android, and Swift are installed by hand, so there is nothing to ask.
pub fn package_manager_choice_for(machine: &Machine<'_>, dir: &str, sdk_id: &str) -> Choice {
    let argv_for = |pm: &str| install_args(machine, dir, sdk_id, pm).0;
    let on_path = |tool: &str| machine.on_path(tool);
    let definite = |name: &str| Choice {
        name: name.into(),
        confidence: DEFINITE,
        reason: String::new(),
        candidates: Vec::new(),
    };
    match sdk_id {
        "node-server" | "js-client-sdk" | "react-client-sdk" | "react-native" => {
            node_pm_signals(dir).choose(&["npm", "yarn", "pnpm", "bun"], "npm", argv_for, on_path)
        }
        "python-server-sdk" => python_pm_signals(dir).choose(
            &["pip", "uv", "poetry", "pipenv", "pdm"],
            "pip",
            argv_for,
            on_path,
        ),
        "ruby-server-sdk" => {
            ruby_pm_signals(dir).choose(&["bundle", "gem"], "gem", argv_for, on_path)
        }
        "go-server-sdk" => definite("go"),
        "dotnet-server-sdk" => definite("dotnet"),
        _ => definite(""),
    }
}
