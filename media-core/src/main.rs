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
//! It opens no listener. The caller creates a Unix socket, starts this process
//! with `--socket <path>`, and this connects back to it - so there is never a
//! moment where something else on the machine could arrive first.

mod ipc;

use std::io::BufWriter;
use std::os::unix::net::UnixStream;
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
    let mut args = std::env::args().skip(1);
    let mut socket: Option<String> = None;
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--socket" => socket = args.next(),
            "--version" | "-V" => {
                println!("{}", identity());
                return ExitCode::SUCCESS;
            }
            other => {
                eprintln!("{}: unknown argument {other}", identity());
                return ExitCode::from(2);
            }
        }
    }

    let Some(path) = socket else {
        // Still answers the question it answered before there was a socket.
        println!("{}", identity());
        return ExitCode::SUCCESS;
    };

    match serve(&path) {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("{}: {e}", identity());
            ExitCode::from(1)
        }
    }
}

/// One connection, one request at a time, until the caller says stop or goes away.
///
/// A closed socket ends this process without complaint: the caller hanging up is
/// how it is told to finish, and treating that as a failure would make every
/// normal shutdown look like a crash.
fn serve(path: &str) -> std::io::Result<()> {
    let stream = UnixStream::connect(path)?;
    let mut reader = stream.try_clone()?;
    let mut writer = BufWriter::new(stream);

    // One session for the connection, because the audio shadow's observers are
    // stateful: what a stream said in an earlier request is part of the answer to
    // the next one. It lives and dies with this socket.
    let mut session = ipc::Session::new();

    while let Some(frame) = ipc::read_frame(&mut reader)? {
        match session.handle(&frame) {
            ipc::Outcome::Answer(body) => {
                ipc::write_frame(&mut writer, frame.kind, frame.request_id, &body)?;
            }
            ipc::Outcome::Finished(body) => {
                ipc::write_frame(&mut writer, frame.kind, frame.request_id, &body)?;
                return Ok(());
            }
        }
    }
    Ok(())
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
