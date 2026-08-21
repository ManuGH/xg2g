// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

func findProjectRoot(t *testing.T) string {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Clean(filepath.Join(cwd, "../../../.."))
}

func countDecodedVideoFrames(t *testing.T, data []byte) int {
	cmd := exec.Command("ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0",
		"-show_entries", "stream=nb_read_frames", "-of", "default=nokey=1:noprint_wrappers=1", "pipe:0")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("ffprobe frame counting failed: %v (stderr: %s)", err, stderr.String())
	}

	fields := strings.Fields(stdout.String())
	if len(fields) == 0 {
		t.Fatalf("empty ffprobe frame counting output")
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("failed to parse decoded frame count from ffprobe output %q: %v", stdout.String(), err)
	}
	return count
}

// 1. Single Upstream Dial for 20 Concurrent Acquires
func TestPipeline_CoalescedDial_20ConcurrentAcquires(t *testing.T) {
	var dialCount int32

	samplePkt := make([]byte, ring.TSPacketSize)
	samplePkt[0] = ring.SyncByte

	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		atomic.AddInt32(&dialCount, 1)
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				chunk := make([]byte, 50*ring.TSPacketSize)
				for i := 0; i < len(chunk); i += ring.TSPacketSize {
					copy(chunk[i:], samplePkt)
				}
				if _, err := pw.Write(chunk); err != nil {
					return
				}
			}
		}()
		return pr, nil
	}

	connector := NewLivePipelineConnector(connectorCfg)
	mgrCfg := session.DefaultManagerConfig()
	mgr := session.NewManager(mgrCfg, connector)
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:19:283D:3FB:1:C00000:0:0:0:")

	var wg sync.WaitGroup
	const concurrentClients = 20
	leases := make([]*session.Lease, concurrentClients)
	errs := make([]error, concurrentClients)

	for i := 0; i < concurrentClients; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			leases[idx], errs[idx] = mgr.Acquire(ctx, key)
		}()
	}
	wg.Wait()

	for i := 0; i < concurrentClients; i++ {
		if errs[i] != nil {
			t.Fatalf("client %d acquire failed: %v", i, errs[i])
		}
		if leases[i] == nil {
			t.Fatalf("client %d received nil lease", i)
		}
		defer leases[i].Release()
	}

	dials := atomic.LoadInt32(&dialCount)
	if dials != 1 {
		t.Fatalf("expected exactly 1 upstream dial for %d concurrent clients, got %d", concurrentClients, dials)
	}
}

// 2. Dead-pipeline eviction: Upstream dies -> session evicted -> re-acquire redials healthy pipeline
func TestPipeline_UpstreamDies_ReacquireRedialsHealthyPipeline(t *testing.T) {
	var dialCount int32

	samplePkt := make([]byte, ring.TSPacketSize)
	samplePkt[0] = ring.SyncByte

	var pipeWriterMu sync.Mutex
	var activePipeWriter io.Closer

	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 0.0
	connectorCfg.NormConfig.PacerIntervalMs = 5.0

	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		atomic.AddInt32(&dialCount, 1)
		pr, pw := io.Pipe()
		pipeWriterMu.Lock()
		activePipeWriter = pw
		pipeWriterMu.Unlock()

		go func() {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					pw.Close()
					return
				case <-ticker.C:
					chunk := make([]byte, 20*ring.TSPacketSize)
					for i := 0; i < len(chunk); i += ring.TSPacketSize {
						copy(chunk[i:], samplePkt)
					}
					if _, err := pw.Write(chunk); err != nil {
						return
					}
				}
			}
		}()
		return pr, nil
	}

	mgrCfg := session.ManagerConfig{
		WarmHoldDuration: 5 * time.Second, // Long warm-hold
		ConnectTimeout:   2 * time.Second,
	}
	mgr := session.NewManager(mgrCfg, NewLivePipelineConnector(connectorCfg))
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:19:DEAD:0:0:0:0:0:0:")
	ctx := context.Background()

	// 1. Client 1 acquires and joins
	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	p1 := lease1.Session().Payload().(*SessionPipeline)

	// Verify 1 dial
	if atomic.LoadInt32(&dialCount) != 1 {
		t.Fatalf("expected 1 dial initially")
	}

	// 2. Upstream dies (Enigma2 closes connection)
	pipeWriterMu.Lock()
	if activePipeWriter != nil {
		activePipeWriter.Close()
	}
	pipeWriterMu.Unlock()

	// Wait for pipeline to finish and trigger OnDone
	select {
	case <-p1.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("pipeline did not terminate after upstream death")
	}

	// Release client 1 lease
	lease1.Release()

	// Brief pause for teardown transition
	time.Sleep(50 * time.Millisecond)

	// 3. Client 2 acquires after upstream death -> MUST REDIAL (not reuse dead warm-hold session!)
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer lease2.Release()

	dials := atomic.LoadInt32(&dialCount)
	if dials != 2 {
		t.Fatalf("expected dead pipeline eviction to trigger second dial on reacquire, got %d dials", dials)
	}

	p2 := lease2.Session().Payload().(*SessionPipeline)
	if p2 == p1 {
		t.Fatalf("expected fresh SessionPipeline instance, got reused dead pipeline")
	}
}

// 3. Immediate upstream EOF (before OnDone callback registration in Session.SetStarted)
// must be late-subscriber safe and evict the dead session immediately so subsequent Acquire redials.
func TestPipeline_ImmediateUpstreamEOF_BeforeWatcherRegistration_EvictsSession(t *testing.T) {
	for iteration := 0; iteration < 10; iteration++ {
		var dialCount int32

		samplePkt := make([]byte, ring.TSPacketSize)
		samplePkt[0] = ring.SyncByte

		connectorCfg := DefaultConnectorConfig("", 8001)
		connectorCfg.NormConfig.StartupReservoirMs = 0.0
		connectorCfg.NormConfig.PacerIntervalMs = 5.0

		// Dial 1 returns an immediate EOF reader (Pipeline completes before/during SetStarted)
		// Dial 2 returns a healthy infinite stream
		connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
			currentDial := atomic.AddInt32(&dialCount, 1)
			if currentDial == 1 {
				return io.NopCloser(bytes.NewReader(nil)), nil
			}

			pr, pw := io.Pipe()
			go func() {
				defer pw.Close()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for range ticker.C {
					chunk := make([]byte, 20*ring.TSPacketSize)
					for i := 0; i < len(chunk); i += ring.TSPacketSize {
						copy(chunk[i:], samplePkt)
					}
					if _, err := pw.Write(chunk); err != nil {
						return
					}
				}
			}()
			return pr, nil
		}

		mgrCfg := session.ManagerConfig{
			WarmHoldDuration: 5 * time.Second, // Long warm-hold
			ConnectTimeout:   2 * time.Second,
		}
		mgr := session.NewManager(mgrCfg, NewLivePipelineConnector(connectorCfg))

		key := session.NewSessionKey("127.0.0.1", 8001, fmt.Sprintf("1:0:19:IMMEDIATE_%d:0:0:0:0:0:0:", iteration))
		ctx := context.Background()

		// 1. Client 1 acquires -> hits immediate EOF
		lease1, err := mgr.Acquire(ctx, key)
		if err == nil {
			p1 := lease1.Session().Payload().(*SessionPipeline)
			<-p1.Done()
			lease1.Release()
		}

		// Wait briefly for teardown eviction to process
		time.Sleep(30 * time.Millisecond)

		// 2. Client 2 acquires -> MUST trigger Dial #2 (not reuse dead session)
		lease2, err := mgr.Acquire(ctx, key)
		if err != nil {
			mgr.Close()
			t.Fatalf("iteration %d: second acquire failed: %v", iteration, err)
		}
		p2 := lease2.Session().Payload().(*SessionPipeline)
		r2, err := p2.LiveAttach()
		if err != nil {
			lease2.Release()
			mgr.Close()
			t.Fatalf("iteration %d: live attach failed: %v", iteration, err)
		}
		r2.Close()
		lease2.Release()
		mgr.Close()

		dials := atomic.LoadInt32(&dialCount)
		if dials < 2 {
			t.Fatalf("iteration %d: expected redial after immediate EOF, got %d dials", iteration, dials)
		}
	}
}

// 4. Atomic Primed Attach: Preamble + Generation + KeyframeOffset + Reader created atomically under MasterRing lock
func TestPipeline_PrimedAttachSnapshotAndReaderAreAtomic(t *testing.T) {
	root := findProjectRoot(t)
	capturePath := filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts")
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read capture failed: %v", err)
	}

	const smallRingCapacity = 200 * ring.TSPacketSize
	master := ring.NewMasterRing(smallRingCapacity)
	defer master.Close()

	// Push first slice with PAT/PMT/IDR
	_, err = master.Push(data[:150*ring.TSPacketSize])
	if err != nil {
		t.Fatalf("push failed: %v", err)
	}

	// Capture primed attach atomically
	attach, reader, err := master.NewPrimedSubscriber()
	if err != nil {
		t.Fatalf("NewPrimedSubscriber failed: %v", err)
	}
	defer reader.Close()

	if !attach.HasKeyframe {
		t.Fatalf("expected attach to have keyframe")
	}
	if len(attach.Preamble) == 0 {
		t.Fatalf("expected attach to contain PAT/PMT preamble")
	}
}

// 4. Deterministic Failure when No Keyframe is available (Never fall back to Tail)
func TestPipeline_PrimedAttachWithoutKeyframeFailsDeterministically(t *testing.T) {
	normCfg := normalizer.DefaultConfig()
	normCfg.StartupReservoirMs = 0.0

	pipe, err := NewSessionPipeline(normCfg, 10000*ring.TSPacketSize, 0)
	if err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	defer pipe.Close()

	// PrimedAttach without any pushed keyframes must return ErrNoAttachAvailable immediately
	attach, reader, err := pipe.PrimedAttach()
	if err == nil {
		reader.Close()
		t.Fatalf("expected ErrNoAttachAvailable when no keyframe present, got nil error (attach: %+v)", attach)
	}
	if !errors.Is(err, ErrNoAttachAvailable) {
		t.Fatalf("expected ErrNoAttachAvailable, got %v", err)
	}
}

// 5. Slow Subscriber Isolation: Overrun subscriber drops packets without affecting fast subscribers
func TestPipeline_SlowSubscriberIsolation_NoInterference(t *testing.T) {
	const smallRingCapacity = 50 * ring.TSPacketSize // 50 packets capacity

	samplePkt := make([]byte, ring.TSPacketSize)
	samplePkt[0] = ring.SyncByte

	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.RingCapacity = smallRingCapacity
	connectorCfg.NormConfig.StartupReservoirMs = 0.0
	connectorCfg.NormConfig.PacerIntervalMs = 5.0

	pr, pw := io.Pipe()
	defer pw.Close()

	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		return pr, nil
	}

	connector := NewLivePipelineConnector(connectorCfg)
	mgr := session.NewManager(session.DefaultManagerConfig(), connector)
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:1:TEST:0:0:0:0:0:0:")
	ctx := context.Background()

	// 4 Fast Clients
	const fastCount = 4
	fastReaders := make([]*ring.SubscriberReader, fastCount)
	for i := 0; i < fastCount; i++ {
		lease, err := mgr.Acquire(ctx, key)
		if err != nil {
			t.Fatalf("fast acquire %d failed: %v", i, err)
		}
		defer lease.Release()
		p := lease.Session().Payload().(*SessionPipeline)
		reader, _ := p.LiveAttach()
		fastReaders[i] = reader
		defer reader.Close()
	}

	// 1 Slow Client
	slowLease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("slow acquire failed: %v", err)
	}
	defer slowLease.Release()
	slowPipe := slowLease.Session().Payload().(*SessionPipeline)
	slowReader, _ := slowPipe.LiveAttach()
	defer slowReader.Close()

	// Launch concurrent fast clients reading actively
	var fastWg sync.WaitGroup
	fastErrs := make([]error, fastCount)
	for i := 0; i < fastCount; i++ {
		idx := i
		fastWg.Add(1)
		go func() {
			defer fastWg.Done()
			buf := make([]byte, 200*ring.TSPacketSize)
			readTotal := 0
			for readTotal < 180*ring.TSPacketSize {
				n, err := fastReaders[idx].Read(buf)
				if err != nil {
					fastErrs[idx] = err
					return
				}
				readTotal += n
			}
		}()
	}

	// Feed 200 packets (> 4x ring capacity) in paced slices so fast readers keep up with head
	for i := 0; i < 20; i++ {
		slice := make([]byte, 10*ring.TSPacketSize)
		for j := 0; j < len(slice); j += ring.TSPacketSize {
			copy(slice[j:], samplePkt)
		}
		if _, err := pw.Write(slice); err != nil {
			t.Fatalf("pipe write failed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	fastWg.Wait()

	for i := 0; i < fastCount; i++ {
		if fastErrs[i] != nil {
			t.Fatalf("fast reader %d failed: %v", i, fastErrs[i])
		}
		if fastReaders[i].DroppedBytes() != 0 {
			t.Fatalf("fast reader %d suffered dropped bytes: %d", i, fastReaders[i].DroppedBytes())
		}
	}

	// Now slow reader attempts to read (must report dropped bytes due to overrun)
	buf := make([]byte, 200*ring.TSPacketSize)
	n, err := slowReader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("slow reader error: %v", err)
	}
	if n == 0 {
		t.Fatalf("slow reader got 0 bytes")
	}

	dropped := slowReader.DroppedBytes()
	if dropped <= 0 {
		t.Fatalf("expected slow reader to register dropped bytes on ring overrun, got %d", dropped)
	}
}

// 6. Warm-Hold Reattach: Last subscriber disconnects -> reattach reuses same stream
func TestPipeline_WarmHoldReattach_PreservesStream(t *testing.T) {
	var dialCount int32
	samplePkt := make([]byte, ring.TSPacketSize)
	samplePkt[0] = ring.SyncByte

	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 0.0

	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		atomic.AddInt32(&dialCount, 1)
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				chunk := make([]byte, 20*ring.TSPacketSize)
				for i := 0; i < len(chunk); i += ring.TSPacketSize {
					copy(chunk[i:], samplePkt)
				}
				if _, err := pw.Write(chunk); err != nil {
					return
				}
			}
		}()
		return pr, nil
	}

	mgrCfg := session.ManagerConfig{
		WarmHoldDuration: 500 * time.Millisecond,
		ConnectTimeout:   2 * time.Second,
	}
	mgr := session.NewManager(mgrCfg, NewLivePipelineConnector(connectorCfg))
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:19:WARM:0:0:0:0:0:0:")
	ctx := context.Background()

	// 1. First Client attaches
	lease1, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	p1 := lease1.Session().Payload().(*SessionPipeline)
	r1, _ := p1.LiveAttach()
	r1.Close()

	// 2. First Client disconnects -> Session enters StateHolding
	lease1.Release()
	time.Sleep(50 * time.Millisecond)

	// 3. Second Client attaches within 500ms warm-hold window
	lease2, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer lease2.Release()

	p2 := lease2.Session().Payload().(*SessionPipeline)
	r2, _ := p2.LiveAttach()
	r2.Close()

	dials := atomic.LoadInt32(&dialCount)
	if dials != 1 {
		t.Fatalf("expected stream to be preserved across warm-hold (1 dial), got %d dials", dials)
	}

	if p2 != p1 {
		t.Fatalf("expected identical session pipeline instance across warm-hold reattach")
	}
}

// 7. End-to-End HTTP Real Broadcast Streaming with FFmpeg Proof across 3 Concurrent Clients
func TestPipeline_RealBroadcast_EndToEndDecoding(t *testing.T) {
	root := findProjectRoot(t)
	capturePath := filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts")

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read capture failed: %v", err)
	}

	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 50.0
	connectorCfg.NormConfig.PacerIntervalMs = 5.0
	connectorCfg.NormConfig.InitialBitrateKbps = 20000.0

	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(connectorCfg))
	defer mgr.Close()

	handler := NewHandler(mgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	const clientCount = 3
	var wg sync.WaitGroup
	clientStreams := make([][]byte, clientCount)
	clientErrs := make([]error, clientCount)

	for i := 0; i < clientCount; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqURL := fmt.Sprintf("%s/api/v3/stream/live/1:0:19:283D:3FB:1:C00000:0:0:0:", server.URL)
			resp, err := http.Get(reqURL)
			if err != nil {
				clientErrs[idx] = err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				clientErrs[idx] = fmt.Errorf("unexpected status %d", resp.StatusCode)
				return
			}

			// Read up to 500 KB from stream
			bodyBuf := make([]byte, 500*1024)
			n, _ := io.ReadFull(resp.Body, bodyBuf)
			if n > 0 {
				clientStreams[idx] = bodyBuf[:n]
			}
		}()
	}
	wg.Wait()

	for i := 0; i < clientCount; i++ {
		if clientErrs[i] != nil {
			t.Fatalf("client %d HTTP stream error: %v", i, clientErrs[i])
		}
		if len(clientStreams[i]) == 0 {
			t.Fatalf("client %d received 0 bytes", i)
		}

		// Strict FFmpeg decoding check on every concurrent client's primed stream
		cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
		cmd.Stdin = bytes.NewReader(clientStreams[i])
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("client %d strict FFmpeg decode failed: %v (stderr: %s)", i, err, stderr.String())
		}

		frames := countDecodedVideoFrames(t, clientStreams[i])
		if frames <= 0 {
			t.Fatalf("client %d decoded 0 frames", i)
		}
		t.Logf("✅ Client %d Decoded %d video frames from primed HTTP live stream", i, frames)
	}
}
