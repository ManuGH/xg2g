// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Domain contract for digital video recording (DVR) and scheduled timer operations.
protocol DVRRepository: Sendable {
    /// Loads all completed and in-progress recordings.
    func recordings() async throws -> [Recording]

    /// Loads all active and scheduled recording timers.
    func timers() async throws -> [DVRTimer]

    /// Schedules a new recording timer on the receiver.
    func createTimer(serviceRef: String, name: String, description: String?, begin: Date, end: Date) async throws

    /// Deletes a recording by identifier.
    func deleteRecording(id: String) async throws
}
