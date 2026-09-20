// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

#if os(iOS)
@main
struct Xg2gApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
        }
        .commands {
            SidebarCommands()

            CommandMenu("Navigation") {
                Button("Für dich") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.home])
                }
                .keyboardShortcut("1", modifiers: .command)

                Button("Live TV") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.liveTV])
                }
                .keyboardShortcut("2", modifiers: .command)

                Button("Programm") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.guide])
                }
                .keyboardShortcut("3", modifiers: .command)

                Button("Aufnahmen") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.recordings])
                }
                .keyboardShortcut("4", modifiers: .command)

                Button("Timer") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.timers])
                }
                .keyboardShortcut("5", modifiers: .command)

                Divider()

                Button("Einstellungen") {
                    NotificationCenter.default.post(name: .selectAppTab, object: nil, userInfo: ["tab": Tab.settings])
                }
                .keyboardShortcut(",", modifiers: .command)
            }

            CommandMenu("Wiedergabe") {
                Button("Wiedergabe / Pause") {
                    NotificationCenter.default.post(name: .playerTogglePlayPause, object: nil)
                }
                .keyboardShortcut(.space, modifiers: [])

                Divider()

                Button("30s zurückspulen") {
                    NotificationCenter.default.post(name: .playerSeekBackward, object: nil)
                }
                .keyboardShortcut(.leftArrow, modifiers: [])

                Button("30s vorspulen") {
                    NotificationCenter.default.post(name: .playerSeekForward, object: nil)
                }
                .keyboardShortcut(.rightArrow, modifiers: [])

                Divider()

                Button("Nächster Kanal (Zappen)") {
                    NotificationCenter.default.post(name: .playerZapNext, object: nil)
                }
                .keyboardShortcut(.upArrow, modifiers: [])

                Button("Vorheriger Kanal (Zappen)") {
                    NotificationCenter.default.post(name: .playerZapPrevious, object: nil)
                }
                .keyboardShortcut(.downArrow, modifiers: [])

                Divider()

                Button("Sender-Schnellauswahl") {
                    NotificationCenter.default.post(name: .playerToggleZapDrawer, object: nil)
                }
                .keyboardShortcut("z", modifiers: [])

                Button("Bildformat durchschalten") {
                    NotificationCenter.default.post(name: .playerCycleAspect, object: nil)
                }
                .keyboardShortcut("f", modifiers: [])

                Button("Diagnose & Telemetrie-HUD") {
                    NotificationCenter.default.post(name: .playerToggleHUD, object: nil)
                }
                .keyboardShortcut("i", modifiers: [])

                Button("Bild-in-Bild starten") {
                    NotificationCenter.default.post(name: .playerStartPiP, object: nil)
                }
                .keyboardShortcut("p", modifiers: [])

                Divider()

                Button("Player schließen") {
                    NotificationCenter.default.post(name: .playerClose, object: nil)
                }
                .keyboardShortcut(.escape, modifiers: [])

                Button("Player schließen (Magic Keyboard)") {
                    NotificationCenter.default.post(name: .playerClose, object: nil)
                }
                .keyboardShortcut(".", modifiers: .command)
            }
        }
    }
}
#endif
