// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@MainActor
struct FavoriteChannelsTests {

    @Test func togglingFavoritesUpdatesState() {
        let model = AppModel()
        let c1 = Channel(id: "fav_test_1", name: "ORF1 HD", number: "1", serviceRef: "ref_1", logoURL: nil)

        #expect(model.isFavorite(c1) == false)

        model.toggleFavorite(c1)
        #expect(model.isFavorite(c1) == true)

        model.toggleFavorite(c1)
        #expect(model.isFavorite(c1) == false)
    }
}
