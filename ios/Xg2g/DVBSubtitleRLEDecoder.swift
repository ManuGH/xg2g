// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Helper bit-stream reader for variable-length DVB Subtitle RLE bitstreams.
public struct DVBBitReader {
    private let bytes: [UInt8]
    private var byteOffset: Int = 0
    private var bitOffset: Int = 0 // 0 to 7 (MSB to LSB)

    public init(data: Data) {
        self.bytes = [UInt8](data)
    }

    public var isEOF: Bool {
        byteOffset >= bytes.count
    }

    public var bitsRemaining: Int {
        if byteOffset >= bytes.count { return 0 }
        return (bytes.count - byteOffset) * 8 - bitOffset
    }

    public var bytesRead: Int {
        if bitOffset == 0 {
            return byteOffset
        } else {
            return byteOffset + 1
        }
    }

    /// Reads up to 32 bits from the bitstream.
    public mutating func readBits(_ count: Int) -> UInt32? {
        guard count > 0 && count <= 32 else { return nil }
        guard bitsRemaining >= count else { return nil }

        var result: UInt32 = 0
        var remaining = count

        while remaining > 0 {
            let bitsAvailableInByte = 8 - bitOffset
            let bitsToTake = min(remaining, bitsAvailableInByte)
            let shift = bitsAvailableInByte - bitsToTake
            let mask = UInt8((1 << bitsToTake) - 1)
            let chunk = (bytes[byteOffset] >> shift) & mask

            result = (result << bitsToTake) | UInt32(chunk)
            remaining -= bitsToTake
            bitOffset += bitsToTake

            if bitOffset == 8 {
                bitOffset = 0
                byteOffset += 1
            }
        }

        return result
    }

    /// Aligns bit reader to the next full byte boundary.
    public mutating func alignToByte() {
        if bitOffset != 0 {
            bitOffset = 0
            byteOffset += 1
        }
    }
}

/// Decodes DVB Subtitle Run-Length Encoded (RLE) bitstreams into raw pixel index rasters (ETSI EN 300 743 Clause 5.2.5.2 & Annex B).
public final class DVBSubtitleRLEDecoder: @unchecked Sendable {

    public init() {}

    /// Decodes an object's top and bottom field bitstreams into a 2D pixel index raster (width x height).
    ///
    /// - Parameters:
    ///   - objectData: The raw object data segment containing field bitstreams.
    ///   - width: Region width in pixels.
    ///   - height: Region height in pixels.
    ///   - depth: Target color depth (1 = 2-bit, 2 = 4-bit, 3 = 8-bit).
    /// - Returns: A 1D array of length `width * height` containing color index values for each pixel.
    public func decode(
        objectData: DVBObjectDataSegment,
        width: Int,
        height: Int,
        depth: UInt8
    ) -> [UInt8] {
        guard width > 0 && height > 0 else { return [] }
        var raster = [UInt8](repeating: 0, count: width * height)

        let topLines = decodeField(data: objectData.topFieldData, width: width, depth: depth)

        if let bottomData = objectData.bottomFieldData, !bottomData.isEmpty {
            // Interlaced mode: Top field fills even lines (0, 2, 4...), Bottom field fills odd lines (1, 3, 5...)
            let bottomLines = decodeField(data: bottomData, width: width, depth: depth)

            for (lineIdx, line) in topLines.enumerated() {
                let rasterY = lineIdx * 2
                guard rasterY < height else { break }
                let start = rasterY * width
                let copyCount = min(line.count, width)
                raster.replaceSubrange(start..<(start + copyCount), with: line.prefix(copyCount))
            }

            for (lineIdx, line) in bottomLines.enumerated() {
                let rasterY = lineIdx * 2 + 1
                guard rasterY < height else { break }
                let start = rasterY * width
                let copyCount = min(line.count, width)
                raster.replaceSubrange(start..<(start + copyCount), with: line.prefix(copyCount))
            }
        } else {
            // Progressive / Non-interlaced mode: Top field lines are doubled or mapped linearly
            for (lineIdx, line) in topLines.enumerated() {
                let rasterY0 = lineIdx * 2
                let rasterY1 = rasterY0 + 1

                let copyCount = min(line.count, width)

                if rasterY0 < height {
                    let start0 = rasterY0 * width
                    raster.replaceSubrange(start0..<(start0 + copyCount), with: line.prefix(copyCount))
                }
                if rasterY1 < height {
                    let start1 = rasterY1 * width
                    raster.replaceSubrange(start1..<(start1 + copyCount), with: line.prefix(copyCount))
                }
            }
        }

        return raster
    }

    /// Decodes a single field's pixel data sub-blocks into a list of scanlines.
    public func decodeField(data: Data, width: Int, depth: UInt8) -> [[UInt8]] {
        var lines: [[UInt8]] = []
        var offset = 0

        while offset < data.count {
            let dataType = data[offset]
            offset += 1

            switch dataType {
            case 0x10: // 2-bit/pixel pixel-code string (Table 9)
                let remainingData = data.dropFirst(offset)
                let (decodedLines, consumedBytes) = decode2BitLines(data: remainingData, width: width)
                lines.append(contentsOf: decodedLines)
                offset += max(consumedBytes, 1)

            case 0x11: // 4-bit/pixel pixel-code string (Table 10)
                let remainingData = data.dropFirst(offset)
                let (decodedLines, consumedBytes) = decode4BitLines(data: remainingData, width: width)
                lines.append(contentsOf: decodedLines)
                offset += max(consumedBytes, 1)

            case 0x12: // 8-bit/pixel pixel-code string (Table 11)
                let remainingData = data.dropFirst(offset)
                let (decodedLines, consumedBytes) = decode8BitLines(data: remainingData, width: width)
                lines.append(contentsOf: decodedLines)
                offset += max(consumedBytes, 1)

            case 0x20: // 2-to-4-bit map-table
                offset += 4
            case 0x21: // 2-to-8-bit map-table
                offset += 8
            case 0x22: // 4-to-8-bit map-table
                offset += 16

            case 0xF0: // End of object line code
                break

            default:
                break
            }
        }

        return lines
    }

    // MARK: - 2-Bit/Pixel RLE Decoder (ETSI EN 300 743 Table 9)

    private func decode2BitLines(data: Data, width: Int) -> ([[UInt8]], Int) {
        var reader = DVBBitReader(data: data)
        var lines: [[UInt8]] = []
        var currentLine: [UInt8] = []

        while !reader.isEOF {
            guard let code = reader.readBits(2) else { break }

            if code != 0x00 {
                // Non-zero 2-bit pixel
                currentLine.append(UInt8(code))
            } else {
                // Escape code starting with '00'
                guard let flag1 = reader.readBits(1) else { break }

                if flag1 == 1 {
                    // '00 1': 3-bit run of color 0
                    guard let run = reader.readBits(3) else { break }
                    currentLine.append(contentsOf: repeatElement(0, count: Int(run)))
                } else {
                    // '00 0':
                    guard let flag2 = reader.readBits(1) else { break }

                    if flag2 == 1 {
                        // '00 0 1': 2-bit run length (3..10), 2-bit color
                        guard let runBits = reader.readBits(2),
                              let color = reader.readBits(2) else { break }
                        let run = Int(runBits) + 3
                        currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))
                    } else {
                        // '00 0 0':
                        guard let subCode = reader.readBits(2) else { break }

                        switch subCode {
                        case 0x00:
                            // '00 0 0 00': End of line
                            if !currentLine.isEmpty {
                                lines.append(currentLine)
                                currentLine.removeAll(keepingCapacity: true)
                            }
                            reader.alignToByte()

                        case 0x01:
                            // '00 0 0 01': 2 pixels of color 0
                            currentLine.append(contentsOf: [0, 0])

                        case 0x02:
                            // '00 0 0 10': 4-bit run length (12..27), 2-bit color
                            guard let runBits = reader.readBits(4),
                                  let color = reader.readBits(2) else { break }
                            let run = Int(runBits) + 12
                            currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))

                        case 0x03:
                            // '00 0 0 11': 8-bit run length (29..284), 2-bit color
                            guard let runBits = reader.readBits(8),
                                  let color = reader.readBits(2) else { break }
                            let run = Int(runBits) + 29
                            currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))

                        default:
                            break
                        }
                    }
                }
            }

            if currentLine.count >= width {
                lines.append(currentLine)
                currentLine.removeAll(keepingCapacity: true)
                reader.alignToByte()
            }
        }

        if !currentLine.isEmpty {
            lines.append(currentLine)
        }

        return (lines, reader.bytesRead)
    }

    // MARK: - 4-Bit/Pixel RLE Decoder (ETSI EN 300 743 Table 10)

    private func decode4BitLines(data: Data, width: Int) -> ([[UInt8]], Int) {
        var reader = DVBBitReader(data: data)
        var lines: [[UInt8]] = []
        var currentLine: [UInt8] = []

        while !reader.isEOF {
            guard let code = reader.readBits(4) else { break }

            if code != 0x00 {
                // Non-zero 4-bit pixel
                currentLine.append(UInt8(code))
            } else {
                // Escape code starting with '0000'
                guard let flag1 = reader.readBits(1) else { break }

                if flag1 == 0 {
                    // '0000 0':
                    guard let runBits = reader.readBits(3) else { break }
                    if runBits != 0 {
                        // 1..7 pixels of color 0
                        currentLine.append(contentsOf: repeatElement(0, count: Int(runBits)))
                    } else {
                        // '0000 0 000': End of line
                        if !currentLine.isEmpty {
                            lines.append(currentLine)
                            currentLine.removeAll(keepingCapacity: true)
                        }
                        reader.alignToByte()
                    }
                } else {
                    // '0000 1':
                    guard let flag2 = reader.readBits(1) else { break }

                    if flag2 == 0 {
                        // '0000 1 0': 2-bit run length (4..7), 4-bit color
                        guard let runBits = reader.readBits(2),
                              let color = reader.readBits(4) else { break }
                        let run = Int(runBits) + 4
                        currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))
                    } else {
                        // '0000 1 1':
                        guard let flag3 = reader.readBits(1) else { break }

                        if flag3 == 0 {
                            // '0000 1 1 0': 4-bit run length (9..24), 4-bit color
                            guard let runBits = reader.readBits(4),
                                  let color = reader.readBits(4) else { break }
                            let run = Int(runBits) + 9
                            currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))
                        } else {
                            // '0000 1 1 1':
                            guard let flag4 = reader.readBits(1) else { break }

                            if flag4 == 0 {
                                // '0000 1 1 1 0': 2 pixels of color 0
                                currentLine.append(contentsOf: [0, 0])
                            } else {
                                // '0000 1 1 1 1': 8-bit run length (25..280), 4-bit color
                                guard let runBits = reader.readBits(8),
                                      let color = reader.readBits(4) else { break }
                                let run = Int(runBits) + 25
                                currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))
                            }
                        }
                    }
                }
            }

            if currentLine.count >= width {
                lines.append(currentLine)
                currentLine.removeAll(keepingCapacity: true)
                reader.alignToByte()
            }
        }

        if !currentLine.isEmpty {
            lines.append(currentLine)
        }

        return (lines, reader.bytesRead)
    }

    // MARK: - 8-Bit/Pixel RLE Decoder (ETSI EN 300 743 Table 11)

    private func decode8BitLines(data: Data, width: Int) -> ([[UInt8]], Int) {
        var reader = DVBBitReader(data: data)
        var lines: [[UInt8]] = []
        var currentLine: [UInt8] = []

        while !reader.isEOF {
            guard let code = reader.readBits(8) else { break }

            if code != 0x00 {
                // Non-zero 8-bit pixel
                currentLine.append(UInt8(code))
            } else {
                // Escape code starting with '00000000'
                guard let flag = reader.readBits(1) else { break }

                if flag == 0 {
                    // '00000000 0':
                    guard let runBits = reader.readBits(7) else { break }
                    if runBits != 0 {
                        // 1..127 pixels of color 0
                        currentLine.append(contentsOf: repeatElement(0, count: Int(runBits)))
                    } else {
                        // '00000000 0 0000000': End of line
                        if !currentLine.isEmpty {
                            lines.append(currentLine)
                            currentLine.removeAll(keepingCapacity: true)
                        }
                        reader.alignToByte()
                    }
                } else {
                    // '00000000 1': 7-bit run length (4..127), 8-bit color
                    guard let runBits = reader.readBits(7),
                          let color = reader.readBits(8) else { break }
                    let run = Int(runBits)
                    currentLine.append(contentsOf: repeatElement(UInt8(color), count: run))
                }
            }

            if currentLine.count >= width {
                lines.append(currentLine)
                currentLine.removeAll(keepingCapacity: true)
                reader.alignToByte()
            }
        }

        if !currentLine.isEmpty {
            lines.append(currentLine)
        }

        return (lines, reader.bytesRead)
    }
}
