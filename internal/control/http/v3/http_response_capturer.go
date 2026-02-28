package v3

import (
	"io"
	"net/http"
	"sync/atomic"
)

// Purpose: Tracks header write + provides truthful optional interface passthrough.
// HeaderTracker provides a read-only view of whether headers have been written.
type HeaderTracker interface {
	WroteHeader() bool
}

// StatusTracker provides a read-only view of the final HTTP status code.
type StatusTracker interface {
	StatusCode() int
}

// baseResponseWriter is the core wrapper that tracks if headers were written.
type baseResponseWriter struct {
	http.ResponseWriter
	wrote  atomic.Bool
	status atomic.Int64
}

func (b *baseResponseWriter) WroteHeader() bool {
	return b.wrote.Load()
}

func (b *baseResponseWriter) StatusCode() int {
	v := int(b.status.Load())
	if v > 0 {
		return v
	}
	return http.StatusOK
}

func (b *baseResponseWriter) markWrite() {
	b.wrote.Store(true)
	if b.status.Load() == 0 {
		b.status.Store(http.StatusOK)
	}
}

func (b *baseResponseWriter) WriteHeader(code int) {
	b.wrote.Store(true)
	b.status.Store(int64(code))
	b.ResponseWriter.WriteHeader(code)
}

func (b *baseResponseWriter) Write(p []byte) (int, error) {
	b.markWrite()
	return b.ResponseWriter.Write(p)
}

func (b *baseResponseWriter) Unwrap() http.ResponseWriter {
	return b.ResponseWriter
}

// Concrete Combinatorial Wrapper Types
// Naming: bw[RF][H][F][P] where RF=ReaderFrom, H=Hijacker, F=Flusher, P=Pusher

type bw struct{ *baseResponseWriter }

type bwRF struct {
	*baseResponseWriter
	io.ReaderFrom
}

func (b *bwRF) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwH struct {
	*baseResponseWriter
	http.Hijacker
}

type bwF struct {
	*baseResponseWriter
	http.Flusher
}

type bwP struct {
	*baseResponseWriter
	http.Pusher
}

type bwRF_H struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Hijacker
}

func (b *bwRF_H) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_F struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Flusher
}

func (b *bwRF_F) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_P struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Pusher
}

func (b *bwRF_P) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwH_F struct {
	*baseResponseWriter
	http.Hijacker
	http.Flusher
}

type bwH_P struct {
	*baseResponseWriter
	http.Hijacker
	http.Pusher
}

type bwF_P struct {
	*baseResponseWriter
	http.Flusher
	http.Pusher
}

type bwRF_H_F struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Hijacker
	http.Flusher
}

func (b *bwRF_H_F) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_H_P struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Hijacker
	http.Pusher
}

func (b *bwRF_H_P) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_F_P struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Flusher
	http.Pusher
}

func (b *bwRF_F_P) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

type bwH_F_P struct {
	*baseResponseWriter
	http.Hijacker
	http.Flusher
	http.Pusher
}

type bwRF_H_F_P struct {
	*baseResponseWriter
	io.ReaderFrom
	http.Hijacker
	http.Flusher
	http.Pusher
}

func (b *bwRF_H_F_P) ReadFrom(r io.Reader) (int64, error) {
	b.markWrite()
	return b.ReaderFrom.ReadFrom(r)
}

// wrapResponseWriter detects capabilities of w and returns a truthful wrapper.
func wrapResponseWriter(w http.ResponseWriter) (http.ResponseWriter, HeaderTracker) {
	base := &baseResponseWriter{ResponseWriter: w}

	rf, isRF := w.(io.ReaderFrom)
	h, isH := w.(http.Hijacker)
	f, isF := w.(http.Flusher)
	p, isP := w.(http.Pusher)

	// Bitmask: bit 0=RF, bit 1=H, bit 2=F, bit 3=P
	mask := 0
	if isRF {
		mask |= 1 << 0
	}
	if isH {
		mask |= 1 << 1
	}
	if isF {
		mask |= 1 << 2
	}
	if isP {
		mask |= 1 << 3
	}

	switch mask {
	case 0: // None
		return &bw{base}, base
	case 1: // RF
		return &bwRF{base, rf}, base
	case 2: // H
		return &bwH{base, h}, base
	case 3: // RF + H
		return &bwRF_H{base, rf, h}, base
	case 4: // F
		return &bwF{base, f}, base
	case 5: // RF + F
		return &bwRF_F{base, rf, f}, base
	case 6: // H + F
		return &bwH_F{base, h, f}, base
	case 7: // RF + H + F
		return &bwRF_H_F{base, rf, h, f}, base
	case 8: // P
		return &bwP{base, p}, base
	case 9: // RF + P
		return &bwRF_P{base, rf, p}, base
	case 10: // H + P
		return &bwH_P{base, h, p}, base
	case 11: // RF + H + P
		return &bwRF_H_P{base, rf, h, p}, base
	case 12: // F + P
		return &bwF_P{base, f, p}, base
	case 13: // RF + F + P
		return &bwRF_F_P{base, rf, f, p}, base
	case 14: // H + F + P
		return &bwH_F_P{base, h, f, p}, base
	case 15: // RF + H + F + P
		return &bwRF_H_F_P{base, rf, h, f, p}, base
	default:
		return &bw{base}, base
	}
}
