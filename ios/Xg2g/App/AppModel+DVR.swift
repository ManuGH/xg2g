// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension AppModel {

    // MARK: - Recording Progress & Resume

    func updateRecordingProgress(id: String, currentTime: Double, totalDuration: Double, title: String? = nil, channelName: String? = nil) {
        guard totalDuration > 0 else { return }
        let fraction = min(1.0, max(0.0, currentTime / totalDuration))
        let finished = fraction > 0.95
        if finished {
            // Finished
            recordingProgress.removeValue(forKey: id)
        } else if fraction > 0.02 {
            recordingProgress[id] = currentTime
        }
        UserDefaults.standard.set(recordingProgress, forKey: "xg2g.recordingProgress")

        // Sync to xg2g server profile in background (Cross-Device Sync)
        if let repo = recordingsRepository {
            Task {
                _ = try? await session?.validSession()
                try? await repo.saveResume(
                    id: id,
                    position: currentTime,
                    total: totalDuration,
                    finished: finished,
                    title: title ?? "",
                    channel: channelName ?? ""
                )
            }
        }
    }

    func resumePosition(for id: String) -> Double? {
        recordingProgress[id]
    }

    // MARK: - Load Recordings & Timers

    func loadRecordings() async {
        guard let recordingsRepository, !isLoadingRecordings else { return }
        isLoadingRecordings = true
        defer { isLoadingRecordings = false }

        do {
            _ = try? await session?.validSession()
            let list = try await recordingsRepository.recordings()
            recordings = list

            // Seed local cache from server profile resume states
            for rec in list {
                if let sPos = rec.serverResumePos, sPos > 0 {
                    recordingProgress[rec.id] = sPos
                }
            }
            UserDefaults.standard.set(recordingProgress, forKey: "xg2g.recordingProgress")
            lastError = nil
        } catch {
            handle(error)
        }
    }

    func loadTimers() async {
        guard let timersRepository, !isLoadingTimers else { return }
        isLoadingTimers = true
        defer { isLoadingTimers = false }

        do {
            _ = try? await session?.validSession()
            timers = try await timersRepository.timers()
            lastError = nil
        } catch {
            handle(error)
        }
    }

    // MARK: - DVR & Timer Management

    func scheduleProgramTimer(
        channel: Channel,
        entry: NowNext.Entry,
        leadMinutes: Int = 3,
        trailMinutes: Int = 7
    ) async -> Bool {
        guard let timersRepository else { return false }
        let begin = entry.start.addingTimeInterval(TimeInterval(-leadMinutes * 60))
        let end = entry.end.addingTimeInterval(TimeInterval(trailMinutes * 60))
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: entry.title,
                description: entry.description,
                begin: begin,
                end: end
            )
            await loadTimers()
            return true
        } catch {
            handle(error)
            return false
        }
    }

    func addCustomTimer(
        channel: Channel,
        name: String,
        description: String?,
        start: Date,
        end: Date
    ) async -> Bool {
        guard let timersRepository else { return false }
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: name,
                description: description,
                begin: start,
                end: end
            )
            await loadTimers()
            return true
        } catch {
            handle(error)
            return false
        }
    }

    func recordLiveNow(channel: Channel, durationMinutes: Int = 120) async -> Bool {
        guard let timersRepository else { return false }
        let entry = schedule[channel.serviceRef]?.now
        let title = entry?.title ?? "\(channel.name) Sofortaufnahme"
        let begin = Date()
        let end = entry?.end ?? begin.addingTimeInterval(TimeInterval(durationMinutes * 60))
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: title,
                description: entry?.description,
                begin: begin,
                end: end.addingTimeInterval(300)
            )
            await loadTimers()
            return true
        } catch {
            handle(error)
            return false
        }
    }

    func deleteTimer(_ timer: DVRTimer) async {
        guard let timersRepository else { return }
        do {
            try await timersRepository.deleteTimer(id: timer.id)
            timers.removeAll { $0.id == timer.id }
        } catch {
            handle(error)
        }
    }

    func deleteRecording(_ recording: Recording) async {
        guard let recordingsRepository else { return }
        do {
            try await recordingsRepository.deleteRecording(id: recording.id)
            recordings.removeAll { $0.id == recording.id }
        } catch {
            handle(error)
        }
    }

    func recordingPlaybackUrl(for recordingId: String) async throws -> String? {
        guard let recordingsRepository else { return nil }
        return try await recordingsRepository.playbackUrl(for: recordingId)
    }
}
