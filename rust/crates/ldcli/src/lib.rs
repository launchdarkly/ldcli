//! The LaunchDarkly CLI.
//!
//! The binary is a thin wrapper over these modules so that argv handling,
//! precedence, help rendering, and the analytics property map can be tested
//! directly. Where this crate and the Go tree disagree, the Go tree is the
//! specification: `make parity` diffs the two binaries.

pub mod analytics;
pub mod cli;
pub mod config;
pub mod config_cmd;
pub mod flags;
pub mod help;
pub mod http;
pub mod output;
pub mod settings;
pub mod whoami;
