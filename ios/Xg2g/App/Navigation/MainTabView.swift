// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Main Tab View (Compact iPhone Mode)

struct MainTabView: View {

    @Bindable var model: AppModel

    /// tvOS tab bars are text: with icons, six German labels no longer fit
    /// the bar and the last one is clipped. iPhone keeps the icon-and-text item.
    @ViewBuilder
    private func tabLabel(_ tab: Tab) -> some View {
#if os(tvOS)
        Text(tab.rawValue)
#else
        Label(tab.rawValue, systemImage: tab.systemImage)
#endif
    }

    var body: some View {
        TabView(selection: $model.selectedTab) {
#if os(tvOS)
            // The television has its own dedicated screens on the
            // shared model: uniform card sizes, readable typography, Siri Remote focus.
            TVHomeView(model: model)
                .tabItem { tabLabel(.home) }
                .tag(Tab.home)

            TVOSGuideHubView(model: model)
                .tabItem { tabLabel(.liveTV) }
                .tag(Tab.liveTV)

            TVRecordingsView(model: model)
                .tabItem { tabLabel(.recordings) }
                .tag(Tab.recordings)

            SearchView(model: model)
                .tabItem { tabLabel(.search) }
                .tag(Tab.search)

            SettingsView(model: model)
                .tabItem { tabLabel(.settings) }
                .tag(Tab.settings)
#else
            HomeHubView(model: model)
                .tabItem { tabLabel(.home) }
                .tag(Tab.home)

            TVGuideHubView(model: model)
                .tabItem { tabLabel(.liveTV) }
                .tag(Tab.liveTV)

            RecordingsView(model: model)
                .tabItem { tabLabel(.recordings) }
                .tag(Tab.recordings)

            SettingsView(model: model)
                .tabItem { tabLabel(.settings) }
                .tag(Tab.settings)
#endif
        }
#if os(tvOS)
        .onExitCommand {
            if model.selectedTab != .home {
                model.selectedTab = .home
            }
        }
#endif
    }
}
