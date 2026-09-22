// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Adapter bridging the Greenfield `DVRRepository` protocol to the existing `AppModel`.
@MainActor
final class LegacyBridgeDVRRepository: DVRRepository {
    private weak var appModel: AppModel?

    init(appModel: AppModel) {
        self.appModel = appModel
    }

    func recordings() async throws -> [Recording] {
        guard let appModel else { return [] }
        if appModel.recordings.isEmpty {
            await appModel.loadRecordings()
        }
        return appModel.recordings
    }

    func timers() async throws -> [DVRTimer] {
        guard let appModel else { return [] }
        if appModel.timers.isEmpty {
            await appModel.loadTimers()
        }
        return appModel.timers
    }

    func createTimer(serviceRef: String, name: String, description: String?, begin: Date, end: Date) async throws {
        guard let appModel else { return }
        let channel = appModel.channels.first(where: { $0.serviceRef == serviceRef }) ?? Channel(id: serviceRef, name: name, number: nil, serviceRef: serviceRef, logoURL: nil)
        _ = await appModel.addCustomTimer(channel: channel, name: name, description: description, start: begin, end: end)
    }

    func deleteRecording(id: String) async throws {
        guard let appModel else { return }
        if let recording = appModel.recordings.first(where: { $0.id == id }) {
            await appModel.deleteRecording(recording)
        }
    }
}
