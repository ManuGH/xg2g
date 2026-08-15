// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A scheduled or active DVR timer.
struct DVRTimer: Identifiable, Equatable, Sendable {
    let id: String
    let name: String
    let description: String?
    let serviceRef: String
    let serviceName: String?
    let beginDate: Date
    let endDate: Date
    let state: String

    var formattedTimeRange: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        let endFormatter = DateFormatter()
        endFormatter.dateStyle = .none
        endFormatter.timeStyle = .short
        return "\(formatter.string(from: beginDate)) – \(endFormatter.string(from: endDate))"
    }

    var isRunning: Bool {
        state.lowercased() == "running" || state.lowercased() == "active"
    }
}

/// Reads timers from the backend DVR scheduler.
actor TimersRepository {

    private let api: APIClient

    init(api: APIClient) {
        self.api = api
    }

    func timers() async throws -> [DVRTimer] {
        let items: [TimerWire.Item] = try await api.send(
            APIRequest(method: .get, path: "timers")
        )

        return items
            .compactMap { $0.toDomain() }
            .sorted { $0.beginDate < $1.beginDate }
    }
    func createTimer(serviceRef: String, name: String, description: String? = nil, begin: Date, end: Date) async throws {
        let body = TimerWire.CreateRequest(
            serviceRef: serviceRef,
            name: name,
            description: description,
            begin: Int64(begin.timeIntervalSince1970),
            end: Int64(end.timeIntervalSince1970)
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

// MARK: - Wire

enum TimerWire {

    struct CreateRequest: Encodable, Sendable {
        let serviceRef: String
        let name: String
        let description: String?
        let begin: Int64
        let end: Int64
    }

    struct Item: Decodable, Sendable {
        let timerId: String?
        let name: String?
        let description: String?
        let serviceRef: String?
        let serviceName: String?
        let begin: Int64?
        let end: Int64?
        let state: String?

        func toDomain() -> DVRTimer? {
            guard let id = timerId?.trimmingCharacters(in: .whitespaces), !id.isEmpty,
                  let name = name?.trimmingCharacters(in: .whitespaces), !name.isEmpty,
                  let serviceRef = serviceRef?.trimmingCharacters(in: .whitespaces), !serviceRef.isEmpty
            else { return nil }

            let startSeconds = begin ?? 0
            let endSeconds = end ?? (startSeconds + 3600)

            return DVRTimer(
                id: id,
                name: name,
                description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
                serviceRef: serviceRef,
                serviceName: serviceName?.trimmingCharacters(in: .whitespaces).isEmpty == false ? serviceName : nil,
                beginDate: Date(timeIntervalSince1970: TimeInterval(startSeconds)),
                endDate: Date(timeIntervalSince1970: TimeInterval(endSeconds)),
                state: state ?? "waiting"
            )
        }
    }
}
