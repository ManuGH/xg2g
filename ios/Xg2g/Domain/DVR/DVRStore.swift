// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// UI-facing store managing recordings and scheduled timers.
///
/// Strictly bound to `@MainActor` for thread-safe SwiftUI updates, delegating all
/// background fetching, caching, and network coordination to an injected `DVRRepository`.
@MainActor
@Observable
final class DVRStore {
    private(set) var recordings: [Recording] = []
    private(set) var timers: [DVRTimer] = []
    var selectedRecording: Recording?
    private(set) var isLoading: Bool = false
    private(set) var errorMessage: String?

    private let repository: any DVRRepository

    init(repository: any DVRRepository) {
        self.repository = repository
    }

    /// Fetches all recordings from the repository.
    func loadRecordings() async {
        isLoading = true
        defer { isLoading = false }
        do {
            recordings = try await repository.recordings()
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Fetches all scheduled and active timers.
    func loadTimers() async {
        do {
            timers = try await repository.timers()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Schedules a new recording timer on the receiver.
    func createTimer(serviceRef: String, name: String, description: String? = nil, begin: Date, end: Date) async {
        do {
            try await repository.createTimer(serviceRef: serviceRef, name: name, description: description, begin: begin, end: end)
            await loadTimers()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Deletes a recording by identifier.
    func deleteRecording(id: String) async {
        do {
            try await repository.deleteRecording(id: id)
            recordings.removeAll { $0.id == id }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Selects a recording for detail or playback inspection.
    func selectRecording(_ recording: Recording?) {
        selectedRecording = recording
    }
}
