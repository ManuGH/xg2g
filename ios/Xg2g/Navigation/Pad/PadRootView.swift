// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Dedicated regular touch and pointer navigation shell for iPadOS and Mac Catalyst.
///
/// Presents a responsive `NavigationSplitView` with a persistent sidebar for primary
/// categories and bouquet management, and a rich content viewport.
struct PadRootView: View {
    @Bindable var model: AppModel
    @ObservedObject var playbackManager: PlaybackManager

    init(model: AppModel, playbackManager: PlaybackManager) {
        self.model = model
        self.playbackManager = playbackManager
    }

    var body: some View {
        #if !os(tvOS)
        ZStack(alignment: .bottom) {
            NavigationSplitView {
                iPadSidebar(model: model)
            } detail: {
                switch model.selectedTab {
                case .home:
                    HomeHubView(model: model)
                case .liveTV:
                    ChannelListView(model: model)
                case .guide:
                    GuideView(model: model)
                case .recordings:
                    RecordingsView(model: model)
                case .timers:
                    TimersView(model: model)
                case .settings:
                    SettingsView(model: model)
                }
            }
            .navigationSplitViewStyle(.balanced)

            // Docked MiniPlayer overlay for iPad
            if playbackManager.presentationMode == .miniplayer {
                MiniPlayerBar(playbackManager: playbackManager, model: model)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .padding(.horizontal, 24)
                    .padding(.bottom, 16)
                    .zIndex(10)
            }
        }
        #else
        EmptyView()
        #endif
    }
}
