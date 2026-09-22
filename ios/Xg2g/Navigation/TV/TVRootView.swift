// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Dedicated tvOS Living Room navigation shell.
///
/// Designed around the Apple TV Focus Engine and Siri Remote, featuring a top-shelf
/// navigation bar without floating mini-player bars that could intercept focus.
struct TVRootView: View {
    @Bindable var model: AppModel

    init(model: AppModel) {
        self.model = model
    }

    var body: some View {
        #if os(tvOS)
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

            TimersView(model: model)
                .tabItem {
                    Label(Tab.timers.rawValue, systemImage: Tab.timers.systemImage)
                }
                .tag(Tab.timers)

            SettingsView(model: model)
                .tabItem {
                    Label(Tab.settings.rawValue, systemImage: Tab.settings.systemImage)
                }
                .tag(Tab.settings)
        }
        #else
        EmptyView()
        #endif
    }
}
