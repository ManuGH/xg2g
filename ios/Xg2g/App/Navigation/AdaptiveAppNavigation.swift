// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Adaptive App Navigation (iPhone TabView vs iPadOS NavigationSplitView)

struct AdaptiveAppNavigation: View {

    @Bindable var model: AppModel

    var body: some View {
        if UIDevice.current.userInterfaceIdiom == .pad {
            // MARK: - iPadOS NavigationSplitView with Glass Sidebar
            NavigationSplitView {
                IPadSidebar(model: model)
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
        } else {
            // MARK: - iOS Compact TabView
            MainTabView(model: model)
        }
    }
}
