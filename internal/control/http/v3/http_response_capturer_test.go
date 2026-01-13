package v3

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseW is a TRULY minimal ResponseWriter (no Flusher, etc.)
type baseW struct{ http.ResponseWriter }

// Capability mocks
type mRF struct {
	baseW
}

func (m mRF) ReadFrom(r io.Reader) (int64, error) { return 0, nil }

type mH struct {
	baseW
}

func (m mH) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

type mF struct {
	baseW
}

func (m mF) Flush() {}

type mP struct {
	baseW
}

func (m mP) Push(string, *http.PushOptions) error { return nil }

type mRF_H struct {
	baseW
}

func (m mRF_H) ReadFrom(r io.Reader) (int64, error)          { return 0, nil }
func (m mRF_H) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

type mRF_F struct {
	baseW
}

func (m mRF_F) ReadFrom(r io.Reader) (int64, error) { return 0, nil }
func (m mRF_F) Flush()                              {}

type mRF_P struct {
	baseW
}

func (m mRF_P) ReadFrom(r io.Reader) (int64, error)  { return 0, nil }
func (m mRF_P) Push(string, *http.PushOptions) error { return nil }

type mH_F struct {
	baseW
}

func (m mH_F) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mH_F) Flush()                                       {}

type mH_P struct {
	baseW
}

func (m mH_P) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mH_P) Push(string, *http.PushOptions) error         { return nil }

type mF_P struct {
	baseW
}

func (m mF_P) Flush()                               {}
func (m mF_P) Push(string, *http.PushOptions) error { return nil }

type mRF_H_F struct {
	baseW
}

func (m mRF_H_F) ReadFrom(r io.Reader) (int64, error)          { return 0, nil }
func (m mRF_H_F) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mRF_H_F) Flush()                                       {}

type mRF_H_P struct {
	baseW
}

func (m mRF_H_P) ReadFrom(r io.Reader) (int64, error)          { return 0, nil }
func (m mRF_H_P) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mRF_H_P) Push(string, *http.PushOptions) error         { return nil }

type mRF_F_P struct {
	baseW
}

func (m mRF_F_P) ReadFrom(r io.Reader) (int64, error)  { return 0, nil }
func (m mRF_F_P) Flush()                               {}
func (m mRF_F_P) Push(string, *http.PushOptions) error { return nil }

type mH_F_P struct {
	baseW
}

func (m mH_F_P) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mH_F_P) Flush()                                       {}
func (m mH_F_P) Push(string, *http.PushOptions) error         { return nil }

type mRF_H_F_P struct {
	baseW
}

func (m mRF_H_F_P) ReadFrom(r io.Reader) (int64, error)          { return 0, nil }
func (m mRF_H_F_P) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (m mRF_H_F_P) Flush()                                       {}
func (m mRF_H_F_P) Push(string, *http.PushOptions) error         { return nil }

func TestWriterTransparent_TruthTable(t *testing.T) {
	bw := baseW{httptest.NewRecorder()}
	tests := []struct {
		name string
		w    http.ResponseWriter
		rf   bool
		h    bool
		f    bool
		p    bool
	}{
		{"none", bw, false, false, false, false},
		{"rf", mRF{bw}, true, false, false, false},
		{"h", mH{bw}, false, true, false, false},
		{"rf_h", mRF_H{bw}, true, true, false, false},
		{"f", mF{bw}, false, false, true, false},
		{"rf_f", mRF_F{bw}, true, false, true, false},
		{"h_f", mH_F{bw}, false, true, true, false},
		{"rf_h_f", mRF_H_F{bw}, true, true, true, false},
		{"p", mP{bw}, false, false, false, true},
		{"rf_p", mRF_P{bw}, true, false, false, true},
		{"h_p", mH_P{bw}, false, true, false, true},
		{"rf_h_p", mRF_H_P{bw}, true, true, false, true},
		{"f_p", mF_P{bw}, false, false, true, true},
		{"rf_f_p", mRF_F_P{bw}, true, false, true, true},
		{"h_f_p", mH_F_P{bw}, false, true, true, true},
		{"rf_h_f_p", mRF_H_F_P{bw}, true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := wrapResponseWriter(tt.w)

			_, okRF := w.(io.ReaderFrom)
			assert.Equal(t, tt.rf, okRF, "io.ReaderFrom truth mismatch")

			_, okH := w.(http.Hijacker)
			assert.Equal(t, tt.h, okH, "http.Hijacker truth mismatch")

			_, okF := w.(http.Flusher)
			assert.Equal(t, tt.f, okF, "http.Flusher truth mismatch")

			_, okP := w.(http.Pusher)
			assert.Equal(t, tt.p, okP, "http.Pusher truth mismatch")
		})
	}
}

func TestWriterTransparent_StatusTracking(t *testing.T) {
	t.Run("WriteHeader", func(t *testing.T) {
		w, tracker := wrapResponseWriter(httptest.NewRecorder())
		assert.False(t, tracker.WroteHeader())
		w.WriteHeader(http.StatusNoContent)
		assert.True(t, tracker.WroteHeader())
	})

	t.Run("Write", func(t *testing.T) {
		w, tracker := wrapResponseWriter(httptest.NewRecorder())
		assert.False(t, tracker.WroteHeader())
		w.Write([]byte("hello"))
		assert.True(t, tracker.WroteHeader())
	})

	t.Run("ReadFrom", func(t *testing.T) {
		underlying := mRF{baseW{httptest.NewRecorder()}}
		w, tracker := wrapResponseWriter(underlying)
		assert.False(t, tracker.WroteHeader())

		rf := w.(io.ReaderFrom)
		rf.ReadFrom(strings.NewReader("bulk data"))
		assert.True(t, tracker.WroteHeader())
	})

	t.Run("Unwrap", func(t *testing.T) {
		rec := httptest.NewRecorder()
		w, _ := wrapResponseWriter(rec)

		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		require.True(t, ok)
		assert.Equal(t, rec, unwrapper.Unwrap())
	})
}
