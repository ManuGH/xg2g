// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Dedicated compact iPhone navigation shell.
///
/// Presents a bottom `TabView` with primary feature sections and a persistent
/// `MiniPlayerBar` overlay when active playback is minimized.
struct PhoneRootView: View {
    @Bindable var model: AppModel
    @ObservedObject var playbackManager: PlaybackManager

    init(model: AppModel, playbackManager: PlaybackManager) {
        self.model = model
        self.playbackManager = playbackManager
    }

    var body: some View {
        ZStack(alignment: .bottom) {
            #if !os(tvOS)
            MainTabView(model: model)
            #else
            EmptyView()
            #endif

            #if !os(tvOS)
            // Mini player bar when playback is active and docked
            if playbackManager.presentationMode == .miniplayer {
                MiniPlayerBar(playbackManager: playbackManager, model: model)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .padding(.bottom, 56) // Docked above UITabBar
                    .zIndex(10)
            }
            #endif
        }
    }
}
