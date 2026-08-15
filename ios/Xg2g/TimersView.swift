// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct TimersView: View {

    let model: AppModel

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                Group {
                    if model.timers.isEmpty && model.isLoadingTimers {
                        ProgressView("Lade Timer…")
                            .tint(Theme.Colors.accentAction)
                            .foregroundStyle(Theme.Colors.textSecondary)
                    } else if model.timers.isEmpty {
                        ContentUnavailableView(
                            "Keine Timer",
                            systemImage: "clock.badge.checkmark",
                            description: Text("Derzeit sind keine Aufnahme-Timer geplant.")
                        )
                        .foregroundStyle(Theme.Colors.textSecondary)
                    } else {
                        List(model.timers) { timer in
                            TimerRow(timer: timer)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                        }
                        .listStyle(.plain)
                        .scrollContentBackground(.hidden)
                        .refreshable { await model.loadTimers() }
                    }
                }
            }
            .navigationTitle("Timer")
            .toolbar {
                if model.isLoadingTimers && !model.timers.isEmpty {
                    ProgressView()
                        .tint(Theme.Colors.accentAction)
                }
            }
        }
    }
}

struct TimerRow: View {

    let timer: DVRTimer

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: timer.isRunning ? "record.circle.fill" : "clock.fill")
                .font(.title2)
                .foregroundStyle(timer.isRunning ? Theme.Colors.statusError : Theme.Colors.accentAction)
                .frame(width: 44, height: 44)
                .background(Theme.Colors.surfaceGlass, in: RoundedRectangle(cornerRadius: 8))

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(timer.name)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    Spacer()

                    Text(timer.state.uppercased())
                        .font(.caption2.bold().monospaced())
                        .foregroundStyle(timer.isRunning ? Theme.Colors.statusError : Theme.Colors.textTertiary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            (timer.isRunning ? Theme.Colors.statusError : Theme.Colors.borderSubtle).opacity(0.15),
                            in: Capsule()
                        )
                }

                if let serviceName = timer.serviceName {
                    Text(serviceName)
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }

                Text(timer.formattedTimeRange)
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
        }
        .padding(.vertical, 4)
    }
}
