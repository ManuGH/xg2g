// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

// MARK: - Contract Domain Mapping

extension Xg2gContract.RecordingItem {
    func toDomain() -> Recording? {
        guard let title = title?.trimmingCharacters(in: .whitespaces), !title.isEmpty,
              let id = (recordingId ?? filename)?.trimmingCharacters(in: .whitespaces), !id.isEmpty
        else { return nil }

        let startSeconds = beginUnixSeconds ?? 0
        let duration = Int(durationSeconds ?? 0)

        var serverResume: Double? = nil
        if let r = resume, r.posSeconds > 0, !(r.finished ?? false) {
            serverResume = Double(r.posSeconds)
        }

        return Recording(
            id: id,
            title: title,
            description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
            beginDate: Date(timeIntervalSince1970: TimeInterval(startSeconds)),
            durationSeconds: duration,
            serviceRef: serviceRef,
            filename: filename,
            status: status.rawValue,
            serverResumePos: serverResume
        )
    }
}
