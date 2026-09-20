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
            HomeHubView(model: model)
                .tabItem { tabLabel(.home) }
                .tag(Tab.home)

            ChannelListView(model: model)
                .tabItem { tabLabel(.liveTV) }
                .tag(Tab.liveTV)

            GuideView(model: model)
                .tabItem { tabLabel(.guide) }
                .tag(Tab.guide)

            RecordingsView(model: model)
                .tabItem { tabLabel(.recordings) }
                .tag(Tab.recordings)

#if os(tvOS)
            SearchView(model: model)
                .tabItem { tabLabel(.search) }
                .tag(Tab.search)
#endif

            SettingsView(model: model)
                .tabItem { tabLabel(.settings) }
                .tag(Tab.settings)
        }
    }
}
