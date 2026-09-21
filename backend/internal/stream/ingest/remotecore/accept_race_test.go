// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

type controlledListener struct {
	conn       net.Conn
	acceptedCh chan struct{}
	closeOnce  sync.Once
	closed     atomic.Bool
}

func (l *controlledListener) Accept() (net.Conn, error) {
	<-l.acceptedCh
	return l.conn, nil
}

func (l *controlledListener) Close() error {
	l.closed.Store(true)
	l.closeOnce.Do(func() {
		select {
		case <-l.acceptedCh:
		default:
			close(l.acceptedCh)
		}
	})
	return nil
}

func (l *controlledListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func TestRemoteCore_Accept_ClosesAcceptedConnOnWaitDoneRace(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() {
		_ = serverEnd.Close()
		_ = clientEnd.Close()
	})

	ln := &controlledListener{
		conn:       serverEnd,
		acceptedCh: make(chan struct{}),
	}

	waitDone := make(chan struct{})
	close(waitDone) // Already finished / crashed

	r := &RemoteCore{
		ln:       ln,
		waitDone: waitDone,
		waitErr:  errors.New("child crashed before accept"),
	}

	// Unblock listener Accept() so r.accept receives it during select or drain
	close(ln.acceptedCh)

	_, err := r.accept(context.Background())
	if err == nil || !errors.Is(err, mediafacts.ErrCoreCrashed) {
		t.Fatalf("expected ErrCoreCrashed, got %v", err)
	}

	// Verify serverEnd was closed by drainAndClose(): reading clientEnd must yield EOF
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, rErr := clientEnd.Read(buf)
		readDone <- rErr
	}()

	select {
	case rErr := <-readDone:
		if rErr != io.EOF {
			t.Fatalf("expected io.EOF on clientEnd after drainAndClose(), got %v", rErr)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("clientEnd.Read timed out: accepted connection was leaked and not closed")
	}
}

func TestRemoteCore_Accept_ClosesAcceptedConnOnContextCancellationRace(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() {
		_ = serverEnd.Close()
		_ = clientEnd.Close()
	})

	ln := &controlledListener{
		conn:       serverEnd,
		acceptedCh: make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled

	r := &RemoteCore{
		ln:       ln,
		waitDone: make(chan struct{}),
	}

	close(ln.acceptedCh)

	_, err := r.accept(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, rErr := clientEnd.Read(buf)
		readDone <- rErr
	}()

	select {
	case rErr := <-readDone:
		if rErr != io.EOF {
			t.Fatalf("expected io.EOF on clientEnd after drainAndClose(), got %v", rErr)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("clientEnd.Read timed out: accepted connection was leaked and not closed")
	}
}

func TestRemoteCore_Accept_ClosesAcceptedConnOnTimeoutRace(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() {
		_ = serverEnd.Close()
		_ = clientEnd.Close()
	})

	ln := &controlledListener{
		conn:       serverEnd,
		acceptedCh: make(chan struct{}),
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()

	r := &RemoteCore{
		ln:       ln,
		waitDone: make(chan struct{}),
	}

	close(ln.acceptedCh)

	_, err := r.accept(ctx)
	if err == nil || !errors.Is(err, mediafacts.ErrCoreTimeout) {
		t.Fatalf("expected ErrCoreTimeout, got %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, rErr := clientEnd.Read(buf)
		readDone <- rErr
	}()

	select {
	case rErr := <-readDone:
		if rErr != io.EOF {
			t.Fatalf("expected io.EOF on clientEnd after drainAndClose(), got %v", rErr)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("clientEnd.Read timed out: accepted connection was leaked and not closed")
	}
}

func TestRemoteCore_Accept_RealSocketCancelRace(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context to race against accept

	r := &RemoteCore{
		ln:       ln,
		waitDone: make(chan struct{}),
	}

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	_, err = r.accept(ctx)
	if err == nil {
		t.Fatalf("expected error from accept with cancelled context")
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1)
	n, readErr := clientConn.Read(buf)
	if readErr == nil {
		t.Fatalf("expected client to observe closed connection, got n=%d, err=nil", n)
	}
}
