// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// The one place that decides which screen the app is on.
///
/// Routing off `AppState` rather than off a pile of optionals: every screen
/// here corresponds to exactly one state, so there is no combination that lands
/// on a view nobody designed.
struct RootView: View {

    @Environment(\.scenePhase) private var scenePhase
    @State private var model = AppModel()

    var body: some View {
        RootContentView(model: model, playbackManager: model.playbackManager)
            .preferredColorScheme(.dark)
            .tint(Theme.Colors.accentAction)
            .task { await model.start() }
            .onChange(of: scenePhase) { _, newPhase in
                if newPhase == .active {
                    Task { await model.handleAppBecameActive() }
                }
            }
            .task(id: scenePhase) {
                guard scenePhase == .active else { return }
                // Periodic background heartbeat / EPG refresh while app is active in foreground
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(60))
                    if scenePhase == .active && model.state == .ready {
                        await model.refreshSchedule()
                    }
                }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIApplication.willEnterForegroundNotification)) { _ in
                Task { await model.handleAppBecameActive() }
            }
            .onReceive(NotificationCenter.default.publisher(for: .selectAppTab)) { notification in
                if let tab = notification.userInfo?["tab"] as? Tab {
                    model.selectedTab = tab
                }
            }
            .onContinueUserActivity(HandoffCoordinator.activityType) { userActivity in
                guard let serviceRef = HandoffCoordinator.extractServiceRef(from: userActivity) else { return }
                Task { @MainActor in
                    if model.channels.isEmpty {
                        await model.loadChannels()
                    }
                    if let target = model.channels.first(where: { $0.id == serviceRef || $0.serviceRef == serviceRef }) {
                        model.playingChannel = target
                    }
                }
            }
    }
}
