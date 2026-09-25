//! The CLI's global flags, and pflag's usage rendering.
//!
//! Help text is part of the product, so the layout here follows pflag's
//! `FlagUsagesWrapped`: build each line, mark where the description starts,
//! then pad every description to the longest mark. Go renders the root's
//! flags unwrapped, so there is no column limit to apply.

use std::fmt::Write as _;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FlagKind {
    /// A string flag. `default` is rendered when it is not empty.
    Str {
        default: &'static str,
    },
    Bool,
    StringSlice,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Flag {
    pub name: &'static str,
    pub shorthand: Option<char>,
    pub usage: &'static str,
    pub kind: FlagKind,
}

impl Flag {
    /// pflag names the value after its type, and prints nothing for a bool.
    fn varname(&self) -> &'static str {
        match self.kind {
            FlagKind::Str { .. } => "string",
            FlagKind::Bool => "",
            FlagKind::StringSlice => "strings",
        }
    }

    /// Only a non-zero default is shown, which is why an empty access token
    /// and an unset field list print no default at all.
    fn default_suffix(&self) -> String {
        match self.kind {
            FlagKind::Str { default } if !default.is_empty() => format!(" (default {default:?})"),
            _ => String::new(),
        }
    }

    pub fn takes_value(&self) -> bool {
        !matches!(self.kind, FlagKind::Bool)
    }
}

/// The persistent flags `NewRootCommand` declares, in declaration order.
pub fn persistent_flags(default_output: &'static str) -> Vec<Flag> {
    vec![
        Flag {
            name: "access-token",
            shorthand: None,
            usage: "LaunchDarkly access token with write-level access",
            kind: FlagKind::Str { default: "" },
        },
        Flag {
            name: "base-uri",
            shorthand: None,
            usage: "LaunchDarkly base URI",
            kind: FlagKind::Str {
                default: crate::settings::BASE_URI_DEFAULT,
            },
        },
        Flag {
            name: "analytics-opt-out",
            shorthand: None,
            usage: "Opt out of analytics tracking",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "output",
            shorthand: Some('o'),
            usage: "Output format: json, plaintext, or markdown (default: plaintext in a terminal, json otherwise)",
            kind: FlagKind::Str {
                default: default_output,
            },
        },
        Flag {
            name: "json",
            shorthand: None,
            usage: "Output JSON format (shorthand for --output json)",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "fields",
            shorthand: None,
            usage: "Comma-separated list of top-level fields to include in JSON output (e.g., --fields key,name,kind)",
            kind: FlagKind::StringSlice,
        },
    ]
}

/// The flags `config` declares, plus the help flag Cobra gives every command.
pub fn config_flags() -> Vec<Flag> {
    vec![
        Flag {
            name: "help",
            shorthand: Some('h'),
            usage: "help for config",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "list",
            shorthand: None,
            usage: "List configs",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "set",
            shorthand: None,
            usage: "Set a config field to a value",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "unset",
            shorthand: None,
            usage: "Unset a config field",
            kind: FlagKind::Str { default: "" },
        },
    ]
}

/// Cobra adds these to the root as it executes a command. The unknown-help-topic
/// path prints its usage before that happens, so it lists neither.
pub fn implicit_flags() -> Vec<Flag> {
    vec![
        Flag {
            name: "help",
            shorthand: Some('h'),
            usage: "help for ldcli",
            kind: FlagKind::Bool,
        },
        Flag {
            name: "version",
            shorthand: Some('v'),
            usage: "version for ldcli",
            kind: FlagKind::Bool,
        },
    ]
}

/// Render a flag block the way pflag does, with no wrapping.
pub fn flag_usages(flags: &[Flag]) -> String {
    let mut sorted: Vec<&Flag> = flags.iter().collect();
    sorted.sort_by_key(|flag| flag.name);

    // Each entry is the head of the line and its description.
    let mut lines: Vec<(String, String)> = Vec::new();
    let mut maxlen = 0usize;
    for flag in sorted {
        let mut head = match flag.shorthand {
            Some(short) => format!("  -{short}, --{}", flag.name),
            None => format!("      --{}", flag.name),
        };
        let varname = flag.varname();
        if !varname.is_empty() {
            head.push(' ');
            head.push_str(varname);
        }
        // pflag measures the head plus the marker byte it inserts here.
        maxlen = maxlen.max(head.len() + 1);
        lines.push((head, format!("{}{}", flag.usage, flag.default_suffix())));
    }

    let mut out = String::new();
    for (head, description) in lines {
        // Go prints head, padding, and description with a space between each,
        // and the padding itself is deliberately one short.
        let padding = " ".repeat(maxlen - head.len());
        let _ = writeln!(out, "{head} {padding} {description}");
    }
    out
}

/// Trailing whitespace on the last line is trimmed by the Go template.
pub fn trim_trailing_whitespace(text: &str) -> &str {
    text.trim_end_matches([' ', '\t', '\n', '\r'])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn descriptions_line_up_on_the_longest_flag() {
        let rendered = flag_usages(&persistent_flags("json"));
        let lines: Vec<&str> = rendered.lines().collect();
        // --access-token string is the longest head, so every description
        // starts three columns past it.
        assert_eq!(
            lines[0],
            "      --access-token string   LaunchDarkly access token with write-level access"
        );
        let columns: Vec<usize> = lines
            .iter()
            .map(|line| line.len() - line.trim_start_matches(' ').len())
            .collect();
        assert!(columns.iter().all(|c| *c == 2 || *c == 6), "{columns:?}");
    }

    #[test]
    fn only_a_non_empty_string_default_is_printed() {
        let rendered = flag_usages(&persistent_flags("json"));
        assert!(rendered.contains(r#"(default "https://app.launchdarkly.com")"#));
        assert!(rendered.contains(r#"(default "json")"#));
        // An empty access token and an unset field list have no default.
        assert!(!rendered.contains("access token with write-level access (default"));
        assert!(!rendered.contains("--fields key,name,kind) (default"));
    }

    #[test]
    fn flags_are_listed_in_name_order_not_declaration_order() {
        let rendered = flag_usages(&persistent_flags("json"));
        let names: Vec<&str> = rendered
            .lines()
            .map(|line| {
                line.trim_start()
                    .trim_start_matches("-o, ")
                    .trim_start_matches("-h, ")
            })
            .map(|line| line.split_whitespace().next().unwrap_or_default())
            .collect();
        assert_eq!(
            names,
            vec![
                "--access-token",
                "--analytics-opt-out",
                "--base-uri",
                "--fields",
                "--json",
                "--output",
            ]
        );
    }
}
