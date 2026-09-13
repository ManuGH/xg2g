// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"encoding/binary"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

// Where an audio elementary stream begins, and what happens when nothing says.
//
// ISO/IEC 13818-1 2.4.3.6: a transport packet whose payload_unit_start_indicator
// is set begins a new payload unit, and on a PID carrying PES that means the
// payload starts with the first byte of a PES packet. Everything after it, until
// the next such packet, is the body of that one PES packet.
//
// So when a payload unit start cannot be read as an audio PES packet, two things
// follow rather than one. The obvious one is that this packet carries no
// elementary stream to feed. The one this file is about is that the packets
// after it carry the body of that same unreadable packet - not audio whose
// boundary anything established.
//
// The adversarial continuations below carry valid AC-3 syncframes declaring 5.1,
// and the recovery after them carries stereo. A parser that feeds the
// quarantined bytes does not merely waste them: it counts frames the programme
// never carried, and says so in the observation.

const startBoundarySecondAudioPID = 0x012D

// --- fixtures ---------------------------------------------------------------

// startBoundaryPad fills a payload out to the 184 bytes a packet carries, with
// the stuffing a multiplexer uses.
func startBoundaryPad(body []byte) []byte {
	if len(body) > 184 {
		t := make([]byte, 184)
		copy(t, body)
		return t
	}
	out := make([]byte, 184)
	copy(out, body)
	for i := len(body); i < 184; i++ {
		out[i] = 0xFF
	}
	return out
}

func startBoundaryPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	if len(payload) != 184 {
		panic("a full packet carries 184 payload bytes")
	}
	p := make([]byte, TSPacketSize)
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)
	copy(p[4:], payload)
	return p
}

// startBoundaryShortPacket leaves exactly len(payload) bytes of payload, the
// rest of the packet being an adaptation field. It is how a payload unit start
// too short to hold a PES header actually reaches a parser.
func startBoundaryShortPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	if len(payload) > 182 {
		panic("a packet with an adaptation field carries at most 182 payload bytes")
	}
	p := make([]byte, TSPacketSize)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x30 | (cc & 0x0F)
	p[4] = byte(TSPacketSize - 5 - len(payload))
	p[5] = 0x00
	copy(p[TSPacketSize-len(payload):], payload)
	return p
}

// startBoundaryPES is the payload of a packet beginning a PES packet: the fixed
// header, the optional header the length declares, then the bytes after it.
func startBoundaryPES(streamID, headerDataLength byte, es []byte) []byte {
	h := []byte{0x00, 0x00, 0x01, streamID, 0x00, 0x00, 0x80, 0x00, headerDataLength}
	h = append(h, make([]byte, int(headerDataLength))...)
	return startBoundaryPad(append(h, es...))
}

// startBoundaryPMT names one video stream and an AC-3 track on each PID given.
func startBoundaryPMT(version uint8, audioPIDs ...uint16) []byte {
	descriptors := []byte{0x6A, 0x02, 0x80, 0x04}
	es := []byte{
		0x1B,
		0xE0 | byte(shadowVideoPID>>8), byte(shadowVideoPID & 0xFF),
		0xF0, 0x00,
	}
	for _, pid := range audioPIDs {
		es = append(es,
			0x06,
			0xE0|byte(pid>>8), byte(pid&0xFF),
			0xF0|byte(len(descriptors)>>8), byte(len(descriptors)&0xFF),
		)
		es = append(es, descriptors...)
	}
	sectionLen := 9 + len(es) + 4
	s := []byte{
		0x02,
		0xB0 | byte(sectionLen>>8), byte(sectionLen & 0xFF),
		0x00, 0x01,
		0xC0 | ((version & 0x1F) << 1) | 0x01,
		0x00, 0x00,
		0xE0 | byte(shadowVideoPID>>8), byte(shadowVideoPID & 0xFF),
		0xF0, 0x00,
	}
	s = append(s, es...)
	s = append(s, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(s[len(s)-4:], CalculateMPEG2CRC32(s[:len(s)-4]))
	return shadowPSIPacket(shadowPMTPID, s)
}

// startBoundaryRun drives a core through the chunks with a shadow attached, and
// waits for the comparison to catch up after each one.
//
// The shadow is what says which bytes the core's own observer was given. Working
// it out from the packets here would be writing a second parser and comparing it
// with itself.
func startBoundaryRun(t *testing.T, core *GoCore, shadow *replayShadow, chunks ...[]byte) Facts {
	t.Helper()
	ctx := t.Context()
	var facts Facts
	offset := int64(0)
	for i, chunk := range chunks {
		res, err := core.Ingest(ctx, offset, chunk)
		if err != nil {
			t.Fatalf("chunk %d: ingest: %v", i, err)
		}
		offset += int64(len(chunk))
		facts = res.Facts
		report := awaitShadow(t, core, "the shadow to catch up", func(r AudioShadowReport) bool {
			return r.Compared == r.Batches || r.Disabled
		})
		if report.Disabled {
			t.Fatalf("chunk %d: the shadow was retired, so the trace is incomplete: %+v", i, report)
		}
		if report.Mismatches != 0 {
			t.Fatalf("chunk %d: the replay disagreed with the core: %+v", i, report)
		}
	}
	return facts
}

func startBoundaryCore(t *testing.T) (*GoCore, *replayShadow) {
	t.Helper()
	shadow := newReplayShadow()
	core := NewGoCore(1)
	core.SetAudioShadow(shadow)
	t.Cleanup(core.CloseAudioShadow)
	return core, shadow
}

func join(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// --- the four payload units that establish nothing ---------------------------

func TestAudioStart_APayloadUnitThatIsNotAnAudioPESQuarantinesWhatFollows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(cc byte) []byte
	}{
		{
			// A: the prefix a PES packet must begin with is not there, so
			// nothing says what this payload unit is.
			name: "a start code prefix that is not 00 00 01",
			start: func(cc byte) []byte {
				body := append([]byte{0x00, 0x00, 0x02, 0xBD, 0x00, 0x00, 0x80, 0x00, 0x00},
					shadowAC3Run(shadowByte6Surround, 1)...)
				return startBoundaryPacket(shadowAudioPID, true, cc, startBoundaryPad(body))
			},
		},
		{
			// B: five bytes cannot hold the fixed header, so neither the stream
			// id nor the length of the optional header was ever read. Where the
			// elementary stream begins is not merely unread - it is unknowable
			// from what arrived.
			name: "a fixed header cut short by an adaptation field",
			start: func(cc byte) []byte {
				return startBoundaryShortPacket(shadowAudioPID, true, cc,
					[]byte{0x00, 0x00, 0x01, 0xBD, 0x00})
			},
		},
		{
			// C: a valid PES packet whose stream id is video. The table says
			// this PID is audio and the stream says otherwise; refusing the
			// start and then taking its body is not a decision.
			name: "a video stream id on a PID the table calls audio",
			start: func(cc byte) []byte {
				return startBoundaryPacket(shadowAudioPID, true, cc,
					startBoundaryPES(0xE0, 0, shadowAC3Run(shadowByte6Surround, 1)))
			},
		},
		{
			// D: padding_stream carries no optional header and no audio. Its
			// body is stuffing.
			name: "a padding stream id, which carries no optional header",
			start: func(cc byte) []byte {
				body := append([]byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x00},
					shadowAC3Run(shadowByte6Surround, 1)...)
				return startBoundaryPacket(shadowAudioPID, true, cc, startBoundaryPad(body))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, shadow := startBoundaryCore(t)

			// Three continuations of the unreadable payload unit, each carrying
			// a syncframe that declares 5.1. Fed, they would establish a layout
			// this programme never carried.
			adversary := make([][]byte, 3)
			for i := range adversary {
				adversary[i] = startBoundaryPad(shadowAC3Run(shadowByte6Surround, 1))
			}
			// Then a PES packet that does say where its elementary stream
			// begins, and three continuations of it. Four stereo frames: enough
			// to prove a layout, and countable.
			recovery := startBoundaryPES(0xBD, 0, shadowAC3Run(shadowByte6Stereo, 1))
			tail := make([][]byte, 3)
			for i := range tail {
				tail[i] = startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))
			}

			facts := startBoundaryRun(t, core, shadow,
				join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
				join(
					tc.start(0),
					startBoundaryPacket(shadowAudioPID, false, 1, adversary[0]),
					startBoundaryPacket(shadowAudioPID, false, 2, adversary[1]),
					startBoundaryPacket(shadowAudioPID, false, 3, adversary[2]),
					startBoundaryPacket(shadowAudioPID, true, 4, recovery),
					startBoundaryPacket(shadowAudioPID, false, 5, tail[0]),
					startBoundaryPacket(shadowAudioPID, false, 6, tail[1]),
					startBoundaryPacket(shadowAudioPID, false, 7, tail[2]),
				),
			)

			// The observer was given the recovered PES packet and nothing else.
			assertFeeds(t, shadow.batchesFor(shadowAudioPID), [][]byte{
				recovery[9:], tail[0], tail[1], tail[2],
			})

			got := observedTrack(t, facts, shadowAudioPID)
			want := esaudio.Observation{Channels: 2, Acmod: 2, HasAcmod: true, Frames: 4}
			if got != want {
				t.Errorf("observation\n got  %+v\n want %+v", got, want)
			}
			// One payload unit could not be read, and one is what the core says.
			// The count is diagnostic, and a count nothing reads is not evidence.
			if core.audioUnreadableStarts != 1 {
				t.Errorf("unreadable starts = %d, want 1", core.audioUnreadableStarts)
			}
		})
	}
}

// The one case this correction deliberately leaves alone.
//
// A payload unit that does begin an audio PES packet, whose optional header
// reaches past the packet carrying it, is a different question: the elementary
// stream boundary was established and then ran out of room. What the reference
// does with the remainder is a divergence reviewed on its own, and changing it
// here would fold two decisions into one commit.
func TestAudioStart_AValidStartWhoseHeaderRunsPastItsPacketIsUnchanged(t *testing.T) {
	core, shadow := startBoundaryCore(t)

	header := startBoundaryPES(0xBD, 200, nil) // 9 + 200 = 209 > 184
	body := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))

	facts := startBoundaryRun(t, core, shadow,
		join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
		join(
			startBoundaryPacket(shadowAudioPID, true, 0, header),
			startBoundaryPacket(shadowAudioPID, false, 1, body),
		),
	)

	assertFeeds(t, shadow.batchesFor(shadowAudioPID), [][]byte{body})
	if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 1 {
		t.Errorf("frames = %d, want 1 - the reviewed behaviour is unchanged", got)
	}
	if core.audioUnreadableStarts != 0 {
		t.Errorf("unreadable starts = %d, want 0 - this start was read", core.audioUnreadableStarts)
	}
}

// --- what the quarantine does and does not reach ----------------------------

func TestAudioStart_OneStreamsQuarantineLeavesTheOtherAlone(t *testing.T) {
	core, shadow := startBoundaryCore(t)

	adversary := startBoundaryPad(shadowAC3Run(shadowByte6Surround, 1))
	other := startBoundaryPES(0xBD, 0, shadowAC3Run(shadowByte6Stereo, 1))
	otherTail := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))

	facts := startBoundaryRun(t, core, shadow,
		join(shadowPAT(), startBoundaryPMT(0, shadowAudioPID, startBoundarySecondAudioPID)),
		join(
			// The first stream loses its boundary.
			startBoundaryPacket(shadowAudioPID, true, 0,
				startBoundaryPES(0xE0, 0, shadowAC3Run(shadowByte6Surround, 1))),
			startBoundaryPacket(shadowAudioPID, false, 1, adversary),
			// The second is untouched by that.
			startBoundaryPacket(startBoundarySecondAudioPID, true, 0, other),
			startBoundaryPacket(startBoundarySecondAudioPID, false, 1, otherTail),
		),
	)

	if batches := shadow.batchesFor(shadowAudioPID); len(batches) != 0 {
		t.Errorf("the quarantined stream was fed %d batches", len(batches))
	}
	assertFeeds(t, shadow.batchesFor(startBoundarySecondAudioPID), [][]byte{other[9:], otherTail})
	if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 0 {
		t.Errorf("quarantined stream: frames = %d, want 0", got)
	}
	if got := observedTrack(t, facts, startBoundarySecondAudioPID).Frames; got != 2 {
		t.Errorf("the other stream: frames = %d, want 2", got)
	}
}

// A programme change ends the stream that was waiting, along with everything
// else the table said. What follows belongs to a different elementary stream,
// and nothing has contradicted the new table yet.
func TestAudioStart_AProgrammeChangeEndsTheWait(t *testing.T) {
	core, shadow := startBoundaryCore(t)

	resumed := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))
	facts := startBoundaryRun(t, core, shadow,
		join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
		join(startBoundaryPacket(shadowAudioPID, true, 0,
			startBoundaryPES(0xE0, 0, shadowAC3Run(shadowByte6Surround, 1)))),
		join(shadowPMT(1, shadowAudioPID)),
		join(startBoundaryPacket(shadowAudioPID, false, 1, resumed)),
	)

	assertFeeds(t, shadow.batchesFor(shadowAudioPID), [][]byte{resumed})
	if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 1 {
		t.Errorf("frames = %d, want 1 - the new table's stream starts unencumbered", got)
	}
}

// Selecting another programme, or resetting the core, does the same and for the
// same reason: what the PID carried belonged to a programme that is no longer
// the one being followed.
//
// Reselecting the programme already selected is not a change and is left alone,
// which is why the first half of this goes away from programme one and comes
// back to it rather than setting it twice.
func TestAudioStart_ATargetChangeOrAResetEndsTheWait(t *testing.T) {
	unreadableStart := func(cc byte) []byte {
		return startBoundaryPacket(shadowAudioPID, true, cc,
			startBoundaryPES(0xE0, 0, shadowAC3Run(shadowByte6Surround, 1)))
	}

	t.Run("a target change", func(t *testing.T) {
		core, shadow := startBoundaryCore(t)
		ctx := t.Context()
		startBoundaryRun(t, core, shadow,
			join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
			join(unreadableStart(0)),
		)
		for _, target := range []uint16{2, 1} {
			if _, err := core.SetTargetProgram(ctx, target); err != nil {
				t.Fatalf("set target %d: %v", target, err)
			}
		}

		resumed := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))
		facts := startBoundaryRun(t, core, shadow,
			join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
			join(startBoundaryPacket(shadowAudioPID, false, 1, resumed)),
		)
		if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 1 {
			t.Errorf("frames = %d, want 1 - the programme that was waiting is gone", got)
		}
	})

	t.Run("a reset", func(t *testing.T) {
		core, shadow := startBoundaryCore(t)
		startBoundaryRun(t, core, shadow,
			join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
			join(unreadableStart(0)),
		)
		core.Reset()

		resumed := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))
		facts := startBoundaryRun(t, core, shadow,
			join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
			join(startBoundaryPacket(shadowAudioPID, false, 1, resumed)),
		)
		if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 1 {
			t.Errorf("frames = %d, want 1 - a reset keeps nothing", got)
		}
	})
}

// Reselecting the programme already being followed is not a change, so a stream
// that was waiting for a start is still waiting. Pinned because the test above
// once said the opposite by setting the same programme twice, and passed for a
// reason that had nothing to do with what it claimed.
func TestAudioStart_ReselectingTheSameProgrammeKeepsTheWait(t *testing.T) {
	core, shadow := startBoundaryCore(t)
	ctx := t.Context()
	startBoundaryRun(t, core, shadow,
		join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
		join(startBoundaryPacket(shadowAudioPID, true, 0,
			startBoundaryPES(0xE0, 0, shadowAC3Run(shadowByte6Surround, 1)))),
	)
	if _, err := core.SetTargetProgram(ctx, 1); err != nil {
		t.Fatalf("set target: %v", err)
	}

	facts := startBoundaryRun(t, core, shadow,
		join(startBoundaryPacket(shadowAudioPID, false, 1,
			startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1)))),
	)
	if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 0 {
		t.Errorf("frames = %d, want 0 - nothing changed, so nothing resumed", got)
	}
}

// A stream that has never seen a payload unit start is not waiting for one.
//
// A capture that begins in the middle of a PES packet carries audio from its
// first packet, and nothing about it contradicts the table. That is a different
// state from one whose payload unit start was read and refused, and it stays
// what it was.
func TestAudioStart_AContinuationBeforeAnyStartIsStillFed(t *testing.T) {
	core, shadow := startBoundaryCore(t)

	body := startBoundaryPad(shadowAC3Run(shadowByte6Stereo, 1))
	facts := startBoundaryRun(t, core, shadow,
		join(shadowPAT(), shadowPMT(0, shadowAudioPID)),
		join(startBoundaryPacket(shadowAudioPID, false, 0, body)),
	)

	assertFeeds(t, shadow.batchesFor(shadowAudioPID), [][]byte{body})
	if got := observedTrack(t, facts, shadowAudioPID).Frames; got != 1 {
		t.Errorf("frames = %d, want 1", got)
	}
}
