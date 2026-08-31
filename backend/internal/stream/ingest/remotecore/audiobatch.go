// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The observe-audio body, in the sizes the encoder and the bound both need.
//
// Named rather than written out at each use, because these numbers are the whole
// of what the frame bound is argued from: a reader checking that argument should
// be reading the same constants the encoder does, not a second copy of them in a
// comment.
const (
	// observeCountPrefix is the u32 in front of a list.
	observeCountPrefix = 4
	// observeBatchOverhead is pid, epoch and the feed count.
	observeBatchOverhead = 2 + 8 + 4
	// observeFeedOverhead is the u32 length in front of a feed's bytes.
	observeFeedOverhead = 4
	// observeObservationSize is pid, epoch, channels, flags, acmod, frames.
	observeObservationSize = 2 + 8 + 1 + 1 + 1 + 8
)

// Observation flags. Three bits, and the byte carrying them may hold nothing
// else: an unknown bit is a peer saying something this build has no meaning for,
// which is the same failure as an unknown message and gets the same answer.
const (
	obsFlagLFE                uint8 = 1 << 0
	obsFlagHasAcmod           uint8 = 1 << 1
	obsFlagDependentSubstream uint8 = 1 << 2

	obsFlagsKnown = obsFlagLFE | obsFlagHasAcmod | obsFlagDependentSubstream
)

// ErrObserveMalformed means a body did not say what the layout says it must.
//
// Truncated, over-long, or announcing a count it does not carry - all of it is
// one thing: the peer and this build no longer agree about the format, and no
// part of such a body may be believed. There is no shorter answer to be salvaged
// from it, because a shadow's answers are only meaningful as a complete set.
var ErrObserveMalformed = errors.New("remotecore: malformed observe-audio body")

// observeRequestSize is what encodeObserveAudioRequest will produce.
//
// Separate from the encoding so the frame bound can be argued about before any
// bytes exist - see TestWire_TheLargestObserveExchangeFitsAFrame, which reasons
// about the worst chunk the ring can produce rather than about one it happens to
// have built.
func observeRequestSize(batches []mediafacts.AudioShadowBatch) int {
	n := observeCountPrefix
	for i := range batches {
		n += observeBatchOverhead
		for _, f := range batches[i].Feeds {
			n += observeFeedOverhead + len(f)
		}
	}
	return n
}

// observeAnswerSize is what the answer to a request about count batches will be.
//
// One observation per batch, in order, is not an assumption this makes about a
// well-behaved peer - it is the contract RemoteAudioShadow enforces on the way
// back, and an answer of any other length is refused there as a peer that lost
// track of the conversation. So the size of the answer is known from the
// question, before the question is asked.
//
// The leading byte is the status every answer starts with. It is counted here
// because the bound is about the frame, and the frame carries it.
func observeAnswerSize(count int) int {
	return 1 + observeCountPrefix + count*observeObservationSize
}

// encodeObserveAudioRequest lays out one ObserveAudio call.
//
// The feeds are written one length-prefixed run at a time and never joined. That
// is the whole point of the message: an observer carries a partial header across
// a call and skips frame payload that ran past the end of one, so the same bytes
// concatenated are a different input. A peer that agreed with the reference on
// merged feeds would have agreed about something nobody asked.
//
// The reference observation does not appear here. The shadow is asked what it
// makes of the bytes, not asked to agree with an answer it was handed - a peer
// that could see the expected result could produce it without reading anything.
func encodeObserveAudioRequest(batches []mediafacts.AudioShadowBatch) ([]byte, error) {
	total := observeRequestSize(batches)
	if total > MaxFrameSize-HeaderSize {
		return nil, fmt.Errorf("%w: an observe request needs %d bytes", ErrFrameTooLarge, total)
	}
	// And the answer, which is the question's problem and not the peer's.
	//
	// A batch costs observeBatchOverhead to ask about and observeObservationSize
	// to answer, and the answer is the larger, so a request that fits a frame can
	// have an answer that does not. media-core/src/ipc.rs already refuses to
	// answer one - and says in as many words that this side refuses to send it,
	// which until now this side did not do. Asking anyway gets StatusMalformed
	// back, and the shadow retires reading that as a peer that could not
	// understand the request. The request was understood perfectly; it was the
	// answer that could not be framed. Refused here, before anything is allocated
	// or sent, it is what it actually is - a question too large to ask.
	if answer := observeAnswerSize(len(batches)); answer > MaxFrameSize-HeaderSize {
		return nil, fmt.Errorf("%w: an observe request for %d batches would be answered with %d bytes",
			ErrFrameTooLarge, len(batches), answer)
	}
	count, err := countAsUint32(len(batches), "batches")
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, total)
	out = binary.BigEndian.AppendUint32(out, count)
	for i := range batches {
		b := &batches[i]
		feeds, err := countAsUint32(len(b.Feeds), "feeds")
		if err != nil {
			return nil, err
		}
		out = binary.BigEndian.AppendUint16(out, b.PID)
		out = binary.BigEndian.AppendUint64(out, b.Epoch)
		out = binary.BigEndian.AppendUint32(out, feeds)
		for _, f := range b.Feeds {
			length, err := countAsUint32(len(f), "feed bytes")
			if err != nil {
				return nil, err
			}
			out = binary.BigEndian.AppendUint32(out, length)
			out = append(out, f...)
		}
	}
	return out, nil
}

// decodeObserveAudioRequest reads a request back.
//
// Production on this side never calls it: Go asks and Rust answers. It exists so
// that the format has a reader here too - a golden frame proves the bytes are
// what was agreed, and this proves they still mean what was agreed, which are
// different claims. The in-process peers the adversarial tests are built from
// use it to answer honestly.
func decodeObserveAudioRequest(body []byte) ([]mediafacts.AudioShadowBatch, error) {
	r := reader{b: body}
	count, ok := r.uint32()
	if !ok {
		return nil, fmt.Errorf("%w: no batch count", ErrObserveMalformed)
	}
	// A count is not an allocation instruction. Every batch costs at least its own
	// overhead, so a body that cannot hold that many is failing before anything is
	// reserved for it.
	if int(count) > (len(body)-observeCountPrefix)/observeBatchOverhead {
		return nil, fmt.Errorf("%w: announced %d batches in %d bytes", ErrObserveMalformed, count, len(body))
	}

	batches := make([]mediafacts.AudioShadowBatch, 0, count)
	for i := uint32(0); i < count; i++ {
		pid, ok1 := r.uint16()
		epoch, ok2 := r.uint64()
		feedCount, ok3 := r.uint32()
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("%w: batch %d ended inside its header", ErrObserveMalformed, i)
		}
		if int(feedCount) > r.left()/observeFeedOverhead {
			return nil, fmt.Errorf("%w: batch %d announced %d feeds in %d remaining bytes",
				ErrObserveMalformed, i, feedCount, r.left())
		}
		b := mediafacts.AudioShadowBatch{PID: pid, Epoch: epoch}
		if feedCount > 0 {
			b.Feeds = make([][]byte, 0, feedCount)
		}
		for j := uint32(0); j < feedCount; j++ {
			length, ok := r.uint32()
			if !ok {
				return nil, fmt.Errorf("%w: batch %d feed %d has no length", ErrObserveMalformed, i, j)
			}
			feed, ok := r.bytes(int(length))
			if !ok {
				return nil, fmt.Errorf("%w: batch %d feed %d promised %d bytes, %d remain",
					ErrObserveMalformed, i, j, length, r.left())
			}
			b.Feeds = append(b.Feeds, feed)
		}
		batches = append(batches, b)
	}
	// Trailing bytes are not a longer message, they are a different one. A reader
	// that ignores them cannot tell a peer it agrees with from one it does not.
	if r.left() != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the last batch", ErrObserveMalformed, r.left())
	}
	return batches, nil
}

// encodeObserveAudioAnswer lays out the answer to one call, status byte first.
func encodeObserveAudioAnswer(observations []mediafacts.AudioShadowObservation) ([]byte, error) {
	count, err := countAsUint32(len(observations), "observations")
	if err != nil {
		return nil, err
	}
	total := observeAnswerSize(len(observations))
	if total > MaxFrameSize-HeaderSize {
		return nil, fmt.Errorf("%w: an observe answer needs %d bytes", ErrFrameTooLarge, total)
	}

	out := make([]byte, 0, total)
	out = append(out, StatusOK)
	out = binary.BigEndian.AppendUint32(out, count)
	for _, o := range observations {
		if o.Observation.Channels < 0 || o.Observation.Channels > 255 {
			return nil, fmt.Errorf("%w: %d channels does not fit the wire", ErrObserveMalformed, o.Observation.Channels)
		}
		var flags uint8
		if o.Observation.LFE {
			flags |= obsFlagLFE
		}
		if o.Observation.HasAcmod {
			flags |= obsFlagHasAcmod
		}
		if o.Observation.DependentSubstream {
			flags |= obsFlagDependentSubstream
		}
		out = binary.BigEndian.AppendUint16(out, o.PID)
		out = binary.BigEndian.AppendUint64(out, o.Epoch)
		out = append(out, uint8(o.Observation.Channels), flags, o.Observation.Acmod)
		out = binary.BigEndian.AppendUint64(out, o.Observation.Frames)
	}
	return out, nil
}

// decodeObserveAudioAnswer reads the answer, and refuses everything that is not
// exactly it.
//
// The status byte has already been checked by the caller; what arrives here is
// the body after it. Nothing is returned on any failure - not the observations
// read so far, not a shorter list. A partial answer is a peer that has lost track
// of what it was asked, and every number in it is about an unknown stream.
func decodeObserveAudioAnswer(body []byte) ([]mediafacts.AudioShadowObservation, error) {
	r := reader{b: body}
	count, ok := r.uint32()
	if !ok {
		return nil, fmt.Errorf("%w: no observation count", ErrObserveMalformed)
	}
	if int(count) > r.left()/observeObservationSize {
		return nil, fmt.Errorf("%w: announced %d observations in %d bytes", ErrObserveMalformed, count, r.left())
	}

	out := make([]mediafacts.AudioShadowObservation, 0, count)
	for i := uint32(0); i < count; i++ {
		pid, ok1 := r.uint16()
		epoch, ok2 := r.uint64()
		fixed, ok3 := r.bytes(3)
		frames, ok4 := r.uint64()
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, fmt.Errorf("%w: observation %d ended early", ErrObserveMalformed, i)
		}
		channels, flags, acmod := fixed[0], fixed[1], fixed[2]
		if flags&^obsFlagsKnown != 0 {
			return nil, fmt.Errorf("%w: observation %d carries flags %#02x, this build knows %#02x",
				ErrObserveMalformed, i, flags, obsFlagsKnown)
		}
		out = append(out, mediafacts.AudioShadowObservation{
			PID:   pid,
			Epoch: epoch,
			Observation: esaudio.Observation{
				Channels:           int(channels),
				LFE:                flags&obsFlagLFE != 0,
				Acmod:              acmod,
				HasAcmod:           flags&obsFlagHasAcmod != 0,
				DependentSubstream: flags&obsFlagDependentSubstream != 0,
				Frames:             frames,
			},
		})
	}
	if r.left() != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the last observation", ErrObserveMalformed, r.left())
	}
	return out, nil
}

func countAsUint32(n int, what string) (uint32, error) {
	if n < 0 || int64(n) > int64(^uint32(0)) {
		return 0, fmt.Errorf("%w: %d %s does not fit the wire", ErrObserveMalformed, n, what)
	}
	return uint32(n), nil
}

// reader walks a body without ever reading past it.
//
// Every read reports whether it happened. Nothing here returns a zero value that
// a caller could mistake for a field the peer actually sent.
type reader struct {
	b []byte
	i int
}

func (r *reader) left() int { return len(r.b) - r.i }

func (r *reader) bytes(n int) ([]byte, bool) {
	if n < 0 || r.left() < n {
		return nil, false
	}
	out := r.b[r.i : r.i+n : r.i+n]
	r.i += n
	return out, true
}

func (r *reader) uint16() (uint16, bool) {
	b, ok := r.bytes(2)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint16(b), true
}

func (r *reader) uint32() (uint32, bool) {
	b, ok := r.bytes(4)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint32(b), true
}

func (r *reader) uint64() (uint64, bool) {
	b, ok := r.bytes(8)
	if !ok {
		return 0, false
	}
	return binary.BigEndian.Uint64(b), true
}
