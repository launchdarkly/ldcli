//! Fail closed when a Go command has neither a help transcript nor an exemption.

use anyhow::{anyhow, Result};
use serde::Deserialize;
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub struct Exemption {
    pub name: String,
    #[serde(default = "default_kind")]
    pub kind: String,
    #[serde(default)]
    pub commands: Vec<String>,
    pub reason: String,
    pub substitute: String,
    /// When true, the substitute also runs against the Rust binary. Like a
    /// case's `rust` field, this stays false until that command exists.
    #[serde(default)]
    pub rust: bool,
}

fn default_kind() -> String {
    "command".into()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CoverageFailure {
    pub command: String,
    pub message: String,
}

#[derive(Debug, Deserialize)]
struct ExemptionFile {
    #[serde(default, rename = "exemption")]
    exemptions: Vec<Exemption>,
}

pub fn load_exemptions(path: &Path) -> Result<Vec<Exemption>> {
    let text = fs::read_to_string(path)
        .map_err(|err| anyhow!("read exemptions {}: {err}", path.display()))?;
    let parsed: ExemptionFile = toml::from_str(&text)
        .map_err(|err| anyhow!("parse exemptions {}: {err}", path.display()))?;
    for exemption in &parsed.exemptions {
        if exemption.substitute.trim().is_empty() {
            return Err(anyhow!(
                "exemption '{}' has no substitute assertion",
                exemption.name
            ));
        }
        if exemption.reason.trim().is_empty() {
            return Err(anyhow!("exemption '{}' has no reason", exemption.name));
        }
    }
    Ok(parsed.exemptions)
}

/// The state of every command in the dump, plus the reasons the gate failed.
/// Both travel together so a failing run still prints the full report.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Coverage {
    pub report: Vec<String>,
    pub failures: Vec<CoverageFailure>,
}

impl Coverage {
    pub fn passed(&self) -> bool {
        self.failures.is_empty()
    }
}

/// `commands` are dump paths such as `ldcli quickstart`.
/// `help_commands` are the same paths that have a `--help` transcript.
pub fn check_coverage(
    commands: &[String],
    help_commands: &BTreeSet<String>,
    exemptions: &[Exemption],
) -> Coverage {
    let mut exempt: BTreeMap<&str, &Exemption> = BTreeMap::new();
    let mut failures = Vec::new();
    for exemption in exemptions {
        if exemption.substitute.trim().is_empty() {
            failures.push(CoverageFailure {
                command: exemption.name.clone(),
                message: format!("exemption '{}' has no substitute assertion", exemption.name),
            });
            continue;
        }
        if exemption.kind == "behavior" {
            continue;
        }
        for command in &exemption.commands {
            exempt.insert(command.as_str(), exemption);
        }
    }

    let mut report = Vec::new();
    for command in commands {
        if help_commands.contains(command) {
            report.push(format!("covered {command}"));
            continue;
        }
        if let Some(exemption) = exempt.get(command.as_str()) {
            report.push(format!("exempt {command} ({})", exemption.name));
            continue;
        }
        report.push(format!("UNCOVERED {command}"));
        failures.push(CoverageFailure {
            command: command.clone(),
            message: format!("{command} has neither a help transcript nor an exemption"),
        });
    }

    Coverage { report, failures }
}

pub fn load_command_list(path: &Path) -> Result<Vec<String>> {
    let text = fs::read_to_string(path)
        .map_err(|err| anyhow!("read commands {}: {err}", path.display()))?;
    let mut commands = Vec::new();
    let mut seen = BTreeSet::new();
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if seen.insert(line.to_string()) {
            commands.push(line.to_string());
        }
    }
    Ok(commands)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn exemption(name: &str, commands: &[&str], substitute: &str) -> Exemption {
        Exemption {
            name: name.into(),
            kind: "command".into(),
            commands: commands.iter().map(|s| (*s).to_string()).collect(),
            reason: "because".into(),
            substitute: substitute.into(),
            rust: false,
        }
    }

    #[test]
    fn fake_command_without_case_or_exemption_fails_and_names_it() {
        let commands = vec!["ldcli".into(), "ldcli not-real".into()];
        let mut help = BTreeSet::new();
        help.insert("ldcli".into());
        let coverage = check_coverage(&commands, &help, &[]);
        assert!(!coverage.passed());
        assert_eq!(coverage.failures.len(), 1);
        assert_eq!(coverage.failures[0].command, "ldcli not-real");
        assert!(
            coverage.failures[0].message.contains("ldcli not-real"),
            "{}",
            coverage.failures[0].message
        );
    }

    #[test]
    fn a_failing_gate_still_reports_the_commands_that_passed() {
        let commands = vec!["ldcli".into(), "ldcli not-real".into()];
        let mut help = BTreeSet::new();
        help.insert("ldcli".into());
        let coverage = check_coverage(&commands, &help, &[]);
        assert_eq!(
            coverage.report,
            vec!["covered ldcli", "UNCOVERED ldcli not-real"]
        );
    }

    #[test]
    fn quickstart_without_help_or_exemption_fails() {
        let commands = vec!["ldcli".into(), "ldcli quickstart".into()];
        let mut help = BTreeSet::new();
        help.insert("ldcli".into());
        let coverage = check_coverage(&commands, &help, &[]);
        assert!(coverage
            .failures
            .iter()
            .any(|f| f.command == "ldcli quickstart"));
    }

    #[test]
    fn exemption_without_substitute_fails() {
        let text = r#"
[[exemption]]
name = "broken"
commands = ["ldcli quickstart"]
reason = "wizard"
substitute = ""
"#;
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("exemptions.toml");
        std::fs::write(&path, text).unwrap();
        let err = load_exemptions(&path).unwrap_err();
        assert!(err.to_string().contains("no substitute"), "{err}");
    }

    #[test]
    fn exemption_covers_a_hidden_command() {
        let commands = vec!["ldcli quickstart".into()];
        let help = BTreeSet::new();
        let exemptions = vec![exemption(
            "quickstart-wizard",
            &["ldcli quickstart"],
            "help",
        )];
        let coverage = check_coverage(&commands, &help, &exemptions);
        assert!(coverage.passed());
        assert!(
            coverage.report[0].contains("exempt ldcli quickstart"),
            "{:?}",
            coverage.report
        );
    }
}
