// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// ErrShadowRetired means this shadow has stopped for good and will not be asked
// anything again.
//
// Once, and permanently. A shadow that failed is not a shadow that needs another
// go: its peer holds an observer per stream and epoch, and a call that did not
// complete leaves that state at an unknown point in the stream. Everything after
// it would be compared against a peer that saw a different stream from the one
// the reference saw, and agreement would stop meaning anything - which is worse
// than no comparison, because it looks like one.
var ErrShadowRetired = errors.New("remotecore: audio shadow has been retired")

// RemoteAudioShadow is a mediafacts.AudioShadow living in another process.
//
// It owns a peer of its own: its own socket directory, its own listener, its own
// media core process and its own connection. That is not tidiness, it is the
// point of the type. The connection carries one request at a time behind a mutex,
// so a shadow sharing a connection with an authoritative RemoteCore would put a
// hung comparison in front of the next ingest - and the ingest path would wait on
// the shadow's peer through a lock, which is exactly the coupling 4b.2a removed
// on this side of the boundary. The wire is shared. The connection is not.
//
// It implements the AudioShadow contract and adds nothing to it. In particular it
// never restarts its peer, never retries a call, and never reconnects: see
// ErrShadowRetired. Ending the comparison is the runner's decision, made from the
// error this returns.
type RemoteAudioShadow struct {
	// peer is a whole media core process, started and owned here. Only
	// MsgObserveAudioBatch is ever sent to it; it is a Core that is never asked
	// to be one.
	peer *RemoteCore

	// retired is the one state transition there is. Read before every call and
	// written once, by whichever of a failure or a Close got there first.
	mu      sync.Mutex
	retired error
}

var _ mediafacts.AudioShadow = (*RemoteAudioShadow)(nil)

// StartAudioShadow launches a media core process to be a second observer.
//
// The handshake is the same one a core gets, with program 0: this peer is never
// asked about a program, and the exchange is here for what it proves rather than
// what it carries - that the peer speaks this build's protocol version. Finding
// that out now, before a single batch has been copied and sent, is the difference
// between a shadow that never starts and one that retires after its first chunk.
func StartAudioShadow(ctx context.Context, binaryPath string) (*RemoteAudioShadow, error) {
	peer, err := start(ctx, binaryPath, nil, 0)
	if err != nil {
		return nil, err
	}
	return &RemoteAudioShadow{peer: peer}, nil
}

// ObserveAudio asks the peer what it made of one chunk's batches.
//
// The cancellation contract is kept here, locally, and does not depend on the
// peer in any way. A context that ends puts a deadline in the past on this side
// of the socket, which is what wakes a blocked read or write; the peer is not
// asked to stop and is not trusted to. So a peer that never answers, that stops
// reading the request half way through, that sends half an answer and hangs, or
// that ignores cancellation entirely, ends this call at the same moment as one
// that behaves.
//
// Every failure is final. The connection is torn down before returning, so a peer
// that is still holding the other end of a half-written request cannot go on
// believing there is a conversation.
func (s *RemoteAudioShadow) ObserveAudio(ctx context.Context, batches []mediafacts.AudioShadowBatch) ([]mediafacts.AudioShadowObservation, error) {
	if err := s.failure(); err != nil {
		return nil, err
	}
	observations, err := s.observe(ctx, batches)
	if err != nil {
		s.retire(err)
		return nil, err
	}
	return observations, nil
}

func (s *RemoteAudioShadow) observe(ctx context.Context, batches []mediafacts.AudioShadowBatch) ([]mediafacts.AudioShadowObservation, error) {
	body, err := encodeObserveAudioRequest(batches)
	if err != nil {
		// A chunk too large to send is refused here rather than cut down to one
		// that fits. A truncated batch is a hole in a stateful comparison, and the
		// peer would carry that hole for the rest of the session.
		return nil, err
	}

	resp, err := s.peer.conn.roundTrip(ctx, Frame{Type: MsgObserveAudioBatch, Body: body})
	if err != nil {
		return nil, err
	}
	if err := statusError(resp); err != nil {
		return nil, err
	}

	observations, err := decodeObserveAudioAnswer(resp.Body[1:])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", mediafacts.ErrCoreInvalidResponse, err)
	}

	// The answer has to line up with the question, one for one and in order.
	//
	// The runner checks this too, and deliberately: it cannot assume anything
	// about the shadow it was handed. Here it is a protocol failure rather than a
	// disagreement, and it is worth being the one to say so - a peer that answers
	// about a stream nobody asked about has lost track of the conversation, not of
	// the audio, and reconnecting to it would tell us nothing about which.
	if len(observations) != len(batches) {
		return nil, fmt.Errorf("%w: asked about %d batches, answered %d",
			mediafacts.ErrCoreInvalidResponse, len(batches), len(observations))
	}
	for i := range observations {
		if observations[i].PID != batches[i].PID || observations[i].Epoch != batches[i].Epoch {
			return nil, fmt.Errorf("%w: answer %d is about pid %d epoch %d, asked about pid %d epoch %d",
				mediafacts.ErrCoreInvalidResponse, i,
				observations[i].PID, observations[i].Epoch, batches[i].PID, batches[i].Epoch)
		}
	}
	return observations, nil
}

// Close ends the comparison and the process behind it.
//
// Bounded and unconditional: the peer is asked to leave, then signalled, then
// killed, and reaped either way. It is not waited on to finish a call - a shadow
// that is mid-request against a peer that never answers is precisely the case
// this has to survive, and the connection being closed is what breaks that
// request loose.
//
// Whoever attached the shadow owns calling this. mediafacts cancels the context
// and waits for its worker; it does not know what the shadow is made of, and a
// process is not something it can be expected to reap.
func (s *RemoteAudioShadow) Close() error {
	s.retire(ErrShadowRetired)
	return s.peer.Close()
}

// retire records why the comparison stopped and breaks the connection.
//
// The connection, not the process. Closing the socket is what unblocks anything
// still inside a read or a write and what tells the peer, by end of stream, that
// there is nothing more coming - and it is safe to do from a failing call. Ending
// the process is a lifecycle decision with a grace period attached, and it
// belongs to Close, which has a caller who is prepared to wait for it.
func (s *RemoteAudioShadow) retire(cause error) {
	s.mu.Lock()
	first := s.retired == nil
	if first {
		s.retired = cause
	}
	s.mu.Unlock()
	if first {
		_ = s.peer.conn.close()
	}
}

func (s *RemoteAudioShadow) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrShadowRetired, s.retired)
}
