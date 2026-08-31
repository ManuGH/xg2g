// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
)

// The same bytes media-core/src/ipc.rs asserts, for the same reason as the ingest
// answer beside them: two implementations of one format drift silently, each
// passing its own tests while agreeing on something the other no longer sends.
//
// The second feed is one byte long on purpose. A reader that quietly joined the
// feeds would still have to produce these exact lengths to pass, so the boundary
// is pinned here and not only in the observer's behaviour.
var goldenObserveRequest = []byte{
	0x00, 0x00, 0x00, 0x23, // length: header 6 + body 29
	0x02,                   // version
	0x05,                   // observe audio batch
	0x00, 0x00, 0x00, 0x09, // request id 9
	0x00, 0x00, 0x00, 0x01, // one batch
	0x01, 0x2C, // pid 300
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // epoch 2
	0x00, 0x00, 0x00, 0x02, // two feeds
	0x00, 0x00, 0x00, 0x02, 0x0B, 0x77, // feed 0
	0x00, 0x00, 0x00, 0x01, 0xAA, // feed 1
}

var goldenObserveAnswer = []byte{
	0x00, 0x00, 0x00, 0x20, // length: header 6 + body 26
	0x02,                   // version
	0x05,                   // observe audio batch
	0x00, 0x00, 0x00, 0x09, // request id 9
	0x00,                   // status ok
	0x00, 0x00, 0x00, 0x01, // one observation
	0x01, 0x2C, // pid 300
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // epoch 2
	0x06,                                           // channels
	0x03,                                           // flags: lfe | has acmod
	0x07,                                           // acmod
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0xB4, // frames 1460
}

func goldenBatch() []mediafacts.AudioShadowBatch {
	return []mediafacts.AudioShadowBatch{{
		PID:   300,
		Epoch: 2,
		Feeds: [][]byte{{0x0B, 0x77}, {0xAA}},
	}}
}

func goldenObservation() []mediafacts.AudioShadowObservation {
	return []mediafacts.AudioShadowObservation{{
		PID:   300,
		Epoch: 2,
		Observation: esaudio.Observation{
			Channels: 6, LFE: true, Acmod: 7, HasAcmod: true, Frames: 1460,
		},
	}}
}

func TestObserveWire_ARequestIsOnTheWireExactlyAsAgreed(t *testing.T) {
	body, err := encodeObserveAudioRequest(goldenBatch())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, err := Frame{Version: Version, Type: MsgObserveAudioBatch, RequestID: 9, Body: body}.Encode()
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	if !bytes.Equal(raw, goldenObserveRequest) {
		t.Errorf("request on the wire =\n  %x\nwant\n  %x", raw, goldenObserveRequest)
	}
}

func TestObserveWire_AnAnswerIsOnTheWireExactlyAsAgreed(t *testing.T) {
	body, err := encodeObserveAudioAnswer(goldenObservation())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, err := Frame{Version: Version, Type: MsgObserveAudioBatch, RequestID: 9, Body: body}.Encode()
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	if !bytes.Equal(raw, goldenObserveAnswer) {
		t.Errorf("answer on the wire =\n  %x\nwant\n  %x", raw, goldenObserveAnswer)
	}
}

// The golden frames prove the bytes are what was agreed. This proves they still
// mean what was agreed, which is a different claim: an encoder and a decoder that
// drifted together would pass one and fail the other.
func TestObserveWire_WhatIsWrittenIsWhatIsRead(t *testing.T) {
	batches := []mediafacts.AudioShadowBatch{
		{PID: 300, Epoch: 1, Feeds: [][]byte{{1, 2, 3}, {}, {4}}},
		{PID: 301, Epoch: 1},
		{PID: 300, Epoch: 2, Feeds: [][]byte{{5, 6}}},
	}
	body, err := encodeObserveAudioRequest(batches)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeObserveAudioRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(batches) {
		t.Fatalf("decoded %d batches, wrote %d", len(got), len(batches))
	}
	for i := range batches {
		if got[i].PID != batches[i].PID || got[i].Epoch != batches[i].Epoch {
			t.Errorf("batch %d = pid %d epoch %d, want pid %d epoch %d",
				i, got[i].PID, got[i].Epoch, batches[i].PID, batches[i].Epoch)
		}
		if len(got[i].Feeds) != len(batches[i].Feeds) {
			t.Fatalf("batch %d came back with %d feeds, sent %d - a boundary was lost",
				i, len(got[i].Feeds), len(batches[i].Feeds))
		}
		for j := range batches[i].Feeds {
			if !bytes.Equal(got[i].Feeds[j], batches[i].Feeds[j]) {
				t.Errorf("batch %d feed %d = %x, want %x", i, j, got[i].Feeds[j], batches[i].Feeds[j])
			}
		}
	}
}

func TestObserveWire_AnAnswerSurvivesTheRoundTrip(t *testing.T) {
	want := []mediafacts.AudioShadowObservation{
		{PID: 1, Epoch: 7, Observation: esaudio.Observation{Channels: 6, LFE: true, Acmod: 7, HasAcmod: true, DependentSubstream: true, Frames: 9}},
		{PID: 2, Epoch: 7},
	}
	body, err := encodeObserveAudioAnswer(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeObserveAudioAnswer(body[1:])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("observations came back as\n  %+v\nwant\n  %+v", got, want)
	}
}

// Everything that is not exactly the agreed layout, refused - and refused whole.
// A shadow's answers only mean anything as a complete set, so there is no shorter
// reading to be salvaged from a body this side cannot read in full.
func TestObserveWire_NothingButTheAgreedLayoutIsAccepted(t *testing.T) {
	good, err := encodeObserveAudioRequest(goldenBatch())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cases := []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"a count and nothing else", good[:4]},
		{"cut inside a batch header", good[:10]},
		{"cut inside a feed", good[:len(good)-1]},
		{"trailing bytes", append(append([]byte(nil), good...), 0x00)},
		{"more batches than the body can hold", func() []byte {
			b := append([]byte(nil), good...)
			b[3] = 0xFF
			return b
		}()},
		{"a feed longer than the body", func() []byte {
			b := append([]byte(nil), good...)
			// The first feed's length, right after the batch header.
			at := observeCountPrefix + observeBatchOverhead
			b[at], b[at+1], b[at+2], b[at+3] = 0xFF, 0xFF, 0xFF, 0xFF
			return b
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeObserveAudioRequest(tc.body); err == nil {
				t.Errorf("decoded %d batches from a body that is not one", len(got))
			}
		})
	}
}

func TestObserveWire_AnAnswerWithNothingButTheAgreedLayoutIsAccepted(t *testing.T) {
	good, err := encodeObserveAudioAnswer(goldenObservation())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	good = good[1:] // past the status byte, which the caller checks

	cases := []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"a count and nothing else", good[:4]},
		{"cut inside an observation", good[:len(good)-1]},
		{"trailing bytes", append(append([]byte(nil), good...), 0x00)},
		{"more observations than the body can hold", func() []byte {
			b := append([]byte(nil), good...)
			b[3] = 0xFF
			return b
		}()},
		{"a flag this build has no meaning for", func() []byte {
			b := append([]byte(nil), good...)
			// flags sits after count, pid, epoch and channels.
			b[observeCountPrefix+2+8+1] |= 1 << 5
			return b
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeObserveAudioAnswer(tc.body); err == nil {
				t.Errorf("decoded %d observations from a body that is not an answer", len(got))
			}
		})
	}
}

// The frame bound, argued rather than assumed.
//
// MaxFrameSize is not raised for this message and this is why it does not have to
// be. The numbers below are the worst a chunk can be, not the shape of any chunk
// the tests happen to build: one batch and one feed per transport packet is the
// most fragmentation the capture side can produce, because a batch needs a feed
// and a feed needs a packet.
func TestObserveWire_TheLargestObserveExchangeFitsAFrame(t *testing.T) {
	const packet = 188
	// A transport packet with the smallest possible adaptation field still carries
	// this much payload, and payload is what a feed is made of.
	const maxPayloadPerPacket = packet - 4

	chunk := normalizer.DefaultConfig().StagingBufferCapacity
	packets := chunk / packet
	// One feed per packet, one batch per feed: the worst case is a chunk that
	// alternates between streams, where every packet starts a batch of its own.
	feeds, batches := packets, packets
	esBytes := packets * maxPayloadPerPacket

	request := observeCountPrefix + batches*observeBatchOverhead + feeds*observeFeedOverhead + esBytes
	answer := 1 + observeCountPrefix + batches*observeObservationSize

	if request > MaxFrameSize-HeaderSize {
		t.Errorf("the worst observe request for a %d byte chunk needs %d bytes, a frame holds %d",
			chunk, request, MaxFrameSize-HeaderSize)
	}
	if answer > MaxFrameSize-HeaderSize {
		t.Errorf("its answer needs %d bytes, a frame holds %d", answer, MaxFrameSize-HeaderSize)
	}
	t.Logf("worst case for a %d KiB chunk: request %d KiB, answer %d KiB, frame limit %d KiB",
		chunk/1024, request/1024, answer/1024, MaxFrameSize/1024)
}

// And a request past the bound is refused rather than cut down. A truncated batch
// is a hole in a stateful comparison, and the peer would carry it for the rest of
// the session.
func TestObserveWire_ARequestPastTheBoundIsRefusedNotTruncated(t *testing.T) {
	batches := []mediafacts.AudioShadowBatch{{
		PID:   1,
		Epoch: 1,
		Feeds: [][]byte{make([]byte, MaxFrameSize)},
	}}
	if _, err := encodeObserveAudioRequest(batches); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("encoding an oversized request gave %v, want ErrFrameTooLarge", err)
	}
}

// The widest value every field can hold, there and back.
//
// A fixed-width layout is only fixed if the widths are the ones both sides think
// they are. A field that fits until it does not - an epoch past a u32, a frame
// count past an int32, every flag at once - is the kind of disagreement that
// shows up as a mismatch about audio rather than as a broken wire.
func TestObserveWire_TheWidestValuesEveryFieldCanHold(t *testing.T) {
	batches := []mediafacts.AudioShadowBatch{{
		PID:   0x1FFF, // the largest PID a transport stream has
		Epoch: ^uint64(0),
		Feeds: [][]byte{{0xFF}},
	}}
	body, err := encodeObserveAudioRequest(batches)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	got, err := decodeObserveAudioRequest(body)
	if err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if got[0].PID != batches[0].PID || got[0].Epoch != batches[0].Epoch {
		t.Errorf("batch came back as pid %d epoch %d", got[0].PID, got[0].Epoch)
	}

	want := []mediafacts.AudioShadowObservation{{
		PID:   0x1FFF,
		Epoch: ^uint64(0),
		Observation: esaudio.Observation{
			Channels:           255,
			LFE:                true,
			Acmod:              255,
			HasAcmod:           true,
			DependentSubstream: true,
			Frames:             ^uint64(0),
		},
	}}
	answer, err := encodeObserveAudioAnswer(want)
	if err != nil {
		t.Fatalf("encode answer: %v", err)
	}
	back, err := decodeObserveAudioAnswer(answer[1:])
	if err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if !reflect.DeepEqual(back, want) {
		t.Errorf("observation came back as\n  %+v\nwant\n  %+v", back, want)
	}
}
