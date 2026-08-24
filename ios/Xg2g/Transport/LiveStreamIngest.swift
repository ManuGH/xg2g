// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// An open byte stream, handed to whoever demuxes it.
///
/// ## Why the decoder no longer owns its own socket
///
/// `NativeTSVideoPipeline` used to be a `URLSessionDataDelegate`: it built the
/// request, chose the cache policy and the timeouts, and set the client-identity
/// headers — the app's User-Agent, and the codec lists the backend planner reads
/// as evidence. A change to how this client identifies itself therefore meant
/// editing a transport-stream demuxer.
///
/// Splitting it leaves the pipeline with one job — bytes in, pictures out — and
/// puts every network decision behind this type. The pipeline never sees a URL
/// it did not receive, never names a header, and cannot be the place a new one
/// is added.
protocol LiveStreamIngestDelegate: AnyObject {
    /// The response headers arrived. Return `false` to abandon the stream.
    func ingest(_ ingest: LiveStreamIngest, didReceiveResponse response: HTTPURLResponse) -> Bool
    func ingest(_ ingest: LiveStreamIngest, didReceive data: Data)
    /// The stream ended. `error` is `nil` for a clean end, and carries
    /// `NSURLErrorCancelled` when the app stopped it.
    func ingest(_ ingest: LiveStreamIngest, didCompleteWith error: Error?, bytesReceived: Int64)
    /// Connection setup broken down by phase, once the task has finished.
    func ingest(_ ingest: LiveStreamIngest, didCollect phases: [LiveStreamIngest.ConnectionPhases])
}

/// One HTTP byte stream: the transport-stream ingest the native player reads.
final class LiveStreamIngest: NSObject, URLSessionDataDelegate {

    /// One transaction's setup cost, split into the phases that have different
    /// causes and different fixes.
    ///
    /// Request-to-first-byte as a single number said the setup cost 769 ms and
    /// nothing about where it went.
    struct ConnectionPhases: Sendable {
        let dnsMs: Double?
        let tcpMs: Double?
        let tlsMs: Double?
        let requestMs: Double?
        let serverThinkMs: Double?
        let totalMs: Double?
        let networkProtocol: String?
        let reusedConnection: Bool
        let cellular: Bool
        let host: String?
    }

    /// How this client identifies itself and what it can decode.
    ///
    /// A value rather than literals at the call site: the identity travels as
    /// data through one place that knows it is a header, which is what stops the
    /// next codec from being announced from inside a decoder.
    struct ClientEvidence: Sendable {
        let userAgent: String
        let audioCodecs: [String]
        let videoCodecs: [String]

        static let nativePlayer = ClientEvidence(
            userAgent: "xg2g-ios-native/1.0",
            audioCodecs: ["aac", "ac3", "eac3"],
            videoCodecs: ["h264", "hevc"]
        )
    }

    private weak var delegate: LiveStreamIngestDelegate?
    private var session: URLSession?
    private var task: URLSessionDataTask?

    /// Whether a stream is currently open.
    var isOpen: Bool { task != nil }

    init(delegate: LiveStreamIngestDelegate) {
        self.delegate = delegate
        super.init()
    }

    /// Opens `url` and starts delivering bytes.
    ///
    /// - Parameters:
    ///   - correlationID: stamped on the request so one channel change can be
    ///     followed across both sides of the wire. Logged as a field, never as a
    ///     metric label, so it only has to be unique within this app run.
    func open(
        url: URL,
        correlationID: String?,
        evidence: ClientEvidence = .nativePlayer
    ) {
        close()

        let config = URLSessionConfiguration.ephemeral
        config.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        config.timeoutIntervalForRequest = 10.0
        config.waitsForConnectivity = false
        config.allowsCellularAccess = true
        config.allowsExpensiveNetworkAccess = true
        config.allowsConstrainedNetworkAccess = true
        config.httpShouldUsePipelining = true

        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 1
        queue.qualityOfService = .userInteractive

        let session = URLSession(configuration: config, delegate: self, delegateQueue: queue)
        self.session = session

        var request = URLRequest(url: url)
        request.setValue(evidence.userAgent, forHTTPHeaderField: "User-Agent")
        if let correlationID {
            request.setValue(correlationID, forHTTPHeaderField: "X-Xg2g-Zap-Id")
        }
        request.setValue(evidence.audioCodecs.joined(separator: ","), forHTTPHeaderField: "X-Client-Audio-Codecs")
        request.setValue(evidence.videoCodecs.joined(separator: ","), forHTTPHeaderField: "X-Client-Video-Codecs")

        let task = session.dataTask(with: request)
        self.task = task
        task.resume()
    }

    /// Cancels the stream and tears the session down. Idempotent.
    func close() {
        task?.cancel()
        task = nil
        session?.invalidateAndCancel()
        session = nil
    }

    // MARK: - URLSessionDataDelegate

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        guard let http = response as? HTTPURLResponse else {
            completionHandler(.allow)
            return
        }
        let accepted = delegate?.ingest(self, didReceiveResponse: http) ?? false
        completionHandler(accepted ? .allow : .cancel)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        delegate?.ingest(self, didReceive: data)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        delegate?.ingest(self, didCompleteWith: error, bytesReceived: task.countOfBytesReceived)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didFinishCollecting metrics: URLSessionTaskMetrics) {
        let phases = metrics.transactionMetrics.map { transaction -> ConnectionPhases in
            func ms(_ from: Date?, _ to: Date?) -> Double? {
                guard let from, let to else { return nil }
                return to.timeIntervalSince(from) * 1000.0
            }
            return ConnectionPhases(
                dnsMs: ms(transaction.domainLookupStartDate, transaction.domainLookupEndDate),
                tcpMs: ms(transaction.connectStartDate, transaction.connectEndDate),
                tlsMs: ms(transaction.secureConnectionStartDate, transaction.secureConnectionEndDate),
                requestMs: ms(transaction.requestStartDate, transaction.requestEndDate),
                serverThinkMs: ms(transaction.requestEndDate, transaction.responseStartDate),
                totalMs: ms(transaction.fetchStartDate, transaction.responseStartDate),
                networkProtocol: transaction.networkProtocolName,
                reusedConnection: transaction.isReusedConnection,
                cellular: transaction.isCellular,
                host: transaction.request.url?.host
            )
        }
        delegate?.ingest(self, didCollect: phases)
    }
}
