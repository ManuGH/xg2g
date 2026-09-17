// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Main Tab View (Compact iPhone Mode)

struct MainTabView: View {

    @Bindable var model: AppModel

    var body: some View {
        TabView(selection: $model.selectedTab) {
            HomeHubView(model: model)
                .tabItem {
                    Label(Tab.home.rawValue, systemImage: Tab.home.systemImage)
                }
                .tag(Tab.home)

            ChannelListView(model: model)
                .tabItem {
                    Label(Tab.liveTV.rawValue, systemImage: Tab.liveTV.systemImage)
                }
                .tag(Tab.liveTV)

            GuideView(model: model)
                .tabItem {
                    Label(Tab.guide.rawValue, systemImage: Tab.guide.systemImage)
                }
                .tag(Tab.guide)

            RecordingsView(model: model)
                .tabItem {
                    Label(Tab.recordings.rawValue, systemImage: Tab.recordings.systemImage)
                }
                .tag(Tab.recordings)

            SettingsView(model: model)
                .tabItem {
                    Label(Tab.settings.rawValue, systemImage: Tab.settings.systemImage)
                }
                .tag(Tab.settings)
        }
    }
}
