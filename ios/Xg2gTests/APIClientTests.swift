// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

// MARK: - Stub transport

final class StubURLProtocol: URLProtocol {
    struct Stub: @unchecked Sendable {
        var status: Int = 200
        var headers: [String: String] = ["Content-Type": "application/json"]
        var body: Data = Data("{}".utf8)
        var error: URLError?
    }

    nonisolated(unsafe) private static var _stub = Stub()
    nonisolated(unsafe) private static var _lastRequest: URLRequest?
    private static let lock = NSLock()

    static var stub: Stub {
        get { lock.lock(); defer { lock.unlock() }; return _stub }
        set { lock.lock(); defer { lock.unlock() }; _stub = newValue }
    }

    static var lastRequest: URLRequest? {
        lock.lock(); defer { lock.unlock() }; return _lastRequest
    }

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        _stub = Stub()
        _lastRequest = nil
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.lock()
        Self._lastRequest = request
        let stub = Self._stub
        Self.lock.unlock()

        if let error = stub.error {
            client?.urlProtocol(self, didFailWithError: error)
            return
        }

        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: stub.status,
            httpVersion: "HTTP/1.1",
            headerFields: stub.headers
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: stub.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

private struct SampleResource: Decodable, Equatable, Sendable {
    let id: String
    let name: String
}

private struct Session: Decodable, Equatable, Sendable {
    let token: String
    let expiresAt: Date
}

// MARK: - Tests

@Suite(.serialized)
struct APIClientTests {

    private func makeClient(root: String = "https://tv.example/") throws -> HTTPAPIClient {
        StubURLProtocol.reset()
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        return HTTPAPIClient(
            address: try ServerAddressParser.parseTrusted(root),
            session: URLSession(configuration: configuration)
        )
    }

    private func respond(status: Int = 200, contentType: String? = "application/json", body: String) {
        var stub = StubURLProtocol.Stub()
        stub.status = status
        stub.headers = contentType.map { ["Content-Type": $0] } ?? [:]
        stub.body = Data(body.utf8)
        StubURLProtocol.stub = stub
    }

    // MARK: URL construction

    @Test func requestsAreBuiltFromTheDeploymentRoot() async throws {
        let client = try makeClient()
        respond(body: #"{"id":"1","name":"Favourites"}"#)

        _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))

        #expect(StubURLProtocol.lastRequest?.url?.absoluteString == "https://tv.example/api/v3/services/bouquets")
    }

    @Test func requestsRespectADeploymentSubPath() async throws {
        let client = try makeClient(root: "https://tv.example/xg2g/")
        respond(body: #"{"id":"1","name":"Favourites"}"#)

        _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))

        #expect(
            StubURLProtocol.lastRequest?.url?.absoluteString
                == "https://tv.example/xg2g/api/v3/services/bouquets"
        )
    }

    @Test func queryItemsAreAppended() async throws {
        let client = try makeClient()
        respond(body: #"{"id":"1","name":"x"}"#)

        _ = try await client.send(
            APIRequest<SampleResource>(path: "epg", query: [URLQueryItem(name: "limit", value: "5")])
        )

        #expect(StubURLProtocol.lastRequest?.url?.query == "limit=5")
    }

    @Test func methodAndBodyArePassedThrough() async throws {
        let client = try makeClient()
        respond(status: 204, contentType: nil, body: "")

        _ = try await client.send(
            APIRequest<EmptyResponse>(
                method: .post,
                path: "system/refresh",
                body: Data(#"{"a":1}"#.utf8),
                contentType: "application/json"
            )
        )

        #expect(StubURLProtocol.lastRequest?.httpMethod == "POST")
        #expect(StubURLProtocol.lastRequest?.value(forHTTPHeaderField: "Content-Type") == "application/json")
    }

    /// Request-specific headers reach the wire, and do not displace the ones every
    /// request carries.
    ///
    /// `Origin` is the one that matters. The backend refuses every unsafe method
    /// without it — CSRF_FORBIDDEN, measured against staging — and the preparation
    /// endpoints are POST and DELETE throughout. A per-request header that shadowed it
    /// would make channel changes fail on the device while every test passed here.
    @Test func perRequestHeadersDoNotDisplaceTheStandardOnes() async throws {
        let client = try makeClient()
        respond(status: 202, contentType: "application/json", body: "{}")

        _ = try await client.send(
            APIRequest<EmptyResponse>(
                method: .post,
                path: "stream/prepare",
                query: [URLQueryItem(name: "sref", value: "1:0:19:132F:3EF:1:C00000:0:0:0:")],
                headers: [
                    "X-Xg2g-Client-Id": "sterling-abc",
                    "X-Xg2g-Zap-Id": "z-7",
                ]
            )
        )

        let sent = StubURLProtocol.lastRequest
        #expect(sent?.value(forHTTPHeaderField: "X-Xg2g-Client-Id") == "sterling-abc")
        #expect(sent?.value(forHTTPHeaderField: "X-Xg2g-Zap-Id") == "z-7")
        #expect(sent?.value(forHTTPHeaderField: "Origin")?.isEmpty == false,
                "an unsafe method without Origin is refused by the backend")
    }

    /// A path is relative to the API base by contract; an absolute one would
    /// escape the deployment root.
    @Test func absolutePathsAreRefused() async throws {
        let client = try makeClient()

        await #expect(throws: APIError.invalidEndpoint(path: "/api/v3/services")) {
            _ = try await client.send(APIRequest<SampleResource>(path: "/api/v3/services"))
        }
    }

    /// Reuses the closed containment layer rather than trusting every future
    /// call site to compose a well-behaved path.
    @Test func pathsClimbingOutOfTheDeploymentAreRefused() async throws {
        let client = try makeClient(root: "https://tv.example/xg2g/")

        await #expect(throws: APIError.invalidEndpoint(path: "../../admin")) {
            _ = try await client.send(APIRequest<SampleResource>(path: "../../admin"))
        }
    }

    // MARK: Decoding

    @Test func decodesASuccessfulResponse() async throws {
        let client = try makeClient()
        respond(body: #"{"id":"42","name":"Favourites"}"#)

        let bouquet = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))

        #expect(bouquet == SampleResource(id: "42", name: "Favourites"))
    }

    /// Go's `time.Time` emits RFC 3339 **Nano**: fractional seconds appear only
    /// when non-zero, so both spellings occur for the same field.
    @Test func decodesTimestampsWithAndWithoutFractionalSeconds() async throws {
        let client = try makeClient()

        respond(body: #"{"token":"t","expiresAt":"2026-04-09T12:00:00Z"}"#)
        let whole = try await client.send(APIRequest<Session>(path: "auth/session"))
        #expect(whole.expiresAt == Date(timeIntervalSince1970: 1_775_736_000))

        respond(body: #"{"token":"t","expiresAt":"2026-04-09T12:00:00.500Z"}"#)
        let fractional = try await client.send(APIRequest<Session>(path: "auth/session"))
        #expect(fractional.expiresAt == Date(timeIntervalSince1970: 1_775_736_000.5))
    }

    @Test func emptyResponsesDoNotRequireABody() async throws {
        let client = try makeClient()
        respond(status: 204, contentType: nil, body: "")

        let result = try await client.send(APIRequest<EmptyResponse>(method: .post, path: "system/refresh"))

        #expect(result == EmptyResponse())
    }

    // MARK: The three error categories

    /// The Android failure mode: the request reached the SPA, which answered
    /// with HTML and status 200. Neither transport nor HTTP error — and
    /// invisible if everything collapses into "network error".
    @Test func htmlWithStatus200IsAnUnexpectedPayloadNotANetworkError() async throws {
        let client = try makeClient()
        respond(contentType: "text/html; charset=utf-8", body: "<!doctype html><html><body>xg2g</body></html>")

        do {
            _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))
            Issue.record("expected a failure")
        } catch let error as APIError {
            guard case .unexpectedPayload(let payload) = error else {
                Issue.record("expected .unexpectedPayload, got \(error)")
                return
            }
            #expect(payload.status == 200)
            #expect(payload.contentType == "text/html; charset=utf-8")
            #expect(payload.bodyPreview.contains("<!doctype html>"))
            #expect(payload.expected == "SampleResource")
            #expect(!error.isRetryable, "a wrong body will be just as wrong next time")
        }
    }

    @Test func aProblemDocumentBecomesADomainError() async throws {
        let client = try makeClient()
        respond(
            status: 403,
            contentType: "application/problem+json",
            body: #"""
            {"type":"about:blank","title":"Forbidden","status":403,
             "requestId":"req_abc123","code":"ADMISSION_DENIED","detail":"nope"}
            """#
        )

        do {
            _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))
            Issue.record("expected a failure")
        } catch let error as APIError {
            guard case .problem(let problem) = error else {
                Issue.record("expected .problem, got \(error)")
                return
            }
            #expect(problem.status == 403)
            #expect(problem.code == "ADMISSION_DENIED")
            #expect(error.requestID == "req_abc123", "the correlation id must survive to the caller")
            #expect(!error.isRetryable)
        }
    }

    /// A non-2xx that is not a problem document usually means a proxy answered
    /// instead of the API. That is a different diagnosis and stays distinct.
    @Test func aNonProblemErrorStatusStaysAnHTTPError() async throws {
        let client = try makeClient()
        respond(status: 502, contentType: "text/html", body: "<html>bad gateway</html>")

        do {
            _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))
            Issue.record("expected a failure")
        } catch let error as APIError {
            guard case .http(let status, let contentType, let preview) = error else {
                Issue.record("expected .http, got \(error)")
                return
            }
            #expect(status == 502)
            #expect(contentType == "text/html")
            #expect(preview.contains("bad gateway"))
            #expect(error.isRetryable, "a gateway error may well succeed on retry")
        }
    }

    @Test func problemDocumentsWithServerErrorStatusAreRetryable() async throws {
        let client = try makeClient()
        respond(
            status: 503,
            contentType: "application/problem+json",
            body: #"{"type":"about:blank","title":"Unavailable","status":503,"requestId":"req_x"}"#
        )

        do {
            _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))
            Issue.record("expected a failure")
        } catch let error as APIError {
            #expect(error.isRetryable)
        }
    }

    // MARK: Transport failures

    @Test func transportFailuresAreClassified() async throws {
        let cases: [(URLError.Code, TransportFailure)] = [
            (.notConnectedToInternet, .offline),
            (.timedOut, .timedOut),
            (.cannotConnectToHost, .cannotConnect),
            (.secureConnectionFailed, .tls),
            (.cancelled, .cancelled),
        ]

        for (code, expected) in cases {
            let client = try makeClient()
            var stub = StubURLProtocol.Stub()
            stub.error = URLError(code)
            StubURLProtocol.stub = stub

            await #expect(throws: APIError.transport(expected)) {
                _ = try await client.send(APIRequest<SampleResource>(path: "services/bouquets"))
            }
        }
    }

    @Test func cancellationIsNotRetryableButOtherTransportFailuresAre() {
        #expect(!APIError.transport(.cancelled).isRetryable)
        #expect(APIError.transport(.timedOut).isRetryable)
        #expect(APIError.transport(.offline).isRetryable)
    }
}
