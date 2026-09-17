// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct TimersRepositoryTests {

    @Test func timersDecodesEnvelopeAndSortsByDate() async throws {
        let api = ScriptedAPI()
        api.stub("timers", json: """
            {
                "items": [
                    {"timerId":"t2","name":"Late Show","serviceRef":"1:0:1:2::","serviceName":"ZDF","begin":1700020000,"end":1700023600,"state":"scheduled"},
                    {"timerId":"t1","name":"Early News","serviceRef":"1:0:1:1::","serviceName":"Das Erste","begin":1700010000,"end":1700011800,"state":"recording"}
                ]
            }
            """)

        let repo = TimersRepository(api: api)
        let timers = try await repo.timers()

        #expect(timers.count == 2)
        // Ordered chronological
        #expect(timers[0].id == "t1")
        #expect(timers[0].name == "Early News")
        #expect(timers[0].isRunning == true)
        #expect(timers[1].id == "t2")
        #expect(timers[1].isRunning == false)
    }

    @Test func timersHandlesEmptyList() async throws {
        let api = ScriptedAPI()
        api.stub("timers", json: """
            {
                "items": []
            }
            """)

        let repo = TimersRepository(api: api)
        let timers = try await repo.timers()
        #expect(timers.isEmpty)
    }

    @Test func timersDropsInvalidItemsWithoutNameOrServiceRef() async throws {
        let api = ScriptedAPI()
        api.stub("timers", json: """
            {
                "items": [
                    {"timerId":"t_invalid_1","name":"","serviceRef":"1:0:1:1::","begin":1700010000,"end":1700011800,"state":"scheduled"},
                    {"timerId":"t_valid","name":"Valid Timer","serviceRef":"1:0:1:1::","begin":1700010000,"end":1700011800,"state":"scheduled"},
                    {"timerId":"t_invalid_2","name":"Missing Ref","serviceRef":"  ","begin":1700010000,"end":1700011800,"state":"scheduled"}
                ]
            }
            """)

        let repo = TimersRepository(api: api)
        let timers = try await repo.timers()
        #expect(timers.count == 1)
        #expect(timers[0].id == "t_valid")
        #expect(timers[0].name == "Valid Timer")
    }

    @Test func timersFailsOnMissingRequiredItems() async throws {
        let api = ScriptedAPI()
        // Missing required 'items' field
        api.stub("timers", json: """
            {}
            """)

        let repo = TimersRepository(api: api)
        do {
            _ = try await repo.timers()
            Issue.record("Expected decoding failure for missing items field")
        } catch {
            // Success: failed cleanly as expected
        }
    }
}
