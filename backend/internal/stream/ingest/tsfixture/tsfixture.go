// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package tsfixture builds topology changes out of the real captures under
// testdata/segments, for tests that need an upstream whose PMT actually changes.
//
// The captures carry a single PMT version each, so none of them contains a change
// on its own. Several of them do differ in topology at identical PIDs, which is
// enough to build one at run time:
//
//	verify_final_v3.ts    H.264 on PID 256, AAC on PID 257
//	test_hevc_stream.ts   HEVC  on PID 256, AAC on PID 257
//	verify_seg.ts         H.264 on PID 256, MP2 on PID 257
//
// Concatenating two of them yields real elementary data on both sides of the cut,
// which a synthetic fixture could not. The second capture's PMT version has to be
// rewritten first: the ring decides a topology changed from the version number and
// the program number, and every capture uses version 0 for program 1. Without the
// bump the change would be invisible - which is itself worth knowing about spliced
// sources, even though spec-conformant DVB always increments.
package tsfixture

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// capturePMTPID is the PMT PID shared by every capture under testdata/segments.
const capturePMTPID = 4096

// Load reads a capture from testdata/segments. A missing capture skips the test
// rather than failing it: the files are large and not every checkout carries them.
func Load(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join(segmentsDir(t), name)
	data, err := os.ReadFile(path) // #nosec G304 -- test fixture path, not user input
	if err != nil {
		t.Skipf("skipping: capture %s not available: %v", name, err)
	}
	return data
}

// segmentsDir resolves testdata/segments from this file's own location, so it does
// not depend on which package's directory the test happens to run in.
func segmentsDir(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the tsfixture source path")
	}
	// .../backend/internal/stream/ingest/tsfixture/tsfixture.go -> .../backend
	backend := filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "..", ".."))
	return filepath.Join(backend, "testdata", "segments")
}

// BumpPMTVersion rewrites every complete PMT section in a capture to the given
// version, recomputing the section CRC. Sections that span packets are left alone;
// the captures repeat their PMT often enough that the single-packet ones suffice.
func BumpPMTVersion(t *testing.T, data []byte, version uint8) []byte {
	t.Helper()

	out := append([]byte(nil), data...)
	offset := -1
	for i := 0; i < ring.TSPacketSize && offset < 0; i++ {
		aligned := true
		for k := 0; k < 5; k++ {
			if i+k*ring.TSPacketSize >= len(out) || out[i+k*ring.TSPacketSize] != ring.SyncByte {
				aligned = false
				break
			}
		}
		if aligned {
			offset = i
		}
	}
	if offset < 0 {
		t.Fatal("capture is not TS aligned")
	}

	rewritten := 0
	for k := 0; offset+(k+1)*ring.TSPacketSize <= len(out); k++ {
		s := offset + k*ring.TSPacketSize
		pid := (uint16(out[s+1]&0x1F) << 8) | uint16(out[s+2])
		pusi := out[s+1]&0x40 != 0
		afc := (out[s+3] >> 4) & 0x03
		if pid != capturePMTPID || !pusi || afc == 0 || afc == 2 {
			continue
		}

		base := s + 4
		if afc == 3 {
			base = s + 5 + int(out[s+4])
		}
		if base >= s+ring.TSPacketSize {
			continue
		}
		sec := base + 1 + int(out[base])
		if sec+12 >= s+ring.TSPacketSize || out[sec] != 0x02 {
			continue
		}

		length := 3 + int(uint16(out[sec+1]&0x0F)<<8|uint16(out[sec+2]))
		if sec+length > s+ring.TSPacketSize {
			continue // section continues in the next packet
		}

		out[sec+5] = (out[sec+5] & 0xC1) | ((version & 0x1F) << 1)
		crc := ring.CalculateMPEG2CRC32(out[sec : sec+length-4])
		binary.BigEndian.PutUint32(out[sec+length-4:sec+length], crc)
		rewritten++
	}

	if rewritten == 0 {
		t.Fatal("no complete PMT section found to bump")
	}
	return out
}
