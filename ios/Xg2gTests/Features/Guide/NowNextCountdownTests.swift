// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct NowNextCountdownTests {

    @Test func remainingMinutesComputesCorrectly() {
        let start = Date(timeIntervalSince1970: 1000)
        let end = Date(timeIntervalSince1970: 2200) // 20 minutes duration

        let entry = NowNext.Entry(title: "Movie", description: nil, start: start, end: end)

        // Mid-way at 1600 (10 minutes remaining)
        let mid = Date(timeIntervalSince1970: 1600)
        #expect(entry.remainingMinutes(at: mid) == 10)

        // Before start -> nil
        let before = Date(timeIntervalSince1970: 500)
        #expect(entry.remainingMinutes(at: before) == nil)

        // After end -> nil
        let after = Date(timeIntervalSince1970: 3000)
        #expect(entry.remainingMinutes(at: after) == nil)
    }
}
