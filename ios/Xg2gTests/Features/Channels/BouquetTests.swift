// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct BouquetTests {

    @Test func bouquetsDecodesNameAndCount() async throws {
        let api = ScriptedAPI()
        api.stub("services/bouquets", json: """
            [{"name":"Favoriten","services":15},
             {"name":"HD Sender","services":42},
             {"name":"  ","services":0}]
            """)

        let bouquets = try await ChannelRepository(api: api).bouquets()

        #expect(bouquets.count == 2)
        #expect(bouquets[0].name == "Favoriten")
        #expect(bouquets[0].servicesCount == 15)
        #expect(bouquets[1].name == "HD Sender")
        #expect(bouquets[1].servicesCount == 42)
    }
}
