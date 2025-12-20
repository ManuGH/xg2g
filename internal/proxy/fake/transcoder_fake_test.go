// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build test

package fake

import (
	"bytes"
	"context"
	"io"
)

// FakeTranscoder is a test double that simulates transcoding behavior
// without actually invoking FFmpeg.
type FakeTranscoder struct {
	Err         error // Error to return from Start
	CopiedBytes int   // Number of bytes successfully "transcoded"
}

// Start simulates transcoding by copying input to output (passthrough).
// Returns FakeTranscoder.Err if set, otherwise copies data.
func (f *FakeTranscoder) Start(ctx context.Context, in io.Reader, out io.Writer) error {
	if f.Err != nil {
		return f.Err
	}

	// Passthrough transcoding (no actual transformation)
	n, err := io.Copy(out, in)
	f.CopiedBytes = int(n)
	return err
}

// FakeProber is a test double that returns predetermined stream metadata.
type FakeProber struct {
	Info StreamInfo
	Err  error
}

// StreamInfo contains metadata about a media stream (test version).
type StreamInfo struct {
	Codec   string
	Bitrate int
}

// Probe returns predetermined metadata without analyzing the stream.
func (f *FakeProber) Probe(_ context.Context, _ io.Reader) (StreamInfo, error) {
	return f.Info, f.Err
}

// NewFakeTranscoderSuccess returns a FakeTranscoder that always succeeds.
func NewFakeTranscoderSuccess() *FakeTranscoder {
	return &FakeTranscoder{}
}

// NewFakeTranscoderError returns a FakeTranscoder that always fails.
func NewFakeTranscoderError(err error) *FakeTranscoder {
	return &FakeTranscoder{Err: err}
}

// Example usage in tests:
//
//	func TestSomething(t *testing.T) {
//	    trans := fake.NewFakeTranscoderSuccess()
//	    in := bytes.NewBufferString("test input")
//	    out := &bytes.Buffer{}
//	    err := trans.Start(context.Background(), in, out)
//	    if err != nil {
//	        t.Fatalf("expected success, got: %v", err)
//	    }
//	    if out.String() != "test input" {
//	        t.Errorf("expected passthrough, got: %s", out.String())
//	    }
//	}

// FakeStreamReader simulates a media stream with predetermined content.
type FakeStreamReader struct {
	*bytes.Buffer
	ReadError error // Error to return on next Read
}

// NewFakeStreamReader creates a fake stream with given content.
func NewFakeStreamReader(content string) *FakeStreamReader {
	return &FakeStreamReader{
		Buffer: bytes.NewBufferString(content),
	}
}

// Read implements io.Reader, returning ReadError if set.
func (f *FakeStreamReader) Read(p []byte) (n int, err error) {
	if f.ReadError != nil {
		return 0, f.ReadError
	}
	return f.Buffer.Read(p)
}
