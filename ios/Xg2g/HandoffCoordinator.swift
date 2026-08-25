// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import UIKit

@MainActor
final class HandoffCoordinator {

    static let shared = HandoffCoordinator()
    nonisolated static let activityType = "io.github.manugh.xg2g.playback"

    private var currentUserActivity: NSUserActivity?

    private init() {}

    func updatePlaybackActivity(
        channel: Channel,
        nowNext: NowNext?,
        serverAddress: String
    ) {
        let activity = NSUserActivity(activityType: Self.activityType)
        activity.title = "\(channel.name): \(nowNext?.now?.title ?? "Live TV")"
        activity.isEligibleForHandoff = true
        activity.isEligibleForSearch = true
        activity.isEligibleForPublicIndexing = false

        var userInfo: [String: Any] = [
            "serviceRef": channel.id,
            "channelName": channel.name,
            "serverAddress": serverAddress
        ]
        if let number = channel.number {
            userInfo["channelNumber"] = number
        }
        if let showTitle = nowNext?.now?.title {
            userInfo["showTitle"] = showTitle
        }

        activity.userInfo = userInfo
        activity.becomeCurrent()
        self.currentUserActivity = activity
    }

    func clearPlaybackActivity() {
        currentUserActivity?.resignCurrent()
        currentUserActivity?.invalidate()
        currentUserActivity = nil
    }

    nonisolated static func extractServiceRef(from userActivity: NSUserActivity) -> String? {
        guard userActivity.activityType == activityType,
              let userInfo = userActivity.userInfo,
              let serviceRef = userInfo["serviceRef"] as? String else {
            return nil
        }
        return serviceRef
    }
}
