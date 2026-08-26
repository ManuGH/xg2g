// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"context"
	"io"
	"time"
)

// LiveSourceFacts is what shared ingest already knows about a stream, so a media
// path does not have to open its own connection to find out.
//
// It is a copy, not a live view: the values are true for the generation named in
// Generation, and a PSI change bumps that number. A consumer that carries facts
// across a generation change is describing two different streams as one.
type LiveSourceFacts struct {
	// Generation increments whenever the PSI describing this stream changes.
	Generation uint64

	HasPAT bool
	HasPMT bool

	VideoPID   uint16
	VideoCodec string

	// ParameterSetsSeen reports whether the decoder configuration for the current
	// codec has been observed - SPS and PPS, plus VPS for HEVC.
	ParameterSetsSeen bool

	// CleanEntryPoints counts entry points whose own access unit carried no
	// scrambled packet. Zero means nothing a decoder could be started on has
	// arrived in the clear.
	CleanEntryPoints uint64

	// CleanAccessUnits counts complete pictures that arrived without an encrypted
	// packet, joinable or not. This is what proves the receiver is descrambling.
	CleanAccessUnits uint64

	ScrambledVideoPackets uint64
	ClearVideoPackets     uint64

	// AudioTracks are the elementary audio streams the PMT names, in the order it
	// names them.
	AudioTracks []LiveAudioTrack
}

// LiveAudioTrack is one audio elementary stream as the PMT declares it.
//
// Everything here is declared, not measured: it comes from the descriptor loop,
// which is why there is no sample rate, no bitrate and no measured channel count.
// A consumer that needs those still has to look at the audio itself.
type LiveAudioTrack struct {
	PID        uint16
	StreamType byte
	Codec      string
	Language   string

	// Channels is the declared channel count where the descriptor names one, and
	// 0 where it does not. Zero means "the stream did not say", which is a
	// different thing from stereo.
	Channels int

	// Multichannel reports a declaration of more than two channels carrying no
	// exact count - an AC-3 service at 5.1 and one at 7.1 declare the same value,
	// so turning this into a number would be an invention.
	Multichannel bool

	// ComponentType is the raw ETSI EN 300 468 component type the declaration was
	// read from, carried through for a consumer that knows what to do with the
	// service-type bits this type deliberately does not interpret.
	ComponentType    uint8
	HasComponentType bool

	// ObservedChannels is the channel count the elementary stream itself has been
	// seen to carry, or 0 where the audio has not established one. It is the only
	// source that can name a count above two - the descriptors above declare a
	// class, and 5.1 and 7.1 declare the same class - and it moves with the audio
	// rather than with the PMT, so it follows a service that changes layout
	// between programmes.
	ObservedChannels int

	// ObservedLFE reports the low frequency effects channel, already counted in
	// ObservedChannels.
	ObservedLFE bool

	// ObservedFrames counts the audio frames the observation rests on, so a
	// consumer can tell a settled reading from a stream that has barely started.
	ObservedFrames uint64

	// ObservedDependentSubstream reports that E-AC-3 dependent substreams were
	// seen. ObservedChannels then describes the independent substream only, and
	// the programme may carry more than it says.
	ObservedDependentSubstream bool
}

// Descrambled reports whether the receiver is delivering this service in the
// clear. It is the question the old TS preflight opened a second connection to
// answer.
func (f LiveSourceFacts) Descrambled() bool {
	return f.CleanAccessUnits > 0
}

// Joinable reports whether a decoder could be started on what has arrived: the
// topology is known, the decoder configuration has been seen, and at least one
// entry point came through unscrambled.
func (f LiveSourceFacts) Joinable() bool {
	return f.HasPAT && f.HasPMT && f.ParameterSetsSeen && f.CleanEntryPoints > 0
}

// LiveSource is one holder's attachment to the shared ingest of a service.
//
// Acquiring it does not open a connection to the receiver on this holder's
// behalf: shared ingest coalesces every holder of the same service onto one
// upstream. Releasing it drops this holder's claim, and the upstream lives on for
// as long as anyone else holds one.
type LiveSource interface {
	// Attach returns the PSI preamble and a reader positioned at a random access
	// point, both taken from the same instant. Reading them separately would let a
	// PSI change land between the two, and the consumer would be handed the
	// topology of one generation with the bytes of another.
	//
	// The reader is this holder's own cursor. Closing it ends this attachment
	// only.
	Attach(ctx context.Context, timeout time.Duration) (preamble []byte, reader io.ReadCloser, err error)

	// Facts reports what the ingest knows about the stream right now.
	Facts() LiveSourceFacts

	// Release drops this holder's claim on the shared session. It is safe to call
	// more than once.
	Release()
}

// LiveSourceProvider hands out attachments to the shared ingest of a service.
//
// A media path that has one has no reason to build a receiver URL: this is the
// only sanctioned way for it to obtain live bytes.
type LiveSourceProvider interface {
	AcquireLiveSource(ctx context.Context, serviceRef string) (LiveSource, error)
}
