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
                        ScrollView {
                            LazyVGrid(
                                columns: [GridItem(.adaptive(minimum: 340, maximum: 540), spacing: 14)],
                                spacing: 14
                            ) {
                                ForEach(model.timers) { timer in
                                    TimerRow(timer: timer)
                                        .contextMenu {
                                            Button(role: .destructive) {
                                                Task { await model.deleteTimer(timer) }
                                            } label: {
                                                Label("Timer löschen", systemImage: "trash")
                                            }
                                        }
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.vertical, 10)
                        }
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
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                ZStack {
                    RoundedRectangle(cornerRadius: 10)
                        .fill(timer.isRunning ? Theme.Colors.statusError.opacity(0.15) : Theme.Colors.surfaceElevated)
                        .overlay(
                            RoundedRectangle(cornerRadius: 10)
                                .strokeBorder(timer.isRunning ? Theme.Colors.statusError.opacity(0.3) : Theme.Colors.borderSubtle, lineWidth: 1)
                        )

                    Image(systemName: timer.isRunning ? "record.circle.fill" : "clock.fill")
                        .font(.title2)
                        .foregroundStyle(timer.isRunning ? Theme.Colors.statusError : Theme.Colors.accentAction)
                }
                .frame(width: 44, height: 44)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        if timer.isRunning {
                            PulsingLiveDot(size: 5)
                            Text("NIMMT AUF")
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.statusError)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1)
                                .background(Theme.Colors.statusError.opacity(0.15), in: Capsule())
                        }

                        Text(timer.name)
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                    }

                    Text(timer.serviceName ?? "Timer")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(Theme.Colors.accentLive)
                }

                Spacer()
            }

            if let description = timer.description, !description.isEmpty {
                Text(description)
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .lineLimit(2)
            }

            HStack {
                Label(timer.formattedTimeRange, systemImage: "calendar")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)

                Spacer()
            }
        }
        .padding(14)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
    }
}

// MARK: - Add Timer Sheet

struct AddTimerSheet: View {

    let model: AppModel
    @Environment(\.dismiss) private var dismiss

    @State private var name = ""
    @State private var description = ""
    @State private var selectedChannel: Channel?
    @State private var begin = Date.now.addingTimeInterval(300)
    @State private var end = Date.now.addingTimeInterval(3900)
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                Form {
                    Section("Sendungsdaten") {
                        TextField("Titel", text: $name)
                        TextField("Beschreibung (optional)", text: $description)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section("Sender") {
                        Picker("Sender", selection: $selectedChannel) {
                            Text("Sender wählen…").tag(nil as Channel?)
                            ForEach(model.channels) { ch in
                                Text(ch.name).tag(ch as Channel?)
                            }
                        }
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section("Sendezeit") {
                        DatePicker("Startzeit", selection: $begin)
                        DatePicker("Endzeit", selection: $end)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)
                }
                .scrollContentBackground(.hidden)
            }
            .navigationTitle("Neuer Timer")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen") {
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Planen") {
                        save()
                    }
                    .disabled(name.isEmpty || selectedChannel == nil || isSaving)
                }
            }
        }
    }

    private func save() {
        guard let channel = selectedChannel, !name.isEmpty else { return }
        isSaving = true
        Task {
            _ = await model.addCustomTimer(
                channel: channel,
                name: name,
                description: description,
                start: begin,
                end: end
            )
            isSaving = false
            dismiss()
        }
    }
}
