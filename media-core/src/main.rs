// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The xg2g media facts core.
//!
//! This binary will eventually read transport stream bytes handed to it by the Go
//! daemon and answer with facts about them. It does none of that yet, and that is
//! deliberate: this crate exists first to prove it can be built, pinned,
//! cross-compiled and shipped in the runtime image without disturbing the Go
//! binary beside it. Parsing, the socket and the process lifecycle arrive in
//! their own changesets, one problem at a time.
//!
//! It opens no socket, starts no listener and reads no input.

use std::process::ExitCode;

/// The crate version, from the manifest rather than a second copy of the number.
#[must_use]
pub fn version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

/// What this binary reports about itself.
#[must_use]
pub fn identity() -> String {
    format!("xg2g-media-core {}", version())
}

fn main() -> ExitCode {
    // Started, said what it is, finished. The next changeset gives it something
    // to do between those two points.
    println!("{}", identity());
    ExitCode::SUCCESS
}

#[cfg(test)]
mod tests {
    use super::{identity, version};

    #[test]
    fn version_comes_from_the_manifest() {
        assert_eq!(version(), env!("CARGO_PKG_VERSION"));
        assert!(!version().is_empty());
    }

    #[test]
    fn identity_names_the_binary_and_its_version() {
        let id = identity();
        assert!(id.starts_with("xg2g-media-core "), "identity = {id}");
        assert!(id.ends_with(version()), "identity = {id}");
    }
}
