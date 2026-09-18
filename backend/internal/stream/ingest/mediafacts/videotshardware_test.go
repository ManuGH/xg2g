// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The archived Step-5d transport, replayed through the video path and measured.
//
// The corpus beside this file authors what the reference should say about
// transport a reviewer can read. What it cannot say is how often the
// conditions it authors actually occur on a receiver: a payload unit start on
// the video PID that is not a video PES start, a PES header reaching past its
// packet, a scrambled PES start, a repeated packet. Those numbers decide
// whether a reference correction changes anything on real transport, and they
// are measured here rather than assumed.
//
// Two things are written per capture. The reference's own answer - its events
// and its final video facts - as a trace a later implementation can be held
// to. And an independent count over the video PID's packets, made with the
// transport syntax alone and no parser state, so the count does not inherit
// whatever the reference does with the packets it counts.
//
// The captures are never re-captured and never modified. Skipped unless
// XG2G_PSI_HARDWARE_DIR names the archive; their identity is checked against
// the Step-5d manifest before a byte is read.

// videoHWChunk is the ingest size, in bytes: 348 packets, as Steps 5d and 6c
// replayed the same bytes.
const videoHWChunk = 348 * TSPacketSize

// videoHWSubjects are the archived captures and the programme each was
// resolved to in Step 5d.
var videoHWSubjects = []struct {
	name    string
	program uint16
}{
	{"A_orf1hd", 4911},
	{"A2_orf1hd", 4911},
	{"B1_puls4", 20007},
	{"B2_schlager", 35},
	{"H_pre", 4911},
	{"H_post", 4911},
	{"I_pre", 4911},
	{"I_post", 4911},
}

// videoHWMeasure is what the transport syntax alone says about one PID.
type videoHWMeasure struct {
	packets, payload, noPayload, clear, scrambled uint64

	pusi, pusiScrambled                       uint64
	validStart, headerIncomplete, headerExact uint64
	tooShort, wrongPrefix, nonVideoStreamID   uint64
	duplicates, gaps                          uint64
	firstIsContinuation                       bool
	startOffsets                              map[int64]bool
}

// videoHWMeasurePID counts the packets of one PID the way 13818-1 reads them,
// without asking the reference anything.
func videoHWMeasurePID(data []byte, pid uint16) videoHWMeasure {
	m := videoHWMeasure{startOffsets: map[int64]bool{}}
	var lastCC byte
	var last []byte
	seenPayload := false
	for at := 0; at+TSPacketSize <= len(data); at += TSPacketSize {
		pkt := data[at : at+TSPacketSize]
		if pkt[0] != SyncByte {
			continue
		}
		if (uint16(pkt[1]&0x1F)<<8)|uint16(pkt[2]) != pid {
			continue
		}
		m.packets++

		afc := (pkt[3] >> 4) & 0x03
		payloadAt := 4
		switch afc {
		case 0x01:
		case 0x03:
			payloadAt = 5 + int(pkt[4])
			if payloadAt >= TSPacketSize {
				m.noPayload++
				continue
			}
		default:
			m.noPayload++
			continue
		}
		m.payload++
		payload := pkt[payloadAt:]
		pusi := pkt[1]&0x40 != 0
		scrambledPkt := (pkt[3]>>6)&0x03 != 0

		cc := pkt[3] & 0x0F
		if seenPayload {
			switch {
			case cc == lastCC && string(pkt) == string(last):
				m.duplicates++
			case cc != (lastCC+1)&0x0F:
				m.gaps++
			}
		} else {
			m.firstIsContinuation = !pusi
		}
		seenPayload = true
		lastCC = cc
		last = pkt

		if scrambledPkt {
			m.scrambled++
			if pusi {
				m.pusi++
				m.pusiScrambled++
			}
			continue
		}
		m.clear++
		if !pusi {
			continue
		}
		m.pusi++
		switch {
		case len(payload) < 9:
			m.tooShort++
		case payload[0] != 0x00 || payload[1] != 0x00 || payload[2] != 0x01:
			m.wrongPrefix++
		case payload[3] < 0xE0 || payload[3] > 0xEF:
			m.nonVideoStreamID++
		default:
			m.validStart++
			m.startOffsets[int64(at)] = true
			esStart := 9 + int(payload[8])
			switch {
			case esStart > len(payload):
				m.headerIncomplete++
			case esStart == len(payload):
				m.headerExact++
			}
		}
	}
	return m
}

func TestVideoTSHardware_TheArchivedTransportIsReplayedAndMeasured(t *testing.T) {
	dir := os.Getenv("XG2G_PSI_HARDWARE_DIR")
	if dir == "" {
		t.Skip("XG2G_PSI_HARDWARE_DIR is unset; the archive is not here")
	}
	outDir := os.Getenv("XG2G_VIDEO_HARDWARE_OUT")
	if outDir == "" {
		t.Fatal("XG2G_VIDEO_HARDWARE_OUT must say where the traces go")
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("create the output directory: %v", err)
	}

	identities := audioHWCaptures(t)
	ctx := context.Background()

	for _, s := range videoHWSubjects {
		t.Run(s.name, func(t *testing.T) {
			want, ok := identities[s.name]
			if !ok {
				t.Fatalf("%s is not in the Step-5d manifest", s.name)
			}
			data := audioHWLoad(t, dir, s.name, want)

			core := NewGoCore(s.program)
			var events []Event
			var facts Facts
			offset := int64(0)
			for at := 0; at < len(data); at += videoHWChunk {
				end := min(at+videoHWChunk, len(data))
				res, err := core.Ingest(ctx, offset, data[at:end])
				if err != nil {
					t.Fatalf("%s at %d: %v", s.name, offset, err)
				}
				events = append(events, res.Events...)
				facts = res.Facts
				offset += int64(end - at)
			}

			var out strings.Builder
			fmt.Fprintf(&out, "# xg2g Step 7.0 video hardware trace, format version 1\n")
			fmt.Fprintf(&out, "capture %s program %d chunk %d bytes %d\n", s.name, s.program, videoHWChunk, len(data))
			fmt.Fprintf(&out, "video pid=%04x codec=%s\n", facts.VideoPID, facts.VideoCodec)

			var raps, joinable, identities uint64
			for _, e := range events {
				switch e.Kind {
				case EventRandomAccessPoint:
					raps++
					if e.Joinable {
						joinable++
					}
				case EventProgramIdentityChanged:
					identities++
				}
			}
			fmt.Fprintf(&out, "events rap=%d joinable=%d identity=%d\n", raps, joinable, identities)

			if facts.VideoPID == 0 {
				fmt.Fprintf(&out, "measure none: the table in force names no video stream\n")
			} else {
				m := videoHWMeasurePID(data, facts.VideoPID)
				fmt.Fprintf(&out, "transport packets=%d payload=%d nopayload=%d clear=%d scrambled=%d\n",
					m.packets, m.payload, m.noPayload, m.clear, m.scrambled)
				fmt.Fprintf(&out, "pusi total=%d scrambled=%d valid=%d header_incomplete=%d header_exact=%d too_short=%d wrong_prefix=%d non_video_stream_id=%d\n",
					m.pusi, m.pusiScrambled, m.validStart, m.headerIncomplete, m.headerExact, m.tooShort, m.wrongPrefix, m.nonVideoStreamID)
				fmt.Fprintf(&out, "continuity duplicates=%d gaps=%d first_is_continuation=%d\n",
					m.duplicates, m.gaps, b2i(m.firstIsContinuation))

				// The reference's own claim, checked against the syntax: every
				// entry point it reported sits on a packet that begins a valid
				// video PES packet. This is the Step-6b containment, made
				// without the Rust side.
				offStarts := 0
				for _, e := range events {
					if e.Kind != EventRandomAccessPoint {
						continue
					}
					if e.Offset%TSPacketSize != 0 {
						t.Errorf("%s: entry point at %d is not packet aligned", s.name, e.Offset)
					}
					if !m.startOffsets[e.Offset] {
						offStarts++
						t.Errorf("%s: entry point at %d is not on a valid video PES start", s.name, e.Offset)
					}
				}
				fmt.Fprintf(&out, "containment entry_points_off_a_pes_start=%d\n", offStarts)
			}

			for _, e := range events {
				switch e.Kind {
				case EventRandomAccessPoint:
					fmt.Fprintf(&out, "event rap offset=%d joinable=%d\n", e.Offset, b2i(e.Joinable))
				case EventProgramIdentityChanged:
					fmt.Fprintf(&out, "event identity\n")
				}
			}
			fmt.Fprintf(&out, "%s\n", strings.TrimSpace(videoTSFactsLine("facts", videoTSFactsOf(facts))))

			path := filepath.Join(outDir, s.name+".video.go.trace")
			if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			t.Logf("%s: %d entry points (%d joinable), pid %04x %s; trace at %s",
				s.name, raps, joinable, facts.VideoPID, facts.VideoCodec, path)
		})
	}
}
