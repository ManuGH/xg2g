// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package middleware

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
)

type RuntimeMode uint8

const (
	RuntimeDisabled RuntimeMode = iota
	RuntimeEnforced
)

type deadlineStateKey struct{}

// DeadlineState is owned by the request-lifecycle middleware.
type DeadlineState struct {
	Policy   deadline.RoutePolicy
	Timeouts deadline.DeadlineTimeouts
	Mode     RuntimeMode

	bound     atomic.Bool
	hijacked  atomic.Bool
	supported atomic.Bool

	controller *http.ResponseController
	now        func() time.Time

	mu              sync.Mutex
	lastErr         error
	currentDeadline time.Time
}

func (s *DeadlineState) BindPolicy(policy deadline.RoutePolicy) error {
	if s == nil {
		return fmt.Errorf("nil DeadlineState")
	}
	if policy.Class == deadline.RouteDeadlineUnknown || policy.Class > deadline.RouteDeadlineStreaming {
		return fmt.Errorf("invalid route deadline class %s", policy.Class)
	}
	if !s.bound.CompareAndSwap(false, true) {
		return fmt.Errorf("policy already bound for request")
	}
	s.Policy = policy
	return s.refreshWriteDeadline(s.timeoutForPolicy())
}

func (s *DeadlineState) IsBound() bool {
	return s != nil && s.bound.Load()
}

func (s *DeadlineState) SetHijacked() {
	if s != nil {
		s.hijacked.Store(true)
	}
}

func (s *DeadlineState) IsHijacked() bool {
	return s != nil && s.hijacked.Load()
}

func (s *DeadlineState) DeadlineSupported() bool {
	return s != nil && s.supported.Load()
}

func (s *DeadlineState) timeoutForPolicy() time.Duration {
	switch s.Policy.Class {
	case deadline.RouteDeadlineAPIBounded:
		return s.Timeouts.APIWriteTimeout
	case deadline.RouteDeadlineMediaBounded:
		return s.Timeouts.MediaWriteTimeout
	case deadline.RouteDeadlineStreaming:
		return s.Timeouts.StreamingIdleTimeout
	default:
		return 0
	}
}

func (s *DeadlineState) setLastError(err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

func (s *DeadlineState) getLastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *DeadlineState) setCurrentDeadline(value time.Time) {
	s.mu.Lock()
	s.currentDeadline = value
	s.mu.Unlock()
}

func (s *DeadlineState) deadlineExpired() bool {
	s.mu.Lock()
	value := s.currentDeadline
	s.mu.Unlock()
	if value.IsZero() {
		return false
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	return !now().Before(value)
}

func (s *DeadlineState) refreshWriteDeadline(timeout time.Duration) error {
	if s == nil {
		return fmt.Errorf("nil DeadlineState")
	}
	if s.controller == nil {
		return fmt.Errorf("deadline response controller missing")
	}
	if timeout <= 0 {
		return fmt.Errorf("invalid route write timeout %v", timeout)
	}
	if s.IsHijacked() || !s.supported.Load() {
		return nil
	}

	now := time.Now
	if s.now != nil {
		now = s.now
	}
	value := now().Add(timeout)
	err := s.controller.SetWriteDeadline(value)
	if errors.Is(err, http.ErrNotSupported) {
		// Direct unit tests and in-process router probes commonly use a
		// ResponseRecorder, which has no network connection. Real net/http
		// HTTP/1.x and HTTP/2 writers are covered by integration probes.
		s.supported.Store(false)
		return nil
	}
	if err != nil {
		s.setLastError(err)
		return fmt.Errorf("set route write deadline: %w", err)
	}
	s.setCurrentDeadline(value)
	return nil
}

func (s *DeadlineState) beforeWrite() error {
	if s == nil || !s.IsBound() || s.IsHijacked() {
		return nil
	}
	if err := s.getLastError(); err != nil {
		return err
	}
	if s.Policy.Class != deadline.RouteDeadlineStreaming {
		return nil
	}
	return s.refreshWriteDeadline(s.Timeouts.StreamingIdleTimeout)
}

func (s *DeadlineState) resetWriteDeadline() error {
	if s == nil || !s.IsBound() || s.IsHijacked() || !s.supported.Load() {
		return nil
	}
	if s.controller == nil {
		return fmt.Errorf("deadline response controller missing")
	}
	// Once a deadline has expired, Go cannot safely extend it. Clearing it here
	// would also allow data buffered after expiry to escape during the server's
	// final response flush.
	if s.deadlineExpired() {
		return nil
	}
	err := s.controller.SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		s.supported.Store(false)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reset route write deadline: %w", err)
	}
	s.setCurrentDeadline(time.Time{})
	return nil
}

func DeadlineStateFromContext(ctx context.Context) (*DeadlineState, bool) {
	state, ok := ctx.Value(deadlineStateKey{}).(*DeadlineState)
	return state, ok
}

// WithRoutePolicy is functionally passive in RuntimeDisabled mode.
func WithRoutePolicy(policy deadline.RoutePolicy, mode RuntimeMode) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == RuntimeDisabled {
				next.ServeHTTP(w, r)
				return
			}

			state, ok := DeadlineStateFromContext(r.Context())
			if !ok {
				http.Error(w, "deadline state missing from request context", http.StatusInternalServerError)
				return
			}
			if err := state.BindPolicy(policy); err != nil {
				http.Error(w, "failed to bind deadline policy", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type deadlineResponseWriter struct {
	http.ResponseWriter
	state *DeadlineState
}

func (w *deadlineResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *deadlineResponseWriter) Write(p []byte) (int, error) {
	if err := w.state.beforeWrite(); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(p)
}

// ReadFrom deliberately routes every chunk through Write. This prevents
// io.Copy from bypassing rolling streaming-deadline renewal through an
// underlying io.ReaderFrom implementation.
func (w *deadlineResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	const copyBufferSize = 32 * 1024
	buf := make([]byte, copyBufferSize)
	return io.CopyBuffer(struct{ io.Writer }{Writer: w}, r, buf)
}

func (w *deadlineResponseWriter) flushError() error {
	if err := w.state.beforeWrite(); err != nil {
		return err
	}
	if flusher, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		return flusher.FlushError()
	}
	w.ResponseWriter.(http.Flusher).Flush()
	return nil
}

func (w *deadlineResponseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.state == nil {
		return nil, nil, fmt.Errorf("deadline state missing during hijack")
	}
	if w.state.IsBound() && !w.state.Policy.MayUpgradePerRequest {
		return nil, nil, fmt.Errorf("route policy does not permit connection upgrade")
	}
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("clear write deadline after hijack: %w", err)
	}
	w.state.SetHijacked()
	return conn, rw, nil
}

func (w *deadlineResponseWriter) push(target string, opts *http.PushOptions) error {
	return w.ResponseWriter.(http.Pusher).Push(target, opts)
}

type deadlineWriterH struct{ *deadlineResponseWriter }

func (w *deadlineWriterH) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type deadlineWriterF struct{ *deadlineResponseWriter }

func (w *deadlineWriterF) Flush() {
	_ = w.flushError()
}

func (w *deadlineWriterF) FlushError() error {
	return w.flushError()
}

type deadlineWriterP struct{ *deadlineResponseWriter }

func (w *deadlineWriterP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type deadlineWriterHF struct{ *deadlineResponseWriter }

func (w *deadlineWriterHF) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

func (w *deadlineWriterHF) Flush() {
	_ = w.flushError()
}

func (w *deadlineWriterHF) FlushError() error {
	return w.flushError()
}

type deadlineWriterHP struct{ *deadlineResponseWriter }

func (w *deadlineWriterHP) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

func (w *deadlineWriterHP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type deadlineWriterFP struct{ *deadlineResponseWriter }

func (w *deadlineWriterFP) Flush() {
	_ = w.flushError()
}

func (w *deadlineWriterFP) FlushError() error {
	return w.flushError()
}

func (w *deadlineWriterFP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type deadlineWriterHFP struct{ *deadlineResponseWriter }

func (w *deadlineWriterHFP) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

func (w *deadlineWriterHFP) Flush() {
	_ = w.flushError()
}

func (w *deadlineWriterHFP) FlushError() error {
	return w.flushError()
}

func (w *deadlineWriterHFP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

func wrapDeadlineResponseWriter(w http.ResponseWriter, state *DeadlineState, protoMajor int) http.ResponseWriter {
	base := &deadlineResponseWriter{ResponseWriter: w, state: state}
	mask := 0
	if _, ok := w.(http.Hijacker); ok && protoMajor == 1 {
		mask |= 1
	}
	if _, ok := w.(http.Flusher); ok {
		mask |= 2
	}
	if _, ok := w.(http.Pusher); ok {
		mask |= 4
	}
	switch mask {
	case 1:
		return &deadlineWriterH{base}
	case 2:
		return &deadlineWriterF{base}
	case 3:
		return &deadlineWriterHF{base}
	case 4:
		return &deadlineWriterP{base}
	case 5:
		return &deadlineWriterHP{base}
	case 6:
		return &deadlineWriterFP{base}
	case 7:
		return &deadlineWriterHFP{base}
	default:
		return base
	}
}

// WriteTimeoutMiddleware establishes the request-owned deadline state and
// network-side writer. It is intentionally outermost so compression and
// response capture write through it and reset finishes after they return.
func WriteTimeoutMiddleware(timeouts deadline.DeadlineTimeouts, mode RuntimeMode) func(http.Handler) http.Handler {
	validationErr := timeouts.Validate()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == RuntimeDisabled {
				next.ServeHTTP(w, r)
				return
			}
			if validationErr != nil {
				http.Error(w, "invalid route deadline configuration", http.StatusInternalServerError)
				return
			}
			state := &DeadlineState{
				Timeouts:   timeouts,
				Mode:       mode,
				controller: http.NewResponseController(w),
				now:        time.Now,
			}
			state.supported.Store(true)
			ctx := context.WithValue(r.Context(), deadlineStateKey{}, state)
			wrapped := wrapDeadlineResponseWriter(w, state, r.ProtoMajor)
			defer func() {
				_ = state.resetWriteDeadline()
			}()
			next.ServeHTTP(wrapped, r.WithContext(ctx))
		})
	}
}
