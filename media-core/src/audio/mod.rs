// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What an audio elementary stream says about its own channel layout.
//!
//! This is the Rust side of a differential migration. The Go implementation in
//! `backend/internal/stream/ingest/esaudio` is the reference for as long as this
//! step lasts, and the two are held together by a corpus checked in at
//! `testdata/audio-corpus/corpus.txt`: a list of feed sequences and the
//! observation expected after each feed. Both implementations are checked
//! against that file, so agreement is a property of the corpus rather than of
//! anybody's memory of what the other one does.
//!
//! Nothing here knows about MPEG-TS, PES, or the socket. The input is elementary
//! stream payload for one track, cut exactly where the caller cut it.

pub mod ac3;
pub mod observer;

#[cfg(test)]
mod corpus_test;
