// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Reads timers from the backend DVR scheduler.
actor TimersRepository {

    private let api: APIClient

    init(api: APIClient) {
        self.api = api
    }

    func timers() async throws -> [DVRTimer] {
        let response: Xg2gContract.TimerList = try await api.send(
            APIRequest(method: .get, path: "timers")
        )

        return response.items
            .compactMap { $0.toDomain() }
            .sorted { $0.beginDate < $1.beginDate }
    }

    func createTimer(serviceRef: String, name: String, description: String? = nil, begin: Date, end: Date) async throws {
        let body = Xg2gContract.TimerCreateRequest(
            begin: Int64(begin.timeIntervalSince1970),
            end: Int64(end.timeIntervalSince1970),
            name: name,
            serviceRef: serviceRef,
            description: description
        )
        let data = try JSONEncoder().encode(body)
        let _: EmptyResponse = try await api.send(
            APIRequest(method: .post, path: "timers", body: data, contentType: "application/json")
        )
    }

    func deleteTimer(id: String) async throws {
        let _: EmptyResponse = try await api.send(
            APIRequest(method: .delete, path: "timers/\(id)")
        )
    }
}
