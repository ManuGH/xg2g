// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct TimersView: View {

    let model: AppModel
    @State private var showingAddTimer = false

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
                            description: Text("Derzeit sind keine Aufnahme-Timer auf der Vu+ Uno 4K geplant.")
                        )
                        .foregroundStyle(Theme.Colors.textSecondary)
                    } else {
                        List(model.timers) { timer in
                            TimerRow(timer: timer)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                                .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                    Button(role: .destructive) {
                                        Task { await model.deleteTimer(timer) }
                                    } label: {
                                        Label("Löschen", systemImage: "trash")
                                    }
                                }
                        }
                        .listStyle(.plain)
                        .scrollContentBackground(.hidden)
                        .refreshable { await model.loadTimers() }
                    }
                }
            }
            .navigationTitle("Timer")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        showingAddTimer = true
                    } label: {
                        Image(systemName: "plus.circle.fill")
                            .font(.title3)
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                }

                if model.isLoadingTimers && !model.timers.isEmpty {
                    ToolbarItem(placement: .status) {
                        ProgressView()
                            .tint(Theme.Colors.accentAction)
                    }
                }
            }
            .sheet(isPresented: $showingAddTimer) {
                AddTimerSheet(model: model)
            }
        }
    }
}

// MARK: - Timer Row

struct TimerRow: View {

    let timer: DVRTimer

    var body: some View {
        HStack(spacing: 14) {
            ZStack {
                Image(systemName: timer.isRunning ? "record.circle.fill" : "clock.fill")
                    .font(.title2)
                    .foregroundStyle(timer.isRunning ? Theme.Colors.statusError : Theme.Colors.accentAction)
            }
            .frame(width: 44, height: 44)
            .background(Theme.Colors.surfaceGlass, in: RoundedRectangle(cornerRadius: 8))

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    if timer.isRunning {
                        PulsingLiveDot(size: 6)
                    }

                    Text(timer.name)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    Spacer()

                    Text(timer.isRunning ? "AUFNAHME LÄUFT" : timer.state.uppercased())
                        .font(.system(size: 9, weight: .bold, design: .monospaced))
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

// MARK: - Add Timer Sheet

struct AddTimerSheet: View {

    let model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var title: String = ""
    @State private var selectedChannelIndex: Int = 0
    @State private var startDate: Date = Date()
    @State private var durationMinutes: Int = 90
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                Form {
                    Section("Sendungsdetails") {
                        TextField("Titel der Aufnahme", text: $title)
                            .foregroundStyle(Theme.Colors.textPrimary)

                        if !model.channels.isEmpty {
                            Picker("Kanal", selection: $selectedChannelIndex) {
                                ForEach(model.channels.indices, id: \.self) { idx in
                                    Text(model.channels[idx].name).tag(idx)
                                }
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                        }
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section("Zeitplan") {
                        DatePicker("Startzeit", selection: $startDate)
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Picker("Dauer", selection: $durationMinutes) {
                            Text("30 Min.").tag(30)
                            Text("45 Min.").tag(45)
                            Text("60 Min.").tag(60)
                            Text("90 Min.").tag(90)
                            Text("120 Min.").tag(120)
                            Text("180 Min.").tag(180)
                        }
                        .foregroundStyle(Theme.Colors.textPrimary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)
                }
                .scrollContentBackground(.hidden)
            }
            .navigationTitle("Neuer Timer")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen") { dismiss() }
                }

                ToolbarItem(placement: .confirmationAction) {
                    Button("Speichern") {
                        saveTimer()
                    }
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || model.channels.isEmpty || isSaving)
                }
            }
        }
    }

    private func saveTimer() {
        guard !model.channels.isEmpty else { return }
        let channel = model.channels[selectedChannelIndex]
        let end = startDate.addingTimeInterval(TimeInterval(durationMinutes * 60))
        isSaving = true

        Task {
            let entry = NowNext.Entry(
                title: title.trimmingCharacters(in: .whitespaces),
                description: nil,
                start: startDate,
                end: end
            )
            _ = await model.scheduleProgramTimer(channel: channel, entry: entry, leadMinutes: 0, trailMinutes: 0)
            dismiss()
        }
    }
}
