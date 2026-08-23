// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import Combine

public enum PlaybackPresentationMode: String, Sendable, Equatable {
    case hidden
    case miniplayer
    case fullscreen
}

public enum PiPState: String, Sendable, Equatable {
    case inactive
    case starting
    case active
}

public enum BackgroundPlaybackState: String, Sendable, Equatable {
    case foreground
    case backgroundAudio
}

/// Central controller for live TV playback sessions and presentation modes.
///
/// Decoupled from `AppModel` and individual screen lifecycles:
/// neither FullscreenPlayer nor MiniPlayer owns the stream; both are
/// presentation modes onto the persistent `ZapCoordinator` session.
@MainActor
final class PlaybackManager: ObservableObject {

    @Published private(set) var presentationMode: PlaybackPresentationMode = .hidden
    @Published private(set) var pipState: PiPState = .inactive
    @Published private(set) var backgroundState: BackgroundPlaybackState = .foreground
    @Published private(set) var currentChannel: Channel?
    @Published private(set) var isStreaming: Bool = false

    let coordinator: ZapCoordinator
    private let streamURLProvider: @MainActor (String) -> URL?
    private var cancellables = Set<AnyCancellable>()

    init(preparations: ZapPreparationClient? = nil,
         preparationsProvider: (@MainActor () -> ZapPreparationClient?)? = nil,
         streamURL: @escaping @MainActor (String) -> URL?) {
        self.streamURLProvider = streamURL
        self.coordinator = ZapCoordinator(
            preparations: preparations,
            preparationsProvider: preparationsProvider,
            streamURL: streamURL
        )

        self.coordinator.objectWillChange
            .sink { [weak self] _ in
                self?.objectWillChange.send()
            }
            .store(in: &cancellables)
    }

    var isPlaying: Bool {
        presentationMode != .hidden && coordinator.playing != nil
    }

    var displayedPlan: SessionRuntimePlan? {
        coordinator.playing?.runtimePlan
    }

    var displayedServiceRef: String? {
        coordinator.displayedServiceRef ?? coordinator.presentedServiceRef ?? currentChannel?.serviceRef
    }

    func play(channel: Channel, mode: PlaybackPresentationMode = .fullscreen) {
        self.currentChannel = channel
        self.presentationMode = mode
        self.isStreaming = true

        let serviceRef = channel.serviceRef
        if coordinator.canPrepare {
            Task { @MainActor in
                await coordinator.zap(to: serviceRef)
            }
        } else if let url = streamURLProvider(serviceRef) {
            Task { @MainActor in
                await coordinator.play(unprepared: url, requestedAt: CACurrentMediaTime())
            }
        }
    }

    func zap(to channel: Channel) {
        self.currentChannel = channel
        if presentationMode == .hidden {
            self.presentationMode = .fullscreen
        }
        let serviceRef = channel.serviceRef
        Task { @MainActor in
            await coordinator.zap(to: serviceRef)
        }
    }

    func minimize() {
        guard presentationMode == .fullscreen else { return }
        self.presentationMode = .miniplayer
    }

    func expand() {
        guard presentationMode == .miniplayer else { return }
        self.presentationMode = .fullscreen
    }

    func stop() {
        self.presentationMode = .hidden
        self.isStreaming = false
        self.currentChannel = nil
        Task { @MainActor in
            await coordinator.stop()
        }
    }
}
