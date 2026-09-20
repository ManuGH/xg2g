// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Search as a destination of its own.
///
/// iPhone and iPad keep the search field in the Home hub's navigation bar. On
/// tvOS that field would take first focus and unfold the inline keyboard over
/// the hub, so search is a tab here, the way the system apps do it. The view
/// is the hub's search branch lifted out: same engine, same result list, same
/// actions.
struct SearchView: View {

    @Bindable var model: AppModel
    @State private var searchText = ""
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    private var trimmedQuery: String {
        searchText.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                if trimmedQuery.isEmpty {
                    ContentUnavailableView(
                        "Suche",
                        systemImage: "magnifyingglass",
                        description: Text("Sender, laufende und kommende Sendungen.")
                    )
                    .foregroundStyle(Theme.Colors.textSecondary)
                } else {
                    SmartSearchResultsView(
                        result: SmartSearchEngine.search(
                            query: searchText,
                            channels: model.channels,
                            schedule: model.schedule,
                            fullEpg: model.fullEpg,
                            now: Date.now
                        ),
                        onPlayChannel: { channel in
                            Haptics.shared.impact(.light)
                            model.playingChannel = channel
                        },
                        onOpenShowDetail: { channel, entry in
                            selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                        },
                        onRecordShow: { channel, entry in
                            scheduleTimer(channel: channel, entry: entry)
                        }
                    )
                }

                if let message = recordConfirmationMessage {
                    VStack {
                        Spacer()
                        HStack(spacing: 8) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(Theme.Colors.statusSuccess)
                            Text(message)
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                        .padding(.bottom, 20)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                }
            }
            .navigationTitle(Tab.search.rawValue)
            .searchable(text: $searchText, prompt: "Sendung, Film oder Sender suchen…")
            .sheet(item: $selectedDetail) { payload in
                ProgramDetailSheet(
                    channel: payload.channel,
                    entry: payload.entry,
                    channelSchedule: model.channelSchedule(for: payload.channel),
                    model: model,
                    onRecord: { entry in scheduleTimer(channel: payload.channel, entry: entry) }
                )
            }
#if os(tvOS)
            .onExitCommand {
                model.selectedTab = .home
            }
#endif
        }
    }

    private func scheduleTimer(channel: Channel, entry: NowNext.Entry) {
        Task {
            let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
            if ok {
                Haptics.shared.impact(.medium)
                withAnimation {
                    recordConfirmationMessage = "„\(entry.title)“ programmiert"
                }
                try? await Task.sleep(for: .seconds(3))
                withAnimation {
                    recordConfirmationMessage = nil
                }
            }
        }
    }
}
