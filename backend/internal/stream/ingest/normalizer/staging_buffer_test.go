package normalizer

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStagingBuffer_BasicReadWrite(t *testing.T) {
	sb := NewStagingBuffer(1000)
	defer sb.Close()

	if sb.Capacity() < 1000 {
		t.Fatalf("expected capacity >= 1000, got %d", sb.Capacity())
	}

	data := []byte("hello world")
	n, err := sb.Write(data)
	if err != nil || n != len(data) {
		t.Fatalf("write failed: n=%d, err=%v", n, err)
	}
	if sb.BufferedBytes() != len(data) {
		t.Fatalf("expected buffered=%d, got %d", len(data), sb.BufferedBytes())
	}

	readBuf := make([]byte, len(data))
	rn, rerr := sb.Read(readBuf)
	if rerr != nil || rn != len(data) {
		t.Fatalf("read failed: rn=%d, err=%v", rn, rerr)
	}
	if string(readBuf) != "hello world" {
		t.Fatalf("data mismatch: got %s", string(readBuf))
	}
	if sb.BufferedBytes() != 0 {
		t.Fatalf("expected buffered=0, got %d", sb.BufferedBytes())
	}
}

func TestStagingBuffer_WriteWithTimeout_BlocksUntilSpaceAvailable(t *testing.T) {
	// Align capacity: TSPacketSize is 188. Minimum allowed is 10 packets.
	capacity := 12 * TSPacketSize
	sb := NewStagingBuffer(capacity)
	defer sb.Close()

	// Fill 10 packets (10 * 188 = 1880 bytes)
	chunk1 := make([]byte, 10*TSPacketSize)
	if _, err := sb.Write(chunk1); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// Now writing 4 packets (752 bytes) would exceed capacity (1880 + 752 = 2632 > 2256).
	// It should block until read drains space.
	chunk2 := make([]byte, 4*TSPacketSize)
	var wg sync.WaitGroup
	var writeErr error
	var writeDuration time.Duration

	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		_, writeErr = sb.WriteWithTimeout(chunk2, 500*time.Millisecond)
		writeDuration = time.Since(start)
	}()

	// Wait 50ms to ensure writer is blocked, then drain 4 packets
	time.Sleep(50 * time.Millisecond)
	drainBuf := make([]byte, 4*TSPacketSize)
	if _, err := sb.Read(drainBuf); err != nil {
		t.Fatalf("drain read failed: %v", err)
	}

	wg.Wait()

	if writeErr != nil {
		t.Fatalf("expected blocked write to succeed after drain, got %v", writeErr)
	}
	if writeDuration < 40*time.Millisecond {
		t.Fatalf("expected write to block for at least 40ms, took %v", writeDuration)
	}

	// Remaining buffered should be: 10 - 4 + 4 = 10 packets
	expectedBytes := 10 * TSPacketSize
	if sb.BufferedBytes() != expectedBytes {
		t.Fatalf("expected %d buffered bytes, got %d", expectedBytes, sb.BufferedBytes())
	}
}

func TestStagingBuffer_WriteWithTimeout_ExpiresOnStall(t *testing.T) {
	capacity := 12 * TSPacketSize
	sb := NewStagingBuffer(capacity)
	defer sb.Close()

	// Fill 10 packets
	chunk1 := make([]byte, 10*TSPacketSize)
	if _, err := sb.Write(chunk1); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// Attempt to write 4 packets with 50ms timeout; no reader drains space
	chunk2 := make([]byte, 4*TSPacketSize)
	start := time.Now()
	_, err := sb.WriteWithTimeout(chunk2, 50*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrStagingBufferOverflow) {
		t.Fatalf("expected ErrStagingBufferOverflow on timeout, got %v", err)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("expected timeout after ~50ms, took %v", elapsed)
	}
}

func TestStagingBuffer_Write_SingleChunkExceedsCapacity(t *testing.T) {
	capacity := 12 * TSPacketSize
	sb := NewStagingBuffer(capacity)
	defer sb.Close()

	oversized := make([]byte, sb.Capacity()+1)
	_, err := sb.Write(oversized)
	if !errors.Is(err, ErrStagingBufferOverflow) {
		t.Fatalf("expected ErrStagingBufferOverflow immediately for chunk > capacity, got %v", err)
	}
}

func TestStagingBuffer_CloseUnblocksWriter(t *testing.T) {
	capacity := 12 * TSPacketSize
	sb := NewStagingBuffer(capacity)

	// Fill 10 packets
	chunk1 := make([]byte, 10*TSPacketSize)
	if _, err := sb.Write(chunk1); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	var wg sync.WaitGroup
	var writeErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		chunk2 := make([]byte, 4*TSPacketSize)
		_, writeErr = sb.WriteWithTimeout(chunk2, 1*time.Second)
	}()

	time.Sleep(30 * time.Millisecond)
	sb.Close()
	wg.Wait()

	if !errors.Is(writeErr, ErrNormalizerClosed) {
		t.Fatalf("expected ErrNormalizerClosed when buffer closed during blocked write, got %v", writeErr)
	}
}
