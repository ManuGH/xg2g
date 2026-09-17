// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct QualityPreferenceTests {

    @Test func allCasesHaveValidDisplayNames() {
        for pref in AppModel.StreamingQualityPreference.allCases {
            #expect(!pref.displayName.isEmpty)
            #expect(!pref.rawValue.isEmpty)
        }
    }
}
