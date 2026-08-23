// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import Foundation
import Testing
@testable import Xg2g

struct DVBSubtitleRLETests {

    @Test("2-bit/pixel RLE: Decodes single pixels, zero runs, color runs, and EOL")
    func decode2BitRLEBitstream() {
        let decoder = DVBSubtitleRLEDecoder()

        // 2-bit RLE encoding for a 10-pixel line:
        // - Color 1 (1 pixel): '01'
        // - Color 2 (1 pixel): '10'
        // - Color 0 run of 4 (4 pixels): '00 1 100' (run = 4)
        // - Color 3 run of 3 (3 pixels): '00 0 1 00 11' (runBits = 00 -> 3, color = 11)
        // - Color 1 (1 pixel): '01'
        // Total = 1 + 1 + 4 + 3 + 1 = 10 pixels
        // Bitstream:
        // '01' '10' '0011' '0000' '0100' '1101' '0000' '00' (EOL)
        // Byte 0: 01 10 00 11 = 0x63
        // Byte 1: 00 00 01 00 = 0x04
        // Byte 2: 11 01 00 00 = 0xD0 (color 3 run 3 + color 1 + EOL '00 0 0 00')
        // Byte 3: 00 00 00 00 = 0x00

        var bitstream = Data([0x10]) // Data type 0x10 = 2-bit/pixel
        bitstream.append(contentsOf: [0x63, 0x04, 0xD0, 0x00])

        let lines = decoder.decodeField(data: bitstream, width: 10, depth: 1)
        #expect(lines.count == 1)

        let line = lines[0]
        #expect(line.count == 10)
        #expect(line[0] == 1)
        #expect(line[1] == 2)
        #expect(Array(line[2...5]) == [0, 0, 0, 0])
        #expect(Array(line[6...8]) == [3, 3, 3])
        #expect(line[9] == 1)
    }

    @Test("4-bit/pixel RLE: Decodes non-zero pixels, zero runs, 4-bit color runs, and EOL")
    func decode4BitRLEBitstream() {
        let decoder = DVBSubtitleRLEDecoder()

        // 4-bit RLE encoding for an 8-pixel line:
        // - Pixel 1: color 5 -> '0101'
        // - Run of 3 zeros -> '0000 0 011'
        // - Run of 4 color 9 -> '0000 1 0 00 1001' (runBits=00 -> 4, color=9)
        // Total = 1 + 3 + 4 = 8 pixels
        // Bitstream:
        // '0101' '0000' '0011' '0000' '1000' '1001' '0000' '0000' (EOL '0000 0 000')
        // Byte 0: 0101 0000 = 0x50
        // Byte 1: 0011 0000 = 0x30
        // Byte 2: 1000 1001 = 0x89
        // Byte 3: 0000 0000 = 0x00 (EOL)

        var bitstream = Data([0x11]) // Data type 0x11 = 4-bit/pixel
        bitstream.append(contentsOf: [0x50, 0x30, 0x89, 0x00])

        let lines = decoder.decodeField(data: bitstream, width: 8, depth: 2)
        #expect(lines.count == 1)

        let line = lines[0]
        #expect(line.count == 8)
        #expect(line[0] == 5)
        #expect(Array(line[1...3]) == [0, 0, 0])
        #expect(Array(line[4...7]) == [9, 9, 9, 9])
    }

    @Test("8-bit/pixel RLE: Decodes 8-bit color strings and zero runs")
    func decode8BitRLEBitstream() {
        let decoder = DVBSubtitleRLEDecoder()

        // 8-bit RLE for a 6-pixel line:
        // - Color 42 -> 0x2A
        // - Run of 3 zeros -> '00000000 0 0000011' (0x00, 0x03)
        // - Color 100 -> 0x64
        // - Color 200 -> 0xC8
        // Total = 1 + 3 + 1 + 1 = 6 pixels
        // Bitstream:
        // Byte 0: 0x2A
        // Byte 1: 0x00
        // Byte 2: 0x03 (run of 3 zeros)
        // Byte 3: 0x64
        // Byte 4: 0xC8
        // Byte 5: 0x00
        // Byte 6: 0x00 (EOL '00000000 0 0000000')

        var bitstream = Data([0x12]) // Data type 0x12 = 8-bit/pixel
        bitstream.append(contentsOf: [0x2A, 0x00, 0x03, 0x64, 0xC8, 0x00, 0x00])

        let lines = decoder.decodeField(data: bitstream, width: 6, depth: 3)
        #expect(lines.count == 1)

        let line = lines[0]
        #expect(line.count == 6)
        #expect(line[0] == 42)
        #expect(Array(line[1...3]) == [0, 0, 0])
        #expect(line[4] == 100)
        #expect(line[5] == 200)
    }

    @Test("Interlaced vs Progressive: Top and bottom fields assemble into interlaced raster")
    func interlacedFieldRasterAssembly() {
        let decoder = DVBSubtitleRLEDecoder()

        // 4x4 image:
        // Top field has 2 lines: Line 0 [1, 1, 1, 1], Line 2 [2, 2, 2, 2]
        // Bottom field has 2 lines: Line 1 [3, 3, 3, 3], Line 3 [4, 4, 4, 4]
        var topData = Data([0x10])
        topData.append(contentsOf: [0x55, 0x00, 0x00, 0xAA, 0x00, 0x00]) // Color 1x4 + EOL, Color 2x4 + EOL

        var bottomData = Data([0x10])
        bottomData.append(contentsOf: [0xFF, 0x00, 0x00, 0x63, 0x04, 0x00]) // Color 3x4 + EOL, etc.

        let ods = DVBObjectDataSegment(
            objectID: 1,
            versionNumber: 1,
            codingMethod: .pixels,
            nonModifyingColorFlag: false,
            topFieldData: topData,
            bottomFieldData: bottomData
        )

        let raster = decoder.decode(objectData: ods, width: 4, height: 4, depth: 1)
        #expect(raster.count == 16)

        // Line 0 (even) from Top Field: [1, 1, 1, 1]
        #expect(Array(raster[0...3]) == [1, 1, 1, 1])
        // Line 1 (odd) from Bottom Field: [3, 3, 3, 3]
        #expect(Array(raster[4...7]) == [3, 3, 3, 3])
    }

    @Test("CLUT Converter: ITU-R BT.601 YCbCr + Transparency conversion to 32-bit RGBA")
    func clutYCbCrToRGBAConversion() {
        let converter = DVBSubtitleCLUTConverter()

        // Create CLUT entries:
        // Entry 0: Transparent Black (Y=0, T=255) -> RGBA [0, 0, 0, 0]
        // Entry 1: Pure White Opaque (Y=235, Cr=128, Cb=128, T=0) -> RGBA [235, 235, 235, 255]
        // Entry 2: Yellow Opaque (Y=210, Cr=146, Cb=16, T=0) -> RGBA [235, 236, 12, 255]
        let entries = [
            DVBCLUTEntry(entryID: 0, entry2BitCLUTFlag: true, entry4BitCLUTFlag: true, entry8BitCLUTFlag: true, y: 0, cr: 128, cb: 128, t: 255),
            DVBCLUTEntry(entryID: 1, entry2BitCLUTFlag: true, entry4BitCLUTFlag: true, entry8BitCLUTFlag: true, y: 235, cr: 128, cb: 128, t: 0),
            DVBCLUTEntry(entryID: 2, entry2BitCLUTFlag: true, entry4BitCLUTFlag: true, entry8BitCLUTFlag: true, y: 210, cr: 146, cb: 16, t: 0)
        ]
        let clut = DVBCLUTDefinitionSegment(clutID: 1, versionNumber: 1, entries: entries)

        // 3-pixel raster: [0, 1, 2]
        let raster: [UInt8] = [0, 1, 2]
        let rgbaData = converter.convertToRGBA(raster: raster, clut: clut, width: 3, height: 1)

        #expect(rgbaData.count == 12) // 3 pixels * 4 bytes

        // Pixel 0: Transparent (A=0)
        #expect(rgbaData[3] == 0)

        // Pixel 1: White Opaque (R=235, G=235, B=235, A=255)
        #expect(rgbaData[4] == 235)
        #expect(rgbaData[5] == 235)
        #expect(rgbaData[6] == 235)
        #expect(rgbaData[7] == 255)

        // Pixel 2: Yellow Opaque (R=235, G=236, B=12, A=255)
        #expect(rgbaData[8] == 235)
        #expect(rgbaData[9] == 236)
        #expect(rgbaData[10] == 12)
        #expect(rgbaData[11] == 255)
    }

    @Test("CGImage Creation: Generates a valid CGImage from raster and CLUT")
    func cgImageCreationFromRaster() {
        let converter = DVBSubtitleCLUTConverter()

        let entries = [
            DVBCLUTEntry(entryID: 0, entry2BitCLUTFlag: true, entry4BitCLUTFlag: true, entry8BitCLUTFlag: true, y: 0, cr: 128, cb: 128, t: 255),
            DVBCLUTEntry(entryID: 1, entry2BitCLUTFlag: true, entry4BitCLUTFlag: true, entry8BitCLUTFlag: true, y: 235, cr: 128, cb: 128, t: 0)
        ]
        let clut = DVBCLUTDefinitionSegment(clutID: 1, versionNumber: 1, entries: entries)

        // 4x2 raster
        let raster: [UInt8] = [
            1, 1, 1, 1,
            0, 1, 1, 0
        ]

        let image = converter.createCGImage(raster: raster, clut: clut, width: 4, height: 2)
        #expect(image != nil)
        #expect(image?.width == 4)
        #expect(image?.height == 2)
        #expect(image?.bitsPerPixel == 32)
    }
}
