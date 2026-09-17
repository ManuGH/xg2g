// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension DeviceGrant {
    func rotated(to refreshToken: String) -> DeviceGrant {
        DeviceGrant(id: id, secret: refreshToken, expiresAt: expiresAt)
    }
}
