// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The parts of the media core that are worth reading on their own.
//!
//! The binary beside this library owns the socket and the process lifecycle. What
//! lives here is the reading of bytes: given elementary stream payload, what does
//! it say about itself. Keeping that in a library is what lets it be tested
//! against a corpus without a process, a socket or a Go daemon in the way.

pub mod audio;
