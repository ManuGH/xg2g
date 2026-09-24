// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"errors"
	"testing"
)

func TestPacketStore_BasicWriteAndRead(t *testing.T) {
	ps := newPacketStore(100)
	if ps.capacity != 100 {
		t.Fatalf("expected capacity 100, got %d", ps.capacity)
	}
	if ps.headOffset() != 0 || ps.tailOffset() != 0 || ps.bufferedBytes() != 0 {
		t.Fatalf("expected initial zeroes, got head=%d tail=%d buf=%d", ps.headOffset(), ps.tailOffset(), ps.bufferedBytes())
	}

	data := []byte("hello world")
	head, tail := ps.writeCommitted(data)
	if head != int64(len(data)) || tail != 0 {
		t.Fatalf("after write: head=%d tail=%d", head, tail)
	}
	if ps.bufferedBytes() != len(data) {
		t.Fatalf("expected buffered %d, got %d", len(data), ps.bufferedBytes())
	}

	buf := make([]byte, len(data))
	n, nextOffset, err := ps.readAt(buf, 0)
	if err != nil {
		t.Fatalf("readAt failed: %v", err)
	}
	if n != len(data) || nextOffset != int64(len(data)) {
		t.Fatalf("expected n=%d nextOffset=%d, got n=%d nextOffset=%d", len(data), len(data), n, nextOffset)
	}
	if !bytes.Equal(buf, data) {
		t.Fatalf("read mismatch: got %q, want %q", buf, data)
	}
}

func TestPacketStore_WrapAround(t *testing.T) {
	capacity := 10
	ps := newPacketStore(capacity)

	// Write 8 bytes (0..7)
	ps.writeCommitted([]byte("01234567"))
	if ps.headOffset() != 8 || ps.tailOffset() != 0 {
		t.Fatalf("head=%d tail=%d", ps.headOffset(), ps.tailOffset())
	}

	// Write 5 bytes (8..12) -> total 13 bytes, buffer should wrap and tail advance to 3
	ps.writeCommitted([]byte("89abc"))
	if ps.headOffset() != 13 || ps.tailOffset() != 3 {
		t.Fatalf("after wrap: head=%d (want 13), tail=%d (want 3)", ps.headOffset(), ps.tailOffset())
	}
	if ps.bufferedBytes() != capacity {
		t.Fatalf("bufferedBytes=%d, want %d", ps.bufferedBytes(), capacity)
	}

	// Reading at offset 2 (behind tail 3) should report overrun
	buf := make([]byte, 5)
	_, _, err := ps.readAt(buf, 2)
	if !errors.Is(err, ErrSubscriberOverrun) {
		t.Fatalf("expected ErrSubscriberOverrun, got %v", err)
	}

	// Reading at tail (3) should succeed and wrap cleanly
	readBuf := make([]byte, 10)
	n, nextOffset, err := ps.readAt(readBuf, 3)
	if err != nil {
		t.Fatalf("readAt failed: %v", err)
	}
	if n != 10 || nextOffset != 13 {
		t.Fatalf("expected n=10 nextOffset=13, got n=%d nextOffset=%d", n, nextOffset)
	}
	expected := []byte("3456789abc")
	if !bytes.Equal(readBuf, expected) {
		t.Fatalf("got %q, want %q", readBuf, expected)
	}
}

func TestPacketStore_WriteLargerThanCapacity(t *testing.T) {
	capacity := 10
	ps := newPacketStore(capacity)

	// Write 25 bytes at once into capacity 10
	largeData := []byte("abcdefghijklmnopqrstuvwxy") // 25 bytes
	head, tail := ps.writeCommitted(largeData)
	if head != 25 || tail != 15 {
		t.Fatalf("head=%d (want 25), tail=%d (want 15)", head, tail)
	}
	if ps.bufferedBytes() != 10 {
		t.Fatalf("buffered=%d, want 10", ps.bufferedBytes())
	}

	// Valid read from tail 15
	buf := make([]byte, 10)
	n, nextOffset, err := ps.readAt(buf, 15)
	if err != nil {
		t.Fatalf("readAt failed: %v", err)
	}
	if n != 10 || nextOffset != 25 {
		t.Fatalf("n=%d nextOffset=%d", n, nextOffset)
	}
	expected := []byte("pqrstuvwxy") // last 10 bytes
	if !bytes.Equal(buf, expected) {
		t.Fatalf("got %q, want %q", buf, expected)
	}

	// Read beyond head (offset 25)
	n2, nextOffset2, err2 := ps.readAt(buf, 25)
	if err2 != nil || n2 != 0 || nextOffset2 != 25 {
		t.Fatalf("read beyond head: n=%d nextOffset=%d err=%v", n2, nextOffset2, err2)
	}
}
