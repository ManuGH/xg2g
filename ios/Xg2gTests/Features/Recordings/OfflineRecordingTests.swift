// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct OfflineRecordingTests {

    @Test func formattingAndPathCalculations() {
        let offline = OfflineRecording(
            id: "off_1",
            recordingId: "rec_1",
            title: "Tatort",
            channelName: "ORF1 HD",
            durationSeconds: 5400, // 1h 30m
            fileSize: 2_147_483_648, // 2 GB
            downloadDate: Date(timeIntervalSince1970: 1700000000),
            localRelativePath: "OfflineRecordings/rec_1.mp4"
        )

        #expect(offline.formattedDuration == "1h 30m")
        #expect(offline.formattedSize.contains("GB") || offline.formattedSize.contains("2"))
        #expect(offline.localFileURL().path.contains("OfflineRecordings/rec_1.mp4"))
    }
}
