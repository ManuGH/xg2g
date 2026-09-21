// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct RootContentView: View {
    @Bindable var model: AppModel
    var playbackManager: PlaybackManager

#if os(tvOS)
    var body: some View {
        ZStack {
            VStack(spacing: 0) {
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
                .frame(maxWidth: .infinity, maxHeight: .infinity)

                // 2. Mini-Player Docked Bar
                if playbackManager.presentationMode == .miniplayer {
                    MiniPlayerBar(playbackManager: playbackManager, model: model)
                        .padding(.horizontal, 64)
                        .padding(.bottom, 24)
                        .focusSection()
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }

            // 3. Native Fullscreen Video Player Overlay
            if playbackManager.presentationMode == .fullscreen, let channel = playbackManager.currentChannel {
                LivePlayerScreen(model: model, playbackManager: playbackManager, channel: channel)
                    .ignoresSafeArea()
                    .zIndex(20)
                    .transition(.opacity)
            } else if playbackManager.presentationMode == .fullscreen, let item = playbackManager.activeRecordingItem, let serverAddress = model.serverAddress {
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
                .ignoresSafeArea()
                .zIndex(20)
                .transition(.opacity)
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: playbackManager.presentationMode)
    }
#else
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
#endif
}
