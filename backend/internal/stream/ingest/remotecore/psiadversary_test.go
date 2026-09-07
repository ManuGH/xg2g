// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// v3 answers a failing peer can give that the frame layer cannot see.
//
// The frame is well formed in every case here: right version, right type, right
// request id, a length that matches. What is wrong is inside the body, which is
// the only place left for a peer to be wrong once framing is agreed - and the
// peer is this process's own child, which is exactly the thing that can be wrong
// in ways nothing else is.

// psiAnswer builds a v3 body around a coverage and a processed-through offset,
// with no events, no facts and no tables. Enough to be accepted, so a test can
// change one thing about it.
func psiAnswer(coverage uint8, through uint64) []byte {
	body := []byte{StatusOK, coverage}
	body = binary.BigEndian.AppendUint64(body, through)
	body = binary.BigEndian.AppendUint32(body, 0) // events
	body = append(body,
		0x00,       // facts flags
		0x00,       // PMT version
		0x00, 0x00, // programme
		0x00, 0x00, // PMT PID
		0x00, 0x00, // video PID
		0x00, // video codec
	)
	body = binary.BigEndian.AppendUint32(body, 0) // audio PIDs
	body = binary.BigEndian.AppendUint32(body, 0) // audio tracks
	body = append(body, 0x00, 0x00, 0x00, 0x00)   // no PAT, no PMT sections
	return body
}

// TestPSIAdversary_AV2AnswerIsNotAV3Answer is the case the version bump exists
// for.
//
// A v2 peer answers ingest with a status byte and an offset. A v3 caller reading
// that would take the offset's first byte as a coverage and the rest as the
// start of one - agreeing with a peer that is describing something else. The
// handshake refuses a v2 peer before this can happen; this proves what would
// occur if one ever got past it.
func TestPSIAdversary_AV2AnswerIsNotAV3Answer(t *testing.T) {
	core, far := coreOnPipe(t)
	go func() {
		req := readRequest(t, far)
		body := append([]byte{StatusOK}, make([]byte, 8)...) // the whole v2 answer
		answer(t, far, Frame{Version: Version, Type: req.Type, RequestID: req.RequestID, Body: body})
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := core.Ingest(context.Background(), 0, make([]byte, 188))
		errCh <- err
	}()
	expect(t, errCh, mediafacts.ErrCoreInvalidResponse)
}

// TestPSIAdversary_APeerClaimingToReadEverythingIsRefused covers the one wrong
// answer that would be dangerous rather than merely wrong.
//
// Nothing on this protocol reads more than PSI. A peer claiming complete
// coverage is claiming something this build knows it cannot do - and it is the
// single claim that would let MasterRing commit the result.
func TestPSIAdversary_APeerClaimingToReadEverythingIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name     string
		coverage uint8
	}{
		{"complete", wireCoverageComplete},
		{"unstated", wireCoverageUnknown},
		{"a coverage nobody defined", 0x7F},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, far := coreOnPipe(t)
			go func() {
				req := readRequest(t, far)
				answer(t, far, Frame{
					Version:   Version,
					Type:      req.Type,
					RequestID: req.RequestID,
					Body:      psiAnswer(tc.coverage, 188),
				})
			}()

			errCh := make(chan error, 1)
			go func() {
				_, err := core.Ingest(context.Background(), 0, make([]byte, 188))
				errCh <- err
			}()
			expect(t, errCh, mediafacts.ErrCoreInvalidResponse)
		})
	}
}

// TestPSIAdversary_AChunkThatIsNotWholePacketsNeverReachesThePeer holds the
// refusal GoCore makes to the same error on this side of the socket. A round
// trip to learn it would give one implementation of Core a different failure
// mode from the other.
func TestPSIAdversary_AChunkThatIsNotWholePacketsNeverReachesThePeer(t *testing.T) {
	core, far := coreOnPipe(t)
	asked := make(chan struct{}, 1)
	go func() {
		_ = readRequest(t, far)
		asked <- struct{}{}
	}()

	_, err := core.Ingest(context.Background(), 0, make([]byte, 187))
	if !errors.Is(err, mediafacts.ErrInvalidPacketSize) {
		t.Fatalf("Ingest returned %v, want ErrInvalidPacketSize", err)
	}
	select {
	case <-asked:
		t.Error("the unaligned chunk was sent to the peer anyway")
	default:
	}
}
