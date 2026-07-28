package v3

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseW is a TRULY minimal ResponseWriter (no Flusher, etc.)
type baseW struct{ http.ResponseWriter }

// Capability mocks
type mRF struct {
	baseW
}

func (m mRF) ReadFrom(r io.Reader) (int64, error) { return io.Copy(m.baseW, r) }

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
		_, err := w.Write([]byte("hello"))
		require.NoError(t, err)
		assert.True(t, tracker.WroteHeader())
		assert.Equal(t, int64(5), tracker.(StatusTracker).BytesWritten())
	})

	t.Run("ReadFrom", func(t *testing.T) {
		underlying := mRF{baseW{httptest.NewRecorder()}}
		w, tracker := wrapResponseWriter(underlying)
		assert.False(t, tracker.WroteHeader())

		rf := w.(io.ReaderFrom)
		_, err := rf.ReadFrom(strings.NewReader("bulk data"))
		require.NoError(t, err)
		assert.True(t, tracker.WroteHeader())
		assert.Equal(t, int64(9), tracker.(StatusTracker).BytesWritten())
	})

	t.Run("Unwrap", func(t *testing.T) {
		rec := httptest.NewRecorder()
		w, _ := wrapResponseWriter(rec)

		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		require.True(t, ok)
		assert.Equal(t, rec, unwrapper.Unwrap())
	})
}

func TestV3ResponseCapturer_ResponseControllerPassThrough(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var capturerActive bool
	var capturedStatus int
	var capturedBytes int64

	// Middleware chain: outer chi WrapResponseWriter + v3 wrapResponseWriter
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Outer chi WrapResponseWriter (simulates outer router)
		chiWrapped := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		// 2. v3 Response Capturer (simulates exposureSecurityMiddleware audit wrapping)
		v3Wrapped, tracker := wrapResponseWriter(chiWrapped)
		require.NotNil(t, tracker)

		// 3. ResponseController lookup from inner handler
		rc := http.NewResponseController(v3Wrapped)
		require.NotNil(t, rc)

		// Empirical check 1: SetWriteDeadline (+1h)
		err := rc.SetWriteDeadline(time.Now().Add(1 * time.Hour))
		assert.NoError(t, err, "SetWriteDeadline(+1h) must succeed through chi + v3 ResponseCapturer on real TCP conn")

		// Empirical check 2: SetWriteDeadline (clear deadline)
		err = rc.SetWriteDeadline(time.Time{})
		assert.NoError(t, err, "SetWriteDeadline(zero) must succeed through chi + v3 ResponseCapturer on real TCP conn")

		// Empirical check 3: Flush
		err = rc.Flush()
		assert.NoError(t, err, "Flush must succeed through chi + v3 ResponseCapturer on real TCP conn")

		payload := []byte("v3-empirical-ok")
		v3Wrapped.WriteHeader(http.StatusOK)
		_, err = v3Wrapped.Write(payload)
		assert.NoError(t, err)

		// Observable verification: Prove wrapResponseWriter was installed, active, and captured stats
		capturerActive = tracker.WroteHeader()
		if st, ok := tracker.(StatusTracker); ok {
			capturedStatus = st.StatusCode()
			capturedBytes = st.BytesWritten()
		}
	})

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	defer srv.Close()

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "v3-empirical-ok", string(body))

	// Assert capturer observable state
	assert.True(t, capturerActive, "wrapResponseWriter must have recorded WroteHeader = true")
	assert.Equal(t, http.StatusOK, capturedStatus, "wrapResponseWriter must have recorded status 200")
	assert.Equal(t, int64(15), capturedBytes, "wrapResponseWriter must have recorded 15 bytes written")
}

func TestV3ResponseCapturer_WithCompressionPassThrough(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	activeCh := make(chan bool, 1)

	// Middleware chain with compression: chi.Compress + chi.WrapResponseWriter + v3 wrapResponseWriter
	compressMw := chimw.Compress(5)
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chiWrapped := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		v3Wrapped, tracker := wrapResponseWriter(chiWrapped)
		require.NotNil(t, tracker)

		rc := http.NewResponseController(v3Wrapped)
		require.NotNil(t, rc)

		v3Wrapped.Header().Set("Content-Type", "text/plain")

		err := rc.SetWriteDeadline(time.Now().Add(1 * time.Hour))
		assert.NoError(t, err, "SetWriteDeadline(+1h) must succeed through compression + chi + v3 ResponseCapturer")

		err = rc.SetWriteDeadline(time.Time{})
		assert.NoError(t, err, "SetWriteDeadline(zero) must succeed through compression + chi + v3 ResponseCapturer")

		payload := []byte(strings.Repeat("compressible-payload-data-stream-content-for-testing-", 40))
		_, err = v3Wrapped.Write(payload)
		assert.NoError(t, err)

		err = rc.Flush()
		assert.NoError(t, err, "Flush must succeed through compression + chi + v3 ResponseCapturer")

		activeCh <- tracker.WroteHeader()
	})

	handler := compressMw(innerHandler)
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	defer srv.Close()

	req, err := http.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), "response must carry Content-Encoding: gzip header")

	gzReader, err := gzip.NewReader(resp.Body)
	require.NoError(t, err, "response body must be valid gzip compressed stream")
	defer gzReader.Close()

	decompressedBody, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("compressible-payload-data-stream-content-for-testing-", 40), string(decompressedBody))

	// Layering assertion: wrapResponseWriter wraps w INSIDE the handler BEFORE chi's compressResponseWriter.
	// Therefore, tracker.BytesWritten() records uncompressed payload bytes written by the inner handler (2120 bytes),
	// while the wire response is gzip-compressed.
	capturerActive := <-activeCh
	assert.True(t, capturerActive, "wrapResponseWriter must have recorded WroteHeader = true with compression active")
}
