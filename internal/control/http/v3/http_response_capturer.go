package v3

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync/atomic"
)

// HeaderTracker provides a read-only view of whether headers have been written.
type HeaderTracker interface {
	WroteHeader() bool
}

// bwBase is the core wrapper that tracks if headers were written.
type bwBase struct {
	http.ResponseWriter
	wrote atomic.Bool
}

func (b *bwBase) WroteHeader() bool {
	return b.wrote.Load()
}

func (b *bwBase) WriteHeader(code int) {
	b.wrote.Store(true)
	b.ResponseWriter.WriteHeader(code)
}

func (b *bwBase) Write(p []byte) (int, error) {
	b.wrote.Store(true)
	return b.ResponseWriter.Write(p)
}

func (b *bwBase) Unwrap() http.ResponseWriter {
	return b.ResponseWriter
}

// Concrete Combinatorial Wrapper Types
// Naming: bw[RF][H][F][P] where RF=ReaderFrom, H=Hijacker, F=Flusher, P=Pusher

type bw struct{ *bwBase }

type bwRF struct {
	*bwBase
	io.ReaderFrom
}

func (b *bwRF) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwH struct {
	*bwBase
	http.Hijacker
}

type bwF struct {
	*bwBase
	http.Flusher
}

type bwP struct {
	*bwBase
	http.Pusher
}

type bwRF_H struct {
	*bwBase
	io.ReaderFrom
	http.Hijacker
}

func (b *bwRF_H) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_F struct {
	*bwBase
	io.ReaderFrom
	http.Flusher
}

func (b *bwRF_F) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_P struct {
	*bwBase
	io.ReaderFrom
	http.Pusher
}

func (b *bwRF_P) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwH_F struct {
	*bwBase
	http.Hijacker
	http.Flusher
}

type bwH_P struct {
	*bwBase
	http.Hijacker
	http.Pusher
}

type bwF_P struct {
	*bwBase
	http.Flusher
	http.Pusher
}

type bwRF_H_F struct {
	*bwBase
	io.ReaderFrom
	http.Hijacker
	http.Flusher
}

func (b *bwRF_H_F) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_H_P struct {
	*bwBase
	io.ReaderFrom
	http.Hijacker
	http.Pusher
}

func (b *bwRF_H_P) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwRF_F_P struct {
	*bwBase
	io.ReaderFrom
	http.Flusher
	http.Pusher
}

func (b *bwRF_F_P) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

type bwH_F_P struct {
	*bwBase
	http.Hijacker
	http.Flusher
	http.Pusher
}

type bwRF_H_F_P struct {
	*bwBase
	io.ReaderFrom
	http.Hijacker
	http.Flusher
	http.Pusher
}

func (b *bwRF_H_F_P) ReadFrom(r io.Reader) (int64, error) {
	b.wrote.Store(true)
	return b.ReaderFrom.ReadFrom(r)
}

// wrapResponseWriter detects capabilities of w and returns a truthful wrapper.
func wrapResponseWriter(w http.ResponseWriter) (http.ResponseWriter, HeaderTracker) {
	base := &bwBase{ResponseWriter: w}

	rf, isRF := w.(io.ReaderFrom)
	h, isH := w.(http.Hijacker)
	f, isF := w.(http.Flusher)
	p, isP := w.(http.Pusher)

	// Bitmask: RF=0, H=1, F=2, P=3
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

// Hijack implementation for types that embed http.Hijacker
func (b *bwH) Hijack() (net.Conn, *bufio.ReadWriter, error)        { return b.Hijacker.Hijack() }
func (b *bwRF_H) Hijack() (net.Conn, *bufio.ReadWriter, error)     { return b.Hijacker.Hijack() }
func (b *bwH_F) Hijack() (net.Conn, *bufio.ReadWriter, error)      { return b.Hijacker.Hijack() }
func (b *bwH_P) Hijack() (net.Conn, *bufio.ReadWriter, error)      { return b.Hijacker.Hijack() }
func (b *bwRF_H_F) Hijack() (net.Conn, *bufio.ReadWriter, error)   { return b.Hijacker.Hijack() }
func (b *bwRF_H_P) Hijack() (net.Conn, *bufio.ReadWriter, error)   { return b.Hijacker.Hijack() }
func (b *bwH_F_P) Hijack() (net.Conn, *bufio.ReadWriter, error)    { return b.Hijacker.Hijack() }
func (b *bwRF_H_F_P) Hijack() (net.Conn, *bufio.ReadWriter, error) { return b.Hijacker.Hijack() }

// Flush implementation for types that embed http.Flusher
func (b *bwF) Flush()        { b.Flusher.Flush() }
func (b *bwH_F) Flush()      { b.Flusher.Flush() }
func (b *bwF_P) Flush()      { b.Flusher.Flush() }
func (b *bwRF_F) Flush()     { b.Flusher.Flush() }
func (b *bwRF_H_F) Flush()   { b.Flusher.Flush() }
func (b *bwRF_F_P) Flush()   { b.Flusher.Flush() }
func (b *bwH_F_P) Flush()    { b.Flusher.Flush() }
func (b *bwRF_H_F_P) Flush() { b.Flusher.Flush() }

// Push implementation for types that embed http.Pusher
func (b *bwP) Push(t string, o *http.PushOptions) error        { return b.Pusher.Push(t, o) }
func (b *bwRF_P) Push(t string, o *http.PushOptions) error     { return b.Pusher.Push(t, o) }
func (b *bwH_P) Push(t string, o *http.PushOptions) error      { return b.Pusher.Push(t, o) }
func (b *bwF_P) Push(t string, o *http.PushOptions) error      { return b.Pusher.Push(t, o) }
func (b *bwRF_H_P) Push(t string, o *http.PushOptions) error   { return b.Pusher.Push(t, o) }
func (b *bwRF_F_P) Push(t string, o *http.PushOptions) error   { return b.Pusher.Push(t, o) }
func (b *bwH_F_P) Push(t string, o *http.PushOptions) error    { return b.Pusher.Push(t, o) }
func (b *bwRF_H_F_P) Push(t string, o *http.PushOptions) error { return b.Pusher.Push(t, o) }
