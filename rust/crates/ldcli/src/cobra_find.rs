//! Cobra's `Command.Find`: which command a list of words names.
//!
//! `help <topic>` and the deprecation notice both depend on it. Find runs
//! before Cobra adds the help and version flags to any command, so `-h` and
//! `-v` are unknown to it, and an unknown flag is assumed to take a value.
//! That is why `ldcli -h quickstart` never reaches quickstart: Find skips
//! `quickstart` as the value of `-h`.

/// One command in the tree Find walks. Only the Rust port's commands are
/// here; a word naming any other command is left over, as an unknown one is.
pub struct Node {
    pub name: &'static str,
    /// Local flags that take no value. The root's persistent bools apply to
    /// every node.
    pub bools: &'static [&'static str],
    pub children: &'static [Node],
}

const PERSISTENT_BOOLS: [&str; 2] = ["analytics-opt-out", "json"];

pub const TREE: Node = Node {
    name: "ldcli",
    bools: &[],
    children: &[
        Node {
            name: "config",
            bools: &["list", "set"],
            children: &[],
        },
        Node {
            name: "help",
            bools: &[],
            children: &[],
        },
        Node {
            name: "login",
            bools: &[],
            children: &[],
        },
        Node {
            name: "quickstart",
            bools: &[],
            children: &[],
        },
        Node {
            name: "setup",
            bools: &[],
            children: &[
                Node {
                    name: "detect",
                    bools: &[],
                    children: &[],
                },
                Node {
                    name: "init",
                    bools: &[],
                    children: &[],
                },
                Node {
                    name: "install",
                    bools: &["dry-run"],
                    children: &[],
                },
            ],
        },
        Node {
            name: "signup",
            bools: &[],
            children: &[],
        },
        Node {
            name: "whoami",
            bools: &[],
            children: &[],
        },
    ],
};

impl Node {
    fn is_bool(&self, name: &str) -> bool {
        PERSISTENT_BOOLS.contains(&name) || self.bools.contains(&name)
    }

    /// Does this long-flag word take the next word as its value? A short
    /// flag always does: none of the ported commands has a bool shorthand
    /// that Find can see.
    fn consumes_next(&self, word: &str) -> bool {
        if word.contains('=') {
            return false;
        }
        match word.strip_prefix("--") {
            Some(name) => !self.is_bool(name),
            None => word.starts_with('-') && word.len() == 2,
        }
    }

    /// Cobra's `stripFlags`: the words that are not flags or flag values.
    /// `--` ends the scan and drops everything after it.
    pub fn strip_flags(&self, args: &[String]) -> Vec<String> {
        let mut commands = Vec::new();
        let mut rest = args.iter();
        while let Some(word) = rest.next() {
            if word == "--" {
                break;
            }
            if self.consumes_next(word) {
                if rest.len() <= 1 {
                    break;
                }
                rest.next();
                continue;
            }
            if !word.is_empty() && !word.starts_with('-') {
                commands.push(word.clone());
            }
        }
        commands
    }

    /// Cobra's `argsMinusFirstX`: the words without the first plain `x`.
    fn args_minus_first(&self, args: &[String], x: &str) -> Vec<String> {
        let mut pos = 0;
        while pos < args.len() {
            let word = &args[pos];
            if word == "--" {
                break;
            }
            if self.consumes_next(word) {
                pos += 2;
                continue;
            }
            if !word.starts_with('-') && word == x {
                let mut out = args[..pos].to_vec();
                out.extend_from_slice(&args[pos + 1..]);
                return out;
            }
            pos += 1;
        }
        args.to_vec()
    }
}

/// What Find settled on.
#[derive(Debug, PartialEq, Eq)]
pub enum Found {
    /// The command path below the root, and the words left over.
    Command(Vec<&'static str>, Vec<String>),
    /// Words were left at the root, which Cobra's legacy argument check
    /// rejects as an unknown command.
    Unknown,
}

pub fn find(args: &[String]) -> Found {
    let mut node = &TREE;
    let mut path = Vec::new();
    let mut rest = args.to_vec();
    loop {
        let words = node.strip_flags(&rest);
        let Some(next) = words.first() else {
            break;
        };
        let Some(child) = node.children.iter().find(|child| child.name == next) else {
            break;
        };
        rest = node.args_minus_first(&rest, next);
        path.push(child.name);
        node = child;
    }
    if path.is_empty() && !TREE.strip_flags(&rest).is_empty() {
        return Found::Unknown;
    }
    Found::Command(path, rest)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn words(args: &[&str]) -> Vec<String> {
        args.iter().map(|a| (*a).to_string()).collect()
    }

    fn path_of(args: &[&str]) -> Option<Vec<&'static str>> {
        match find(&words(args)) {
            Found::Command(path, _) => Some(path),
            Found::Unknown => None,
        }
    }

    #[test]
    fn find_stops_at_the_deepest_command_and_ignores_trailing_words() {
        assert_eq!(path_of(&["setup", "detect"]), Some(vec!["setup", "detect"]));
        assert_eq!(path_of(&["setup", "nope"]), Some(vec!["setup"]));
        assert_eq!(path_of(&["login", "extra", "words"]), Some(vec!["login"]));
        assert_eq!(path_of(&["help", "setup"]), Some(vec!["help"]));
        assert_eq!(path_of(&[]), Some(vec![]));
    }

    #[test]
    fn a_word_left_at_the_root_is_unknown() {
        assert_eq!(path_of(&["nope"]), None);
        assert_eq!(path_of(&["nope", "setup"]), None);
    }

    #[test]
    fn a_flag_find_does_not_know_swallows_the_next_word() {
        assert_eq!(path_of(&["-h", "quickstart"]), Some(vec![]));
        assert_eq!(path_of(&["--access-token", "setup"]), Some(vec![]));
        assert_eq!(path_of(&["--json", "quickstart"]), Some(vec!["quickstart"]));
        assert_eq!(path_of(&["-o", "json", "setup"]), Some(vec!["setup"]));
        assert_eq!(path_of(&["--output=json", "setup"]), Some(vec!["setup"]));
        assert_eq!(
            path_of(&["setup", "install", "--dry-run", "x"]),
            Some(vec!["setup", "install"])
        );
    }

    #[test]
    fn a_value_flag_at_the_end_stops_the_scan() {
        assert_eq!(
            TREE.strip_flags(&words(&["config", "--output"])),
            words(&["config"])
        );
        assert_eq!(TREE.strip_flags(&words(&["--output", "x"])), words(&[]));
    }

    #[test]
    fn a_double_dash_drops_the_words_after_it() {
        assert_eq!(TREE.strip_flags(&words(&["--", "config"])), words(&[]));
        assert_eq!(path_of(&["--", "nope"]), Some(vec![]));
    }
}
