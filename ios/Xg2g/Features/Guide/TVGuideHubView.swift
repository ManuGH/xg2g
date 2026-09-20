// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Unified "TV & Guide" Hub: Consolidates the channel list and EPG timeline into one cohesive screen.
///
/// Provides seamless switching between:
/// - ≣ Sender (ChannelListView: Quick zapping, live progress, and favorite channels)
/// - ⊞ Programm (GuideView: Multi-day EPG, time anchors, and grid view)
struct TVGuideHubView: View {

    @Bindable var model: AppModel
    @AppStorage("xg2g.tvGuideHubMode") var hubMode: TVGuideHubMode = .channels

    enum TVGuideHubMode: String, CaseIterable, Identifiable {
        case channels = "Sender"
        case guide = "Programm"

        var id: String { rawValue }
    }

    var body: some View {
        Group {
            switch hubMode {
            case .channels:
                ChannelListView(model: model, hubMode: $hubMode)
            case .guide:
                GuideView(model: model, hubMode: $hubMode)
            }
        }
    }
}
