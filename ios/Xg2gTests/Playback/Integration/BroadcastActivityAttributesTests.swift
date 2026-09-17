// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

#if canImport(ActivityKit)
struct BroadcastActivityAttributesTests {

    @Test func attributesAndContentStateInitializeProperly() {
        let attrs = BroadcastLiveActivityAttributes(
            channelName: "ORF1 HD",
            serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0"
        )
        #expect(attrs.channelName == "ORF1 HD")
        #expect(attrs.serviceRef == "1:0:19:132F:3EF:1:C00000:0:0:0")

        let state = BroadcastLiveActivityAttributes.ContentState(
            showTitle: "Formel 1",
            subtitle: "GP von Monaco",
            startTimestamp: 1000,
            endTimestamp: 5000,
            isRecording: true,
            isDirectStream: true,
            channelNumber: "1"
        )
        #expect(state.showTitle == "Formel 1")
        #expect(state.subtitle == "GP von Monaco")
        #expect(state.isRecording == true)
        #expect(state.isDirectStream == true)
        #expect(state.channelNumber == "1")
    }
}
#endif
