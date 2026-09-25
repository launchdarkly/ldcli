//! Parity harness for the Go and Rust `ldcli` binaries.
//!
//! Capture records transcripts from the Go binary only. Check compares a fresh
//! Go run to that transcript, and compares the Rust binary when a case opts in.

pub mod case;
pub mod corpus;
pub mod coverage;
pub mod diffutil;
pub mod fixture;
pub mod normalize;
pub mod sandbox;
pub mod secrets;

pub use case::{load_case, save_case, Case, Expectation};
pub use coverage::{check_coverage, load_exemptions, Coverage, CoverageFailure, Exemption};
pub use normalize::{canonicalize_json, normalize_stream, RedactionRules, Redactor};
pub use sandbox::{run_command, RunRequest, RunResult};
pub use secrets::{scan_authorization, scan_tree};
