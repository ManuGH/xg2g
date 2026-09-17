// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct RootContentView: View {
    @Bindable var model: AppModel
    var playbackManager: PlaybackManager

    var body: some View {
        ZStack(alignment: .bottom) {
            // 1. App Navigation
            Group {
                switch model.state {
                case .needsServer:
                    ServerSetupView(model: model)
                case .needsPairing, .needsRePairing:
                    PairingView(model: model)
                case .ready:
                    AdaptiveAppNavigation(model: model)
                }
            }

            // 2. Mini-Player Floating Bar (above TabBar)
            if playbackManager.presentationMode == .miniplayer {
                MiniPlayerBar(playbackManager: playbackManager, model: model)
                    .padding(.bottom, UIDevice.current.userInterfaceIdiom == .pad ? 16 : 56)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .zIndex(10)
            }
        }
        .fullScreenCover(isPresented: Binding(
            get: { playbackManager.presentationMode == .fullscreen && playbackManager.currentChannel != nil },
            set: { isPresented in
                if !isPresented && playbackManager.presentationMode == .fullscreen {
                    playbackManager.minimize()
                }
            }
        )) {
            if let channel = playbackManager.currentChannel {
                LivePlayerScreen(model: model, playbackManager: playbackManager, channel: channel)
            }
        }
        .fullScreenCover(item: Binding(
            get: {
                playbackManager.presentationMode == .fullscreen ? playbackManager.activeRecordingItem : nil
            },
            set: { item in
                if item == nil && playbackManager.presentationMode == .fullscreen {
                    playbackManager.minimize()
                }
            }
        )) { item in
            // No configured deployment means nothing to play. The screen used to
            // take a string and repair it; now the address either exists or the
            // cover does not open.
            if let serverAddress = model.serverAddress {
            RecordingPlayerScreen(
                recording: item.recording,
                serverAddress: serverAddress,
                initialPosition: item.initialPosition,
                model: model,
                onProgressUpdate: { current, total in
                    model.updateRecordingProgress(
                        id: item.recording.id,
                        currentTime: current,
                        totalDuration: total,
                        title: item.recording.title
                    )
                }
            )
            .ignoresSafeArea(.all)
            }
        }
        .fullScreenCover(item: Binding(
            get: { playbackManager.activeOfflineRecording },
            set: { if $0 == nil { playbackManager.stop() } }
        )) { offline in
            OfflinePlayerScreen(offlineRecording: offline)
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: playbackManager.presentationMode)
    }
}
