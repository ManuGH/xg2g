// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Adapter bridging the Greenfield `GuideRepository` protocol to the existing `AppModel`.
@MainActor
final class LegacyBridgeGuideRepository: GuideRepository {
    private weak var appModel: AppModel?

    init(appModel: AppModel) {
        self.appModel = appModel
    }

    func channels() async throws -> [Channel] {
        guard let appModel else { return [] }
        if appModel.channels.isEmpty {
            await appModel.loadChannels()
        }
        return appModel.channels
    }

    func nowNext() async throws -> [String: NowNext] {
        guard let appModel else { return [:] }
        return appModel.schedule
    }

    func schedule(for channels: [Channel], in window: GuideWindow) async throws -> [GuideChannelSchedule] {
        guard let appModel else { return [] }
        let fullEpg = appModel.fullEpg
        return channels.map { channel in
            let shows = fullEpg[channel.serviceRef] ?? []
            let filtered = shows.filter { show in
                show.end > window.start && show.start < window.end
            }
            return GuideChannelSchedule(channel: channel, shows: filtered)
        }
    }
}
