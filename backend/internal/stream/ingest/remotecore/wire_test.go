// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"os"
	"testing"
	"time"
)

// The same bytes media-core/src/ipc.rs asserts. Two implementations of one format
// drift, and they drift silently: each side keeps passing its own tests while
// agreeing on something the other no longer sends. This is the one place where
// both are pinned to the same literal, so an edit on either side turns one of the
// two red.
var goldenIngestAnswer = []byte{
	0x00, 0x00, 0x00, 0x0F, // length: header 6 + body 9
	0x01,                   // version
	0x02,                   // ingest
	0x00, 0x00, 0x00, 0x07, // request id 7
	0x00,                                           // status ok
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x2A, // through = 1066
}

func TestWire_AnIngestAnswerIsOnTheWireExactlyAsAgreed(t *testing.T) {
	body := make([]byte, 9)
	body[0] = StatusOK
	binary.BigEndian.PutUint64(body[1:], 1066)

	raw, err := Frame{Version: Version, Type: MsgIngest, RequestID: 7, Body: body}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(raw, goldenIngestAnswer) {
		t.Errorf("frame on the wire =\n  %x\nwant\n  %x", raw, goldenIngestAnswer)
	}
}

func TestWire_DecodeRejectsAFrameShorterThanItsHeader(t *testing.T) {
	if _, err := DecodeHeader([]byte{0x01, 0x02}); err == nil {
		t.Error("a two byte payload decoded as a header")
	}
}

func TestWire_EncodeRefusesABodyPastTheLimit(t *testing.T) {
	if _, err := (Frame{Body: make([]byte, MaxFrameSize)}).Encode(); err == nil {
		t.Error("a body past the frame limit encoded")
	}
}

// The real core, when there is one to talk to. Skipped rather than built here:
// building Rust from a Go test would make every run of this package depend on a
// toolchain it otherwise does not need. CI and the acceptance run set the path.
func TestEndToEnd_TheRealCoreAnswersWhatItWasGiven(t *testing.T) {
	bin := os.Getenv("XG2G_MEDIA_CORE_BIN")
	if bin == "" {
		t.Skip("XG2G_MEDIA_CORE_BIN not set; build media-core and point this at it")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("media core binary not usable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	core, err := Start(ctx, bin, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := core.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	const start = 4096
	chunk := make([]byte, 188*10)

	res, err := core.Ingest(ctx, start, chunk)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if want := int64(start + len(chunk)); res.ProcessedThroughOffset != want {
		t.Errorf("ProcessedThroughOffset = %d, want %d", res.ProcessedThroughOffset, want)
	}

	if _, err := core.SetTargetProgram(ctx, 2); err != nil {
		t.Errorf("SetTargetProgram: %v", err)
	}
}

// The frame bound is chosen against a number that lives in another package. This
// is the check that says so out loud: raise the default staging buffer past what
// a frame can hold and this fails, instead of every ingest failing at runtime.
//
// Only the default. A configured StagingBufferCapacity has no upper bound - see
// normalizer.Config.Validate, which checks a minimum and nothing else - so this
// cannot promise anything about an operator who sets a larger one. That case is
// handled where it has to be: Encode refuses rather than truncating.
func TestWire_TheDefaultIngestChunkFitsAFrame(t *testing.T) {
	chunk := normalizer.DefaultConfig().StagingBufferCapacity
	// An ingest request is the header, the start offset, then the chunk.
	need := HeaderSize + 8 + chunk
	if need > MaxFrameSize {
		t.Fatalf("a default ingest chunk needs %d bytes on the wire, MaxFrameSize is %d; "+
			"the staging buffer default grew past what a frame can carry", need, MaxFrameSize)
	}
	// And that it still fits is not the same as it being sendable.
	if _, err := (Frame{Type: MsgIngest, Body: make([]byte, 8+chunk)}).Encode(); err != nil {
		t.Fatalf("encoding a default-sized ingest request: %v", err)
	}
}
