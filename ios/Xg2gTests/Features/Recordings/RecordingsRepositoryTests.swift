// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct RecordingsRepositoryTests {

    @Test func recordingsDecodesEnvelopeAndSortsByDate() async throws {
        let api = ScriptedAPI()
        api.stub("recordings", json: """
            {
                "requestId": "req_123",
                "currentRoot": "/media/hdd/movie",
                "currentPath": "",
                "roots": [{"id": "root_hdd", "name": "Harddisk"}],
                "directories": [],
                "breadcrumbs": [],
                "recordings": [
                    {"recordingId":"rec_1","title":"Older Movie","beginUnixSeconds":1700000000,"durationSeconds":7200,"status":"completed"},
                    {"recordingId":"rec_2","title":"Newer Show","beginUnixSeconds":1700010000,"durationSeconds":3600,"status":"completed"}
                ]
            }
            """)

        let repo = RecordingsRepository(api: api)
        let recordings = try await repo.recordings()

        #expect(recordings.count == 2)
        // Ordered newest first
        #expect(recordings[0].id == "rec_2")
        #expect(recordings[0].title == "Newer Show")
        #expect(recordings[0].formattedDuration == "1h 0m")
        #expect(recordings[1].id == "rec_1")
        #expect(recordings[1].formattedDuration == "2h 0m")
    }

    @Test func recordingsHandlesEmptyEnvelope() async throws {
        let api = ScriptedAPI()
        api.stub("recordings", json: """
            {
                "requestId": "req_empty",
                "currentRoot": "/media/hdd/movie",
                "currentPath": "",
                "roots": [],
                "directories": [],
                "breadcrumbs": [],
                "recordings": []
            }
            """)

        let repo = RecordingsRepository(api: api)
        let recordings = try await repo.recordings()
        #expect(recordings.isEmpty)
    }

    @Test func recordingsDropsInvalidItemsWithoutTitleOrId() async throws {
        let api = ScriptedAPI()
        api.stub("recordings", json: """
            {
                "requestId": "req_filtered",
                "recordings": [
                    {"recordingId": "", "title": "No ID", "status": "completed"},
                    {"recordingId": "rec_ok", "title": "Valid Movie", "beginUnixSeconds": 1700000000, "durationSeconds": 3600, "status": "completed"},
                    {"recordingId": "rec_no_title", "title": "   ", "status": "completed"}
                ]
            }
            """)

        let repo = RecordingsRepository(api: api)
        let recordings = try await repo.recordings()
        #expect(recordings.count == 1)
        #expect(recordings[0].id == "rec_ok")
        #expect(recordings[0].title == "Valid Movie")
    }

    @Test func recordingsFailsOnInvalidSchema() async throws {
        let api = ScriptedAPI()
        // Flat array violates canonical RecordingResponse schema
        api.stub("recordings", json: """
            [{"recordingId":"rec_1","title":"Flat Array","status":"completed"}]
            """)

        let repo = RecordingsRepository(api: api)
        do {
            _ = try await repo.recordings()
            Issue.record("Expected decoding failure for flat array")
        } catch {
            // Success: failed cleanly as expected
        }
    }
}
