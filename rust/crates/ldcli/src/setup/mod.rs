//! `ldcli setup`'s non-interactive subcommands: detect, install, and init.
//!
//! The guided wizard that `ldcli setup` itself runs is not ported.

use crate::flags::{Flag, FlagKind};
use crate::help::help_flag;

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
