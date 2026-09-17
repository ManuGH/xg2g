// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing

@testable import Xg2g

/// The ring is the part that has to be right before anything is built on it: a
/// seek that lands on overwritten bytes produces a picture assembled from
/// whatever happened to be there, which is worse than refusing the seek.
@Suite struct TimeshiftBufferTests {

    private func makeBuffer(capacity: Int64) throws -> TimeshiftBuffer {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("timeshift-test-\(capacity)-\(UUID().uuidString).ts")
        return try TimeshiftBuffer(fileURL: url, capacity: capacity)
    }

    private func bytes(_ pattern: UInt8, _ count: Int) -> Data {
        Data(repeating: pattern, count: count)
    }

    @Test func readsBackWhatWasWritten() throws {
        let buffer = try makeBuffer(capacity: 1000)
        try buffer.append(bytes(0xAA, 300))
        try buffer.append(bytes(0xBB, 200))

        #expect(buffer.writtenBytes == 500)
        #expect(try buffer.read(from: 0, maxBytes: 300) == bytes(0xAA, 300))
        #expect(try buffer.read(from: 300, maxBytes: 200) == bytes(0xBB, 200))
    }

    @Test func wrappingKeepsTheNewestBytesAndDropsTheOldest() throws {
        let buffer = try makeBuffer(capacity: 100)
        try buffer.append(bytes(0x11, 80))
        try buffer.append(bytes(0x22, 60))   // wraps: 40 bytes overwrite the head

        #expect(buffer.writtenBytes == 140)
        #expect(buffer.earliestOffset == 40)
        // What survives of the first write is its tail.
        #expect(try buffer.read(from: 40, maxBytes: 40) == bytes(0x11, 40))
        #expect(try buffer.read(from: 80, maxBytes: 60) == bytes(0x22, 60))
    }

    @Test func aWriteAcrossTheSeamIsReadBackWhole() throws {
        let buffer = try makeBuffer(capacity: 100)
        try buffer.append(bytes(0x01, 90))
        var straddling = bytes(0x02, 5)
        straddling.append(bytes(0x03, 5))
        try buffer.append(straddling)        // 5 bytes before the seam, 5 after

        #expect(try buffer.read(from: 90, maxBytes: 10) == straddling)
    }

    @Test func readingOverwrittenBytesIsRefusedRatherThanGuessed() throws {
        let buffer = try makeBuffer(capacity: 100)
        try buffer.append(bytes(0x11, 150))

        #expect(throws: TimeshiftBuffer.BufferError.self) {
            _ = try buffer.read(from: 0, maxBytes: 10)
        }
    }

    @Test func aWriteLargerThanTheWindowKeepsOnlyItsTail() throws {
        let buffer = try makeBuffer(capacity: 100)
        var big = bytes(0x77, 150)
        big.append(bytes(0x88, 50))          // 200 bytes into a 100-byte window
        try buffer.append(big)

        #expect(buffer.writtenBytes == 200)
        #expect(buffer.earliestOffset == 100)
        #expect(try buffer.read(from: 150, maxBytes: 50) == bytes(0x88, 50))
    }

    // MARK: - Seek points

    private func pts(_ seconds: Double) -> CMTime {
        CMTime(seconds: seconds, preferredTimescale: 90000)
    }

    @Test func seekLandsOnTheNewestPointAtOrBeforeTheTarget() throws {
        let buffer = try makeBuffer(capacity: 10_000)
        for i in 0..<10 {
            try buffer.append(bytes(UInt8(i), 100))
            buffer.noteRandomAccessPoint(pts: pts(Double(i)))
        }

        // Points sit at 0…9 s; live is 9 s, so 3.5 s back is 5.5 s -> the 5 s point.
        let point = buffer.seekPoint(secondsBack: 3.5)
        #expect(point?.pts.seconds == 5.0)
    }

    @Test func seekingFurtherBackThanTheWindowLandsOnTheOldestPointHeld() throws {
        let buffer = try makeBuffer(capacity: 10_000)
        for i in 0..<5 {
            try buffer.append(bytes(UInt8(i), 100))
            buffer.noteRandomAccessPoint(pts: pts(Double(i)))
        }

        let point = buffer.seekPoint(secondsBack: 600)
        #expect(point?.pts.seconds == 0.0)
    }

    @Test func pointsThatTheRingHasPassedAreForgotten() throws {
        let buffer = try makeBuffer(capacity: 500)
        for i in 0..<10 {
            buffer.noteRandomAccessPoint(pts: pts(Double(i)))
            try buffer.append(bytes(UInt8(i), 100))
        }

        // 1000 bytes through a 500-byte window: everything below offset 500 is gone.
        #expect(buffer.earliestOffset == 500)
        #expect(buffer.seekPoints.allSatisfy { $0.offset >= 500 })
        #expect(!buffer.seekPoints.isEmpty)
    }

    @Test func aReAnchoredTimelineDiscardsThePointsBehindIt() throws {
        let buffer = try makeBuffer(capacity: 10_000)
        try buffer.append(bytes(0x00, 50))
        buffer.noteRandomAccessPoint(pts: pts(99))    // offset 50
        try buffer.append(bytes(0x01, 50))
        buffer.noteRandomAccessPoint(pts: pts(100))   // offset 100
        try buffer.append(bytes(0x02, 100))
        buffer.noteRandomAccessPoint(pts: pts(101))   // offset 200

        // A discontinuity re-anchors: the same position arrives again carrying a
        // new timeline, and everything indexed at or after it describes the old
        // one. What lies before it is still good.
        buffer.noteRandomAccessPoint(pts: pts(0), atOffset: 100)

        #expect(buffer.seekPoints.count == 2)
        #expect(buffer.seekPoints.first?.pts.seconds == 99.0)
        #expect(buffer.seekPoints.last?.pts.seconds == 0.0)
    }

    @Test func anEmptyBufferHasNowhereToSeekTo() throws {
        let buffer = try makeBuffer(capacity: 100)
        #expect(buffer.seekPoint(secondsBack: 5) == nil)
        #expect(buffer.availableSeconds == 0)
    }
}
