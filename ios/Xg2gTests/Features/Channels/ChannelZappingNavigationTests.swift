// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@MainActor
struct ChannelZappingNavigationTests {

    @Test func zappingWrapsAroundProperly() {
        let model = AppModel()
        let c1 = Channel(id: "1", name: "ORF1", number: "1", serviceRef: "ref1", logoURL: nil)

        // In empty state
        #expect(model.channelAfter(c1) == nil)
        #expect(model.channelBefore(c1) == nil)
    }
}
