// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct NowNextProgressTests {

    private let start = Date(timeIntervalSince1970: 1_800_000_000)

    private func entry(minutes: Double) -> NowNext.Entry {
        NowNext.Entry(title: "T", description: nil, start: start, end: start.addingTimeInterval(minutes * 60))
    }

    @Test func progressIsTheFractionElapsed() {
        let programme = entry(minutes: 60)
        #expect(programme.progress(at: start.addingTimeInterval(1800)) == 0.5)
    }

    /// Before the start and after the end there is no progress — not zero and
    /// not one, because a caller must be able to tell "not on" from "just
    /// began".
    @Test func outsideTheProgrammeThereIsNoProgress() {
        let programme = entry(minutes: 60)
        #expect(programme.progress(at: start.addingTimeInterval(-1)) == nil)
        #expect(programme.progress(at: start.addingTimeInterval(3601)) == nil)
    }

    @Test func aZeroLengthProgrammeHasNoProgressRatherThanADivisionByZero() {
        #expect(entry(minutes: 0).progress(at: start) == nil)
    }
}
