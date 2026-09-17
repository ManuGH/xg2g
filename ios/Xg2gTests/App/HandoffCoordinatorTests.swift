// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct HandoffCoordinatorTests {

    @Test func extractsServiceRefFromMatchingUserActivity() {
        let activity = NSUserActivity(activityType: HandoffCoordinator.activityType)
        activity.userInfo = [
            "serviceRef": "1:0:19:132F:3EF:1:C00000:0:0:0",
            "channelName": "ORF1 HD"
        ]

        let extracted = HandoffCoordinator.extractServiceRef(from: activity)
        #expect(extracted == "1:0:19:132F:3EF:1:C00000:0:0:0")
    }

    @Test func ignoresMismatchedActivityType() {
        let activity = NSUserActivity(activityType: "com.other.app.activity")
        activity.userInfo = ["serviceRef": "1:0:19:132F:3EF:1:C00000:0:0:0"]

        let extracted = HandoffCoordinator.extractServiceRef(from: activity)
        #expect(extracted == nil)
    }
}
