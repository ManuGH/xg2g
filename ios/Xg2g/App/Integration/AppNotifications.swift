// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension Notification.Name {
    /// Request switching the top-level app tab (userInfo: ["tab": Tab])
    static let selectAppTab = Notification.Name("xg2g.selectAppTab")

    /// Player command shortcuts dispatched from iPadOS Discoverability HUD / macOS Menu Bar
    static let playerTogglePlayPause = Notification.Name("xg2g.playerTogglePlayPause")
    static let playerZapNext = Notification.Name("xg2g.playerZapNext")
    static let playerZapPrevious = Notification.Name("xg2g.playerZapPrevious")
    static let playerSeekBackward = Notification.Name("xg2g.playerSeekBackward")
    static let playerSeekForward = Notification.Name("xg2g.playerSeekForward")
    static let playerToggleZapDrawer = Notification.Name("xg2g.playerToggleZapDrawer")
    static let playerCycleAspect = Notification.Name("xg2g.playerCycleAspect")
    static let playerToggleHUD = Notification.Name("xg2g.playerToggleHUD")
    static let playerStartPiP = Notification.Name("xg2g.playerStartPiP")
    static let playerClose = Notification.Name("xg2g.playerClose")
}
