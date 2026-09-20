// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Unified "TV & Guide" Hub for Apple TV.
///
/// Combines the channel grid and EPG timeline into one tvOS screen with a top mode switcher.
struct TVOSGuideHubView: View {

    @Bindable var model: AppModel
    @State private var viewMode: ViewMode = .grid

    enum ViewMode: String, CaseIterable, Identifiable {
        case grid = "Sender-Kacheln"
        case epg = "TV-Programm"

        var id: String { rawValue }
    }

    var body: some View {
        VStack(spacing: 16) {
            Picker("", selection: $viewMode) {
                ForEach(ViewMode.allCases) { mode in
                    Text(mode.rawValue).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 500)
            .padding(.top, 16)

            Group {
                switch viewMode {
                case .grid:
                    TVChannelGridView(model: model)
                case .epg:
                    TVGuideView(model: model)
                }
            }
        }
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .onExitCommand {
            model.selectedTab = .home
        }
    }
}
