package ffmpeg

import (
	"bytes"
	"crypto/sha256"
	"io"
	"sync"
	"testing"
	"time"
)

// slowReader simulates a live stream that delivers chunks with small delays.
type slowReader struct {
	data []byte
	off  int
	step int
	delay time.Duration
}

func (r *slowReader) Read(p []byte) (n int, err error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	n = len(p)
	if n > r.step {
		n = r.step
	}
	if r.off+n > len(r.data) {
		n = len(r.data) - r.off
	}
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

func TestBoundedStartupSpool_ByteForByteIntegrity(t *testing.T) {
	totalSize := 512 << 10
	inputData := make([]byte, totalSize)
	for i := 0; i < totalSize; i++ {
		inputData[i] = byte((i * 13) ^ (i >> 3))
	}
	expectedHash := sha256.Sum256(inputData)

	reader := &slowReader{
		data:  inputData,
		step:  16 << 10,
		delay: 2 * time.Millisecond,
	}

	spool := newBoundedStartupSpool(reader)
	go spool.run(1 << 20)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			time.Sleep(10 * time.Millisecond)
			snap := spool.snapshot()
			_ = snap
		}
		spool.markDecided()
	}()
	wg.Wait()

	outData, err := io.ReadAll(spool)
	if err != nil {
		t.Fatalf("unexpected error draining spool: %v", err)
	}

	if len(outData) != totalSize {
		t.Fatalf("expected %d bytes, got %d", totalSize, len(outData))
	}

	actualHash := sha256.Sum256(outData)
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! data corruption during spooling/draining")
	}
}

func TestBoundedStartupSpool_LimitReachedFallback(t *testing.T) {
	totalSize := 128 << 10
	inputData := make([]byte, totalSize)
	for i := 0; i < totalSize; i++ {
		inputData[i] = byte(i)
	}

	reader := bytes.NewReader(inputData)
	spoolLimit := 32 << 10

	spool := newBoundedStartupSpool(reader)
	go spool.run(spoolLimit)

	time.Sleep(50 * time.Millisecond)

	spool.mu.Lock()
	if spool.err != errSpoolLimit {
		t.Errorf("expected errSpoolLimit during buffering, got %v", spool.err)
	}
	spool.mu.Unlock()

	spool.markDecided()

	outData, err := io.ReadAll(spool)
	if err != nil {
		t.Fatalf("unexpected error draining limit-reached spool: %v", err)
	}

	if len(outData) != totalSize {
		t.Fatalf("expected %d bytes from fallback, got %d", totalSize, len(outData))
	}

	if !bytes.Equal(outData, inputData) {
		t.Fatalf("fallback data corrupted after spool limit")
	}
}

func TestBoundedStartupSpool_StreamingBackpressure(t *testing.T) {
	totalSize := 256 << 10
	inputData := make([]byte, totalSize)
	for i := 0; i < totalSize; i++ {
		inputData[i] = byte((i * 7) & 0xff)
	}

	reader := bytes.NewReader(inputData)
	spoolLimit := 32 << 10

	spool := newBoundedStartupSpool(reader)
	go spool.run(spoolLimit)

	spool.markDecided()

	time.Sleep(20 * time.Millisecond)

	spool.mu.Lock()
	if spool.totalBytes > spoolLimit+32<<10 {
		t.Fatalf("backpressure failed, totalBytes=%d exceeded limit=%d + chunk", spool.totalBytes, spoolLimit)
	}
	spool.mu.Unlock()

	outData, err := io.ReadAll(spool)
	if err != nil {
		t.Fatalf("unexpected error draining backpressure spool: %v", err)
	}

	if len(outData) != totalSize {
		t.Fatalf("expected %d bytes from backpressure test, got %d", totalSize, len(outData))
	}

	if !bytes.Equal(outData, inputData) {
		t.Fatalf("backpressure data corrupted")
	}
}
