package ffmpeg

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// Upper bound on how long a receiver connection may sit without anybody reading
// from it before it is closed regardless of its context.
//
// A live transport stream is delivered continuously, so a consumer that has gone
// away shows up here within seconds. The bound is set far above the delivery
// gaps the receiver itself produces — a Vu+ streamserver was measured bursting
// with pauses up to 888 ms between chunks — so a healthy session never
// approaches it. It exists for the case where the session is already gone.
const receiverIdleTimeout = 30 * time.Second

// How often the watchdog checks. Coarse on purpose: this decides when a tuner
// comes back, not when a frame is shown.
const receiverIdleCheckInterval = 5 * time.Second

// guardedStreamBody bounds the lifetime of a receiver connection.
//
// Every connection to the receiver's streaming port holds a tuner for as long as
// it is open. Closing them was tied solely to the ffmpeg session context:
//
//	go func() { <-ctx.Done(); _ = resp.Body.Close() }()
//
// which is correct while that context is cancelled, and unrecoverable when it is
// not. An orphaned session left two connections open for 14 hours; with the
// receiver's FBC tuners linked to one transponder, that took the whole
// transponder out — every request for a service on it was answered
// "400 Bad Request", and the cause was invisible from the receiver's side.
//
// So the context stays as the normal path and this is the floor underneath it:
// no read for receiverIdleTimeout means nothing is consuming this stream any
// more, and the tuner is given back. Closing is idempotent, so the context path
// and the watchdog cannot fight each other.
type guardedStreamBody struct {
	body     io.ReadCloser
	lastRead atomic.Int64 // UnixNano of the last successful read

	closeOnce sync.Once
	done      chan struct{}

	sessionID string
	mode      string
	opened    time.Time

	// Held per instance rather than read from package state: the watchdog runs
	// on its own goroutine, and anything it reads from a mutable global is a
	// data race the moment a test — or a future config knob — writes it.
	idleTimeout   time.Duration
	checkInterval time.Duration
}

// newGuardedStreamBody wraps body and starts the watchdog. The caller keeps
// closing it on context cancellation as before; this only adds a lower bound on
// how long a forgotten connection can survive.
//
// `mode` matches the label used for the ActiveEnigma2Connections gauge, so the
// metric is decremented exactly once whichever path closes the body.
func newGuardedStreamBody(body io.ReadCloser, sessionID, mode string) *guardedStreamBody {
	return newGuardedStreamBodyWithTimeouts(body, sessionID, mode, receiverIdleTimeout, receiverIdleCheckInterval)
}

// newGuardedStreamBodyWithTimeouts is the injectable form, so the watchdog can
// be exercised without a 30 s wall clock.
func newGuardedStreamBodyWithTimeouts(
	body io.ReadCloser,
	sessionID, mode string,
	idleTimeout, checkInterval time.Duration,
) *guardedStreamBody {
	g := &guardedStreamBody{
		body:          body,
		done:          make(chan struct{}),
		sessionID:     sessionID,
		mode:          mode,
		opened:        time.Now(),
		idleTimeout:   idleTimeout,
		checkInterval: checkInterval,
	}
	g.lastRead.Store(time.Now().UnixNano())
	go g.watch()
	return g
}

func (g *guardedStreamBody) Read(p []byte) (int, error) {
	n, err := g.body.Read(p)
	if n > 0 {
		g.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

// Close is safe to call from the context goroutine, the watchdog and the
// consumer, in any order and any number of times.
func (g *guardedStreamBody) Close() error {
	return g.closeReason("consumer")
}

func (g *guardedStreamBody) closeReason(reason string) error {
	var err error
	g.closeOnce.Do(func() {
		close(g.done)
		err = g.body.Close()
		metrics.DecActiveEnigma2Connections(g.mode)
		log.L().Info().
			Str("session_id", g.sessionID).
			Str("mode", g.mode).
			Str("reason", reason).
			Dur("open_for", time.Since(g.opened)).
			Msg("receiver stream connection closed")
	})
	return err
}

func (g *guardedStreamBody) watch() {
	ticker := time.NewTicker(g.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.done:
			return
		case now := <-ticker.C:
			idle := now.Sub(time.Unix(0, g.lastRead.Load()))
			if idle < g.idleTimeout {
				continue
			}
			// Loud on purpose. Reaching this means a session was orphaned
			// somewhere upstream: the tuner is recovered here, but the leak that
			// stranded it is still a defect worth finding.
			log.L().Warn().
				Str("session_id", g.sessionID).
				Str("mode", g.mode).
				Dur("idle_for", idle).
				Dur("open_for", time.Since(g.opened)).
				Msg("receiver stream idle with no consumer, closing to release the tuner")
			metrics.IncReceiverStreamReaped(g.mode)
			_ = g.closeReason("idle_watchdog")
			return
		}
	}
}
