package manager

import (
	"context"
	"testing"
	"time"
)

func TestSessionRegistryCloseAndWaitDrainsWorkers(t *testing.T) {
	reg := &sessionRegistry{}

	done := make(chan struct{})
	if ok := reg.Go(func() {
		<-done
	}); !ok {
		t.Fatal("expected worker to start")
	}

	close(done)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := reg.CloseAndWait(ctx); err != nil {
		t.Fatalf("expected drain success, got %v", err)
	}
}

func TestSessionRegistryCloseAndWaitTimeout(t *testing.T) {
	reg := &sessionRegistry{}

	block := make(chan struct{})
	if ok := reg.Go(func() {
		<-block
	}); !ok {
		t.Fatal("expected worker to start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := reg.CloseAndWait(ctx); err == nil {
		t.Fatal("expected timeout error")
	}
	close(block)
}

func TestSessionRegistryRejectsNewWorkersAfterClose(t *testing.T) {
	reg := &sessionRegistry{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := reg.CloseAndWait(ctx); err != nil {
		t.Fatalf("expected close on empty registry to succeed, got %v", err)
	}

	if ok := reg.Go(func() {}); ok {
		t.Fatal("expected registry to reject workers after close")
	}
}
