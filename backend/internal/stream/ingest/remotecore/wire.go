// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package remotecore speaks to a media facts core running as a separate process.
//
// The protocol is deliberately small and deliberately boring. It exists to carry
// the contract mediafacts already defines across a process boundary, and nothing
// more: there is no multiplexing, no streaming, no negotiation beyond a version
// check, and exactly one request may be in flight at a time. Everything it does
// not do is something a later step can add once it is needed and understood.
//
// Two implementations of one wire format drift. A golden-frame test on this side
// and the same bytes asserted on the Rust side is what holds them together.
package remotecore

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Version is the protocol both ends must agree on before anything else happens.
//
// A single number, checked once, and fatal when it differs. There is no
// negotiation and no partial compatibility: a peer that speaks a version this
// build does not know is refused, because acting on fields that mean something
// else is worse than not talking at all.
const Version uint8 = 1

// Message types. The set is closed on purpose - it is exactly the calls
// mediafacts.Core makes, plus the two the connection itself needs.
const (
	MsgHandshake        uint8 = 1
	MsgIngest           uint8 = 2
	MsgSetTargetProgram uint8 = 3
	MsgShutdown         uint8 = 4
)

// Response status. Anything other than StatusOK means the body is a reason, not
// a result.
const (
	StatusOK              uint8 = 0
	StatusProtocolVersion uint8 = 1
	StatusMalformed       uint8 = 2
	StatusUnknownMessage  uint8 = 3
	StatusInternal        uint8 = 4
)

// HeaderSize is version + type + request id.
const HeaderSize = 1 + 1 + 4

// MaxFrameSize bounds a single message.
//
// The largest thing this protocol carries is one ingest chunk, and the largest
// chunk the ring produces today is the normalizer's staging buffer at its default
// of 4 MiB. That is the current default, not a ceiling: normalizer config
// validates only a lower bound on StagingBufferCapacity, so an operator can
// configure a larger one and nothing stops them. Eight leaves room for the
// default plus request metadata and a header, and for one increase, while still
// being a number a reader can refuse without allocating.
//
// TestWire_TheDefaultIngestChunkFitsAFrame keeps the first half of that honest:
// if the default staging buffer is raised past what fits, it fails here rather
// than in the field. It cannot speak for a configured value, which is why a
// chunk too large to send is a refusal at Encode and not a truncation.
//
// The bound exists so a length prefix cannot become an allocation instruction. A
// peer that announces more than this is not asking for memory, it is failing.
const MaxFrameSize = 8 * 1024 * 1024

var (
	// ErrFrameTooLarge means a peer announced a frame this side will not hold.
	ErrFrameTooLarge = errors.New("remotecore: frame exceeds the maximum size")

	// ErrShortFrame means a frame ended before the fields it promised.
	ErrShortFrame = errors.New("remotecore: frame is shorter than its header")
)

// Body layouts, so the two implementations have one place to disagree with rather
// than two to drift apart in:
//
//	handshake       request  u16 target program
//	                answer   u8 status
//	ingest          request  u64 start offset, then the chunk
//	                answer   u8 status, u64 processed-through offset
//	set target      request  u16 program number
//	                answer   u8 status
//	shutdown        request  empty
//	                answer   u8 status
//
// Every answer begins with a status byte. A body that does not is not an answer.

// Frame is one message, header and body.
type Frame struct {
	Version   uint8
	Type      uint8
	RequestID uint32
	Body      []byte
}

// Encode lays out a frame: a four byte big-endian length covering everything
// after it, then the header, then the body.
func (f Frame) Encode() ([]byte, error) {
	if len(f.Body) > MaxFrameSize-HeaderSize {
		return nil, fmt.Errorf("%w: body is %d bytes", ErrFrameTooLarge, len(f.Body))
	}

	// Bounded by the check above, written as a conversion that says so rather than
	// a cast that assumes it.
	length, err := safeUint32(HeaderSize + len(f.Body))
	if err != nil {
		return nil, err
	}

	out := make([]byte, 4+HeaderSize+len(f.Body))
	binary.BigEndian.PutUint32(out[0:4], length)
	out[4] = f.Version
	out[5] = f.Type
	binary.BigEndian.PutUint32(out[6:10], f.RequestID)
	copy(out[10:], f.Body)
	return out, nil
}

func safeUint32(n int) (uint32, error) {
	if n < 0 || n > MaxFrameSize {
		return 0, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, n)
	}
	return uint32(n), nil
}

// DecodeHeader reads the fixed fields from the start of a frame payload - the
// bytes after the length prefix.
func DecodeHeader(payload []byte) (Frame, error) {
	if len(payload) < HeaderSize {
		return Frame{}, fmt.Errorf("%w: %d bytes", ErrShortFrame, len(payload))
	}
	return Frame{
		Version:   payload[0],
		Type:      payload[1],
		RequestID: binary.BigEndian.Uint32(payload[2:6]),
		Body:      payload[HeaderSize:],
	}, nil
}
