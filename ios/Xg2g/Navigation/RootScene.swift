// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Top-level scene router choosing the appropriate navigation shell based on the interaction model.
///
/// Injects the `AppComposition` dependency tree into the environment, decoupling
/// subviews from the legacy global `AppModel`.
struct RootScene: View {
    @Bindable var model: AppModel
    @ObservedObject var playbackManager: PlaybackManager
    private let composition: AppComposition

    init(model: AppModel, playbackManager: PlaybackManager) {
        self.model = model
        self.playbackManager = playbackManager
        self.composition = AppComposition.makeBridged(appModel: model)
    }

    var body: some View {
        Group {
            switch composition.capabilities.interactionModel {
            case .focusRemote:
                TVRootView(model: model)
            case .touchRegular, .pointerKeyboard:
                PadRootView(model: model, playbackManager: playbackManager)
            case .touchCompact:
                PhoneRootView(model: model, playbackManager: playbackManager)
            }
        }
        .withAppComposition(composition)
    }
}
