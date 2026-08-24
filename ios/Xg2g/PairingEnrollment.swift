// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension DeviceGrant {
    /// The refresh token is the durable half of the pair, so it is what the
    /// grant record holds. It rotates on every refresh; the old value must never
    /// be presented again, because the server treats that as a replay and
    /// revokes the whole family.
    init(exchange: Xg2gContract.ExchangePairingResponse) {
        self.init(id: exchange.deviceId, secret: exchange.refreshToken, expiresAt: nil)
    }

    func rotated(to refreshToken: String) -> DeviceGrant {
        DeviceGrant(id: id, secret: refreshToken, expiresAt: expiresAt)
    }
}
