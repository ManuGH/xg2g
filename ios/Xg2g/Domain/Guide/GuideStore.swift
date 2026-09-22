// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// UI-facing store managing channel and electronic programme guide (EPG) state.
///
/// Strictly bound to `@MainActor` for thread-safe SwiftUI updates, delegating all
/// background fetching, caching, and network coordination to an injected `GuideRepository`.
@MainActor
@Observable
final class GuideStore {
    private(set) var channels: [Channel] = []
    private(set) var nowNext: [String: NowNext] = [:]
    private(set) var schedules: [GuideChannelSchedule] = []
    var selectedChannel: Channel?
    private(set) var isLoading: Bool = false
    private(set) var errorMessage: String?

    private let repository: any GuideRepository

    init(repository: any GuideRepository) {
        self.repository = repository
    }

    /// Loads the channel lineup from the repository.
    func loadChannels() async {
        isLoading = true
        defer { isLoading = false }
        do {
            channels = try await repository.channels()
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Updates current and upcoming show metadata without blocking the primary UI.
    func loadNowNext() async {
        do {
            nowNext = try await repository.nowNext()
        } catch {
            // Silently retain previous nowNext on temporary network blips
        }
    }

    /// Loads the schedule for all current channels inside the specified window.
    func loadSchedule(in window: GuideWindow) async {
        guard !channels.isEmpty else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            schedules = try await repository.schedule(for: channels, in: window)
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Selects a channel for detailed inspection or preview.
    func selectChannel(_ channel: Channel?) {
        selectedChannel = channel
    }
}
