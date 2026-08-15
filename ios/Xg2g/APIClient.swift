// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

enum HTTPMethod: String, Sendable {
    case get = "GET"
    case post = "POST"
    case put = "PUT"
    case delete = "DELETE"
}

/// Marker for endpoints that answer with no body (204, or a 200 we ignore).
struct EmptyResponse: Decodable, Equatable, Sendable {
    init() {}
    init(from decoder: Decoder) throws { self.init() }
}

/// One API call, described in terms of the v3 API rather than of URLs.
///
/// `path` is relative to `ServerAddress.apiBaseURL` — for example
/// `"services/bouquets"`. Callers never build absolute URLs, so the deployment
/// root stays the single source of truth for where the API lives.
struct APIRequest<Response: Decodable & Sendable>: Sendable {
    let method: HTTPMethod
    let path: String
    let query: [URLQueryItem]
    let body: Data?
    let contentType: String?

    init(
        method: HTTPMethod = .get,
        path: String,
        query: [URLQueryItem] = [],
        body: Data? = nil,
        contentType: String? = nil
    ) {
        self.method = method
        self.path = path
        self.query = query
        self.body = body
        self.contentType = contentType
    }
}

/// Adds authentication to an outgoing request.
///
/// A seam rather than an implementation: 2B supplies the DPoP-bound one. The
/// client itself never learns what a credential key is called, what a device
/// key is, or how a proof is built.
protocol RequestAuthorizer: Sendable {
    func authorized(_ request: URLRequest) async throws -> URLRequest
}

/// No authentication. Used for pairing endpoints, which are unauthenticated by
/// definition, and until 2B lands.
struct UnauthenticatedRequests: RequestAuthorizer {
    func authorized(_ request: URLRequest) async throws -> URLRequest { request }
}

protocol APIClient: Sendable {
    func send<Response>(_ request: APIRequest<Response>) async throws -> Response
}

/// `URLSession`-backed client.
///
/// Owns requests, JSON, status codes, timeouts and error mapping. It does not
/// own credential key names, Secure Enclave details, or playback decisions —
/// those belong to `CredentialStore`, `DeviceKeyStore` and the server
/// respectively.
struct HTTPAPIClient: APIClient {

    private let address: ServerAddress
    private let session: URLSession
    private let authorizer: RequestAuthorizer
    private let decoder: JSONDecoder

    init(
        address: ServerAddress,
        session: URLSession = .shared,
        authorizer: RequestAuthorizer = UnauthenticatedRequests()
    ) {
        self.address = address
        self.session = session
        self.authorizer = authorizer
        self.decoder = .xg2g
    }

    func send<Response>(_ request: APIRequest<Response>) async throws -> Response {
        let urlRequest = try makeURLRequest(for: request)
        let authorized: URLRequest
        do {
            authorized = try await authorizer.authorized(urlRequest)
        } catch {
            throw APIError.invalidEndpoint(path: request.path)
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: authorized)
        } catch let urlError as URLError {
            throw APIError.transport(TransportFailure(urlError: urlError))
        } catch {
            throw APIError.transport(.other(code: (error as NSError).code))
        }

        guard let http = response as? HTTPURLResponse else {
            throw APIError.transport(.other(code: 0))
        }

        let contentType = http.value(forHTTPHeaderField: "Content-Type")

        guard (200...299).contains(http.statusCode) else {
            throw problemOrHTTPError(data: data, status: http.statusCode, contentType: contentType)
        }

        if Response.self == EmptyResponse.self {
            // No body promised, so an unparseable one is not a contract breach.
            return EmptyResponse() as! Response
        }

        do {
            return try decoder.decode(Response.self, from: data)
        } catch {
            // HTTP said yes and the body says something else. This is the
            // "an HTML page came back with 200" case, kept distinguishable
            // from both transport and HTTP failures on purpose.
            throw APIError.unexpectedPayload(
                UnexpectedPayload(
                    url: authorized.url ?? address.apiBaseURL,
                    status: http.statusCode,
                    contentType: contentType,
                    bodyPreview: Self.preview(of: data),
                    expected: String(describing: Response.self)
                )
            )
        }
    }

    // MARK: - Internals

    private func makeURLRequest<Response>(for request: APIRequest<Response>) throws -> URLRequest {
        // Relative to the API base, which is itself derived from the stored
        // deployment root. A leading slash would escape it, so it is refused
        // rather than quietly normalized away.
        guard !request.path.hasPrefix("/") else {
            throw APIError.invalidEndpoint(path: request.path)
        }
        guard var components = URLComponents(
            url: address.apiBaseURL.appendingPathComponent(request.path),
            resolvingAgainstBaseURL: false
        ) else {
            throw APIError.invalidEndpoint(path: request.path)
        }
        if !request.query.isEmpty {
            components.queryItems = request.query
        }
        guard let url = components.url else {
            throw APIError.invalidEndpoint(path: request.path)
        }

        // The boundary is the API base, not the deployment root: a path such as
        // `api/v3/../../admin` resolves back inside the deployment and would
        // pass a root-level check while having left the API entirely. Reuses
        // the one containment implementation rather than growing a second.
        guard address.apiScope.contains(url) else {
            throw APIError.invalidEndpoint(path: request.path)
        }

        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = request.method.rawValue
        urlRequest.httpBody = request.body
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")
        urlRequest.setValue(address.origin.description, forHTTPHeaderField: "Origin")
        urlRequest.setValue("xg2g-ios/3.0", forHTTPHeaderField: "User-Agent")
        if let contentType = request.contentType {
            urlRequest.setValue(contentType, forHTTPHeaderField: "Content-Type")
        }
        return urlRequest
    }

    private func problemOrHTTPError(data: Data, status: Int, contentType: String?) -> APIError {
        let isProblemDocument = contentType?
            .lowercased()
            .contains("application/problem+json") ?? false

        if isProblemDocument, let problem = try? decoder.decode(ProblemDetails.self, from: data) {
            return .problem(problem)
        }

        // A non-2xx that is not a problem document usually means something other
        // than the API answered — a proxy, a gateway, or the SPA.
        return .http(status: status, contentType: contentType, bodyPreview: Self.preview(of: data))
    }

    private static func preview(of data: Data, limit: Int = 256) -> String {
        let text = String(decoding: data.prefix(limit), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return data.count > limit ? text + "…" : text
    }
}

extension JSONDecoder {
    /// Decoder matching what the Go backend actually emits.
    ///
    /// Go's `time.Time` marshals as RFC 3339 **Nano**: fractional seconds appear
    /// only when non-zero, so the same field is `…T12:00:00Z` one moment and
    /// `…T12:00:00.529Z` the next. Foundation's `.iso8601` strategy rejects the
    /// fractional form outright, which would make token expiry decoding fail
    /// intermittently — roughly whenever the server's clock lands on a
    /// non-integral second.
    static var xg2g: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let text = try container.decode(String.self)

            if let date = try? rfc3339WithFraction.parse(text) { return date }
            if let date = try? rfc3339.parse(text) { return date }

            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "expected an RFC 3339 timestamp, got \(text)"
            )
        }
        return decoder
    }

    // `Date.ISO8601FormatStyle` is a value type and therefore `Sendable`,
    // unlike `ISO8601DateFormatter` — no shared mutable state to reason about.
    private static let rfc3339WithFraction = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
    private static let rfc3339 = Date.ISO8601FormatStyle(includingFractionalSeconds: false)
}
