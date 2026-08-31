// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Many audio streams at once, each followed under the epoch it belongs to.
//!
//! The Go side asks about batches: one run of feeds for one elementary stream
//! under one epoch. An observer is stateful, so the same PID before and after a
//! PMT change is two observers - the number is reused, the stream is not, and
//! folding them together would be the exact confusion the differential exists to
//! find.
//!
//! Nothing here knows about the socket or the frame. It is handed feeds in the
//! order they happened and answers what it made of them.

use std::collections::HashMap;

use super::observer::{Observation, Observer};

/// One elementary stream under one epoch. The key an observer belongs to.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct StreamEpoch {
    /// The transport stream PID the elementary stream arrived on.
    pub pid: u16,
    /// The epoch the caller had when these bytes were captured.
    pub epoch: u64,
}

/// Every stream the caller is currently having observed.
///
/// State is bounded by the caller's own epoch rather than by a cache policy. The
/// epoch is monotonic on the Go side and every batch carries it, so an epoch
/// lower than the highest one seen is one the caller has left for good: no future
/// batch can be about it, and the observers under it have had their last
/// necessary evaluation. They are dropped once the request that moved the epoch
/// on has been answered in full - not while it is being answered, because one
/// request may legitimately carry the end of one epoch and the start of the next.
#[derive(Debug, Default)]
pub struct Registry {
    observers: HashMap<StreamEpoch, Observer>,
    highest_epoch: u64,
}

impl Registry {
    /// A registry following nothing yet.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Feeds one batch to the observer that owns it and reports what it now says.
    ///
    /// The feeds are consumed one at a time, in the order given. They are not
    /// joined: where the caller cut the bytes is part of the input, because an
    /// observer carries a partial header across a call and skips frame payload
    /// that ran past the end of one.
    pub fn observe<'a, I>(&mut self, key: StreamEpoch, feeds: I) -> Observation
    where
        I: IntoIterator<Item = &'a [u8]>,
    {
        if key.epoch > self.highest_epoch {
            self.highest_epoch = key.epoch;
        }
        let observer = self.observers.entry(key).or_default();
        for feed in feeds {
            observer.feed(feed);
        }
        observer.current()
    }

    /// Drops every stream belonging to an epoch the caller has left.
    ///
    /// Called after a whole request has been answered, which is the first moment
    /// at which "the epoch moved on" is known to mean "and nothing else in this
    /// request is about the old one".
    pub fn retire_past_epochs(&mut self) {
        let keep = self.highest_epoch;
        self.observers.retain(|k, _| k.epoch >= keep);
    }

    /// How many streams are being followed. For the tests that pin the bound.
    #[must_use]
    pub fn len(&self) -> usize {
        self.observers.len()
    }

    /// Whether nothing is being followed.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.observers.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::{Registry, StreamEpoch};

    /// An AC-3 syncframe of 128 bytes with the given acmod, no LFE.
    fn ac3(acmod: u8) -> Vec<u8> {
        let mut f = vec![0u8; 128];
        f[0] = 0x0B;
        f[1] = 0x77;
        f[4] = 0x00;
        f[5] = 8 << 3;
        f[6] = acmod << 5;
        f
    }

    fn run(acmod: u8, frames: usize) -> Vec<u8> {
        let mut out = Vec::new();
        for _ in 0..frames {
            out.extend_from_slice(&ac3(acmod));
        }
        out
    }

    #[test]
    fn the_same_pid_under_two_epochs_is_two_streams() {
        let mut r = Registry::new();
        let old = StreamEpoch { pid: 300, epoch: 1 };
        let new = StreamEpoch { pid: 300, epoch: 2 };

        let five_one = run(7, 3);
        let stereo = run(2, 3);

        let a = r.observe(old, [five_one.as_slice()]);
        let b = r.observe(new, [stereo.as_slice()]);

        assert_eq!(a.acmod, 7, "the first epoch saw 3/2");
        assert_eq!(b.acmod, 2, "the second epoch is a stream of its own");
        assert_eq!(
            b.frames, 3,
            "the new epoch did not inherit the old one's count"
        );
    }

    #[test]
    fn feeds_are_consumed_in_order_and_not_joined() {
        let key = StreamEpoch { pid: 1, epoch: 1 };
        let whole = run(2, 3);

        let mut split = Registry::new();
        let (head, tail) = whole.split_at(70);
        let a = split.observe(key, [head, tail]);

        let mut once = Registry::new();
        let b = once.observe(key, [whole.as_slice()]);

        assert_eq!(
            a, b,
            "a syncframe split across two feeds is still one frame"
        );
    }

    #[test]
    fn an_epoch_the_caller_has_left_stops_costing_memory() {
        let mut r = Registry::new();
        let audio = run(2, 3);
        for pid in 0..64u16 {
            r.observe(StreamEpoch { pid, epoch: 1 }, [audio.as_slice()]);
        }
        assert_eq!(r.len(), 64);

        // Still inside the request that carries both epochs: the old one is not
        // gone yet, because the batch after it may still be about it.
        r.observe(StreamEpoch { pid: 0, epoch: 2 }, [audio.as_slice()]);
        assert_eq!(r.len(), 65, "an epoch is not dropped mid-request");

        r.retire_past_epochs();
        assert_eq!(r.len(), 1, "only the epoch the caller is in survives");
    }

    #[test]
    fn nothing_is_dropped_while_the_epoch_stands_still() {
        let mut r = Registry::new();
        let audio = run(2, 3);
        r.observe(StreamEpoch { pid: 1, epoch: 9 }, [audio.as_slice()]);
        r.observe(StreamEpoch { pid: 2, epoch: 9 }, [audio.as_slice()]);
        r.retire_past_epochs();
        assert_eq!(r.len(), 2);
    }
}
