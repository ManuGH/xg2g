// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// A rolling on-disk copy of the transport stream, with an index of the points
/// playback can be restarted from.
///
/// The native path takes its stream straight from the receiver, which is the
/// whole reason it exists — no transcode, no segmenting, no server in the
/// middle. Pausing live television needs somewhere to put the part that has
/// already gone past, and the backend's DVR cannot be that place: its window is
/// produced per session by ffmpeg and begins when the viewer tunes, so it
/// reaches no further back than a local copy would while costing a transcode on
/// the receiver we already know delivers in bursts.
///
/// Written as a circular file rather than an ever-growing one. A live 720p50
/// service measures 12.5 Mbps, which is 5.6 GB an hour — a window has to be a
/// hard ceiling, not a hope, and reclaiming space by rewriting a file from the
/// front costs more than the stream earns.
///
/// Offsets are absolute stream positions and keep counting past the wrap. The
/// file position is `offset % capacity`; anything below `earliestOffset` has
/// been overwritten and is gone.
public final class TimeshiftBuffer {

    /// A place playback can start: a random access point and the time it carries.
    ///
    /// Seeking anywhere else in H.264 gives a picture built from references that
    /// were never received, which is the artefact the decode gate exists to
    /// prevent — the same rule applies to a seek as to a tune.
    public struct SeekPoint: Equatable, Sendable {
        public let offset: Int64
        public let pts: CMTime
    }

    public enum BufferError: Error {
        case notOpen
        case offsetOverwritten(requested: Int64, earliest: Int64)
    }

    /// Absolute position one byte past the newest byte written.
    public private(set) var writtenBytes: Int64 = 0

    /// The oldest position still on disk. Below this the ring has wrapped over it.
    public var earliestOffset: Int64 { max(0, writtenBytes - capacity) }

    public let capacity: Int64

    /// Random access points still inside the window, oldest first.
    public private(set) var seekPoints: [SeekPoint] = []

    private let handle: FileHandle
    private let fileURL: URL

    public init(fileURL: URL, capacity: Int64) throws {
        precondition(capacity > 0, "a window of nothing cannot hold anything")
        self.fileURL = fileURL
        self.capacity = capacity

        let fm = FileManager.default
        try? fm.removeItem(at: fileURL)
        try fm.createDirectory(
            at: fileURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        // Allocated up front so a full ring cannot fail on a phone that filled
        // up in the meantime: the failure to handle is at tune-in, not an hour
        // into a recording.
        guard fm.createFile(atPath: fileURL.path, contents: nil) else {
            throw BufferError.notOpen
        }
        self.handle = try FileHandle(forUpdating: fileURL)
        try handle.truncate(atOffset: UInt64(capacity))
    }

    deinit {
        try? handle.close()
        try? FileManager.default.removeItem(at: fileURL)
    }

    /// Appends stream bytes, wrapping at the end of the file.
    public func append(_ data: Data) throws {
        guard !data.isEmpty else { return }

        // More than a whole window in one go: only the tail can survive, and
        // writing the rest first would just overwrite it.
        var payload = data
        if Int64(payload.count) > capacity {
            payload = payload.suffix(Int(capacity))
            writtenBytes += Int64(data.count) - capacity
        }

        let start = writtenBytes % capacity
        let firstChunk = min(Int64(payload.count), capacity - start)

        try handle.seek(toOffset: UInt64(start))
        try handle.write(contentsOf: payload.prefix(Int(firstChunk)))

        if Int64(payload.count) > firstChunk {
            try handle.seek(toOffset: 0)
            try handle.write(contentsOf: payload.suffix(from: payload.startIndex + Int(firstChunk)))
        }

        writtenBytes += Int64(payload.count)
        pruneSeekPoints()
    }

    /// Records that a random access point begins at the current write position.
    ///
    /// Called by the parser when it emits a sync sample, which is the same test
    /// the decode gate applies — a point that can be decoded from cold.
    public func noteRandomAccessPoint(pts: CMTime, atOffset offset: Int64? = nil) {
        let position = offset ?? writtenBytes
        guard pts.isValid else { return }
        // Monotonic by construction; a repeat means the stream was re-anchored
        // and the older entries no longer describe this timeline.
        if let last = seekPoints.last, position <= last.offset {
            seekPoints.removeAll { $0.offset >= position }
        }
        seekPoints.append(SeekPoint(offset: position, pts: pts))
        pruneSeekPoints()
    }

    /// The newest random access point at least `seconds` behind live, or the
    /// oldest one still held if the window does not reach that far back.
    public func seekPoint(secondsBack seconds: Double) -> SeekPoint? {
        guard let newest = seekPoints.last else { return nil }
        let target = newest.pts.seconds - seconds
        // Newest first: the closest point at or before the target.
        if let match = seekPoints.last(where: { $0.pts.seconds <= target }) {
            return match
        }
        return seekPoints.first
    }

    /// Reads back from an absolute offset. Throws if the ring has passed it.
    public func read(from offset: Int64, maxBytes: Int) throws -> Data {
        guard offset >= earliestOffset, offset <= writtenBytes else {
            throw BufferError.offsetOverwritten(requested: offset, earliest: earliestOffset)
        }
        let available = min(Int64(maxBytes), writtenBytes - offset)
        guard available > 0 else { return Data() }

        let start = offset % capacity
        let firstChunk = min(available, capacity - start)

        try handle.seek(toOffset: UInt64(start))
        var out = try handle.read(upToCount: Int(firstChunk)) ?? Data()

        if available > firstChunk {
            try handle.seek(toOffset: 0)
            out.append(try handle.read(upToCount: Int(available - firstChunk)) ?? Data())
        }
        return out
    }

    /// How far back the buffer currently reaches, in stream time.
    public var availableSeconds: Double {
        guard let first = seekPoints.first, let last = seekPoints.last else { return 0 }
        return max(0, last.pts.seconds - first.pts.seconds)
    }

    private func pruneSeekPoints() {
        let floor = earliestOffset
        guard let firstKept = seekPoints.firstIndex(where: { $0.offset >= floor }) else {
            seekPoints.removeAll()
            return
        }
        if firstKept > 0 {
            seekPoints.removeFirst(firstKept)
        }
    }
}
