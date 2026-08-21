// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalizer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
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

func createTestPacketWithPCR(pid uint16, pcrSeconds float64, discontinuity bool) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = byte((pid >> 8) & 0x1F)
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x30 // AFC=0x03 (Adaptation Field + Payload), CC=0

	pkt[4] = 7            // AF length
	var flags byte = 0x10 // has PCR
	if discontinuity {
		flags |= 0x80
	}
	pkt[5] = flags

	pcrTicks := uint64(pcrSeconds * 27_000_000.0)
	pcrBase := pcrTicks / 300
	pcrExt := pcrTicks % 300

	pkt[6] = byte(pcrBase >> 25)
	pkt[7] = byte(pcrBase >> 17)
	pkt[8] = byte(pcrBase >> 9)
	pkt[9] = byte(pcrBase >> 1)
	pkt[10] = byte((pcrBase&0x01)<<7) | 0x7E | byte((pcrExt>>8)&0x01)
	pkt[11] = byte(pcrExt & 0xFF)

	for i := 12; i < TSPacketSize; i++ {
		pkt[i] = 0x55
	}
	return pkt
}

// 1. Fractional Packet Accumulator: 60+ minutes mathematical rate stability with ZERO drift
func TestNormalizer_FractionalAccumulator_LongTermStability(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 0.0 // instant release
	cfg.DeadbandMs = 0.0
	cfg.MaxCorrectionTrim = 0.0 // pure fractional pacer test (no watermark trim)
	cfg.Kp = 0.0
	cfg.StagingBufferCapacity = 10000 * TSPacketSize

	var emittedPackets int64
	sink := func(ctx context.Context, chunk []byte) error {
		atomic.AddInt64(&emittedPackets, int64(len(chunk)/TSPacketSize))
		return nil
	}

	norm, err := NewStreamNormalizer(cfg, sink)
	if err != nil {
		t.Fatalf("NewStreamNormalizer failed: %v", err)
	}
	defer norm.Close()

	// Simulate a 3333.3333 pps stream (non-integer 66.6666 packets per 20ms slice)
	const targetPPS = 3333.3333333333335
	norm.pcr.packetsPerSecond = targetPPS
	norm.pcr.bitrateEstimate = targetPPS * float64(TSPacketSize) * 8.0

	samplePkt := make([]byte, TSPacketSize)
	samplePkt[0] = SyncByte
	replenishChunk := make([]byte, 1000*TSPacketSize)
	for i := 0; i < len(replenishChunk); i += TSPacketSize {
		copy(replenishChunk[i:], samplePkt)
	}

	// Pre-fill
	_, _ = norm.staging.Write(replenishChunk)

	drainBuf := make([]byte, 1000*TSPacketSize)
	ctx := context.Background()

	// Simulate 180,000 ticks of 20ms (= 3,600 seconds = 60 minutes of streaming)
	const totalTicks = 180000
	const dt = 0.020 // 20ms

	for tick := 0; tick < totalTicks; tick++ {
		if norm.staging.BufferedBytes() < 2000*TSPacketSize {
			_, _ = norm.staging.Write(replenishChunk)
		}

		if err := norm.tickEgress(ctx, dt, drainBuf); err != nil {
			t.Fatalf("tickEgress failed at tick %d: %v", tick, err)
		}
	}

	expectedPackets := targetPPS * (float64(totalTicks) * dt)
	actualPackets := atomic.LoadInt64(&emittedPackets)

	diff := math.Abs(float64(actualPackets) - expectedPackets)
	if diff > 1.0 {
		t.Fatalf("quantization drift over 60min: expected %.2f packets, got %d (diff=%.4f packets)",
			expectedPackets, actualPackets, diff)
	}
	t.Logf("✅ 60-Minute Fractional Accumulator Stability: expected %.1f, got %d (diff=%.4f packets)",
		expectedPackets, actualPackets, diff)
}

// 2. Closed-Loop Watermark Regulation: exact non-saturated proportional trim & clamp points
func TestNormalizer_ClosedLoopWatermark_TrimConvergence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 0.0
	cfg.TargetWatermarkMs = 650.0
	cfg.DeadbandMs = 75.0
	cfg.MaxCorrectionTrim = 0.02 // ±2%
	cfg.Kp = 0.04

	norm, err := NewStreamNormalizer(cfg, nil)
	if err != nil {
		t.Fatalf("create normalizer failed: %v", err)
	}
	defer norm.Close()

	// 3000 pps = 4.512 Mbps (1 ms = 3 packets)
	norm.pcr.packetsPerSecond = 3000.0
	norm.pcr.bitrateEstimate = 3000.0 * float64(TSPacketSize) * 8.0

	drainBuf := make([]byte, 1000*TSPacketSize)
	samplePkt := make([]byte, TSPacketSize)
	samplePkt[0] = SyncByte
	ctx := context.Background()

	setWatermarkPackets := func(packets int) {
		norm.staging.Reset()
		buf := make([]byte, packets*TSPacketSize)
		for i := 0; i < len(buf); i += TSPacketSize {
			copy(buf[i:], samplePkt)
		}
		_, _ = norm.staging.Write(buf)
	}

	// 1. Non-saturated point: 750ms (+100ms error, excess = 25ms)
	// trim = (25 / 650) * 0.04 = 0.00153846 -> 1.001538
	setWatermarkPackets(2250) // 2250 pkts / 3 = 750ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m := norm.Metrics()
	expectedTrim750 := 1.0 + (25.0/650.0)*0.04
	if math.Abs(m.CorrectionFactor-expectedTrim750) > 0.0001 {
		t.Fatalf("expected non-saturated factor %.6f at 750ms, got %.6f", expectedTrim750, m.CorrectionFactor)
	}

	// 2. Non-saturated point: 800ms (+150ms error, excess = 75ms)
	// trim = (75 / 650) * 0.04 = 0.00461538 -> 1.004615
	setWatermarkPackets(2400) // 2400 pkts / 3 = 800ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m = norm.Metrics()
	expectedTrim800 := 1.0 + (75.0/650.0)*0.04
	if math.Abs(m.CorrectionFactor-expectedTrim800) > 0.0001 {
		t.Fatalf("expected non-saturated factor %.6f at 800ms, got %.6f", expectedTrim800, m.CorrectionFactor)
	}

	// 3. Deadband point: 680ms (within 650 ± 75ms)
	setWatermarkPackets(2040) // 2040 pkts / 3 = 680ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m = norm.Metrics()
	if m.CorrectionFactor != 1.0000 {
		t.Fatalf("expected factor 1.0000 inside deadband, got %.6f", m.CorrectionFactor)
	}

	// 4. Non-saturated deficit point: 550ms (-100ms error, deficit = 25ms)
	// trim = -(25 / 650) * 0.04 = -0.00153846 -> 0.998462
	setWatermarkPackets(1650) // 1650 pkts / 3 = 550ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m = norm.Metrics()
	expectedTrim550 := 1.0 - (25.0/650.0)*0.04
	if math.Abs(m.CorrectionFactor-expectedTrim550) > 0.0001 {
		t.Fatalf("expected non-saturated factor %.6f at 550ms, got %.6f", expectedTrim550, m.CorrectionFactor)
	}

	// 5. Clamped saturation point: 1200ms (+550ms error, excess = 475ms)
	// (475 / 650) * 0.04 = 0.0292 > 0.02 -> clamp to 1.0200
	setWatermarkPackets(3600) // 3600 pkts / 3 = 1200ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m = norm.Metrics()
	if m.CorrectionFactor != 1.0200 {
		t.Fatalf("expected clamped factor 1.0200 at 1200ms, got %.6f", m.CorrectionFactor)
	}

	// 6. Clamped deficit saturation point: 100ms (-550ms error, deficit = 475ms)
	// clamp to 0.9800
	setWatermarkPackets(300) // 300 pkts / 3 = 100ms
	_ = norm.tickEgress(ctx, 0.020, drainBuf)
	m = norm.Metrics()
	if m.CorrectionFactor != 0.9800 {
		t.Fatalf("expected clamped factor 0.9800 at 100ms, got %.6f", m.CorrectionFactor)
	}
}

// 3. Startup Reservoir: holds egress until 650ms buffered, then begins release
func TestNormalizer_StartupReservoir_HoldsAndReleases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 650.0

	var emittedCount int64
	sink := func(ctx context.Context, chunk []byte) error {
		atomic.AddInt64(&emittedCount, int64(len(chunk)/TSPacketSize))
		return nil
	}

	norm, err := NewStreamNormalizer(cfg, sink)
	if err != nil {
		t.Fatalf("failed to create normalizer: %v", err)
	}
	defer norm.Close()

	// 3000 pps (~4.5 Mbps), 100ms = 300 packets
	norm.pcr.packetsPerSecond = 3000.0
	norm.pcr.bitrateEstimate = 3000.0 * float64(TSPacketSize) * 8.0

	// 1. Push 200 packets (~66ms) -> under 650ms threshold
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	var smallBurst []byte
	for i := 0; i < 200; i++ {
		smallBurst = append(smallBurst, pkt...)
	}
	if err := norm.Feed(smallBurst); err != nil {
		t.Fatalf("feed failed: %v", err)
	}

	drainBuf := make([]byte, 1000*TSPacketSize)
	ctx := context.Background()
	_ = norm.tickEgress(ctx, 0.020, drainBuf)

	if atomic.LoadInt64(&emittedCount) != 0 {
		t.Fatalf("expected 0 packets emitted during startup reservoir, got %d", atomic.LoadInt64(&emittedCount))
	}

	// 2. Push 2000 packets (~666ms, total > 730ms) -> exceeds 650ms threshold
	var largeBurst []byte
	for i := 0; i < 2000; i++ {
		largeBurst = append(largeBurst, pkt...)
	}
	if err := norm.Feed(largeBurst); err != nil {
		t.Fatalf("feed failed: %v", err)
	}

	_ = norm.tickEgress(ctx, 0.020, drainBuf)

	if atomic.LoadInt64(&emittedCount) == 0 {
		t.Fatalf("expected packets to be released after startup reservoir satisfied, got 0")
	}
}

// 4. PCR Discontinuity: preserves last verified rate estimate without jumping to default
func TestNormalizer_PCRDiscontinuity_PreservesBitrate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InitialBitrateKbps = 4500.0

	norm, err := NewStreamNormalizer(cfg, nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer norm.Close()

	dummyPkt := make([]byte, TSPacketSize)
	dummyPkt[0] = SyncByte

	// 1. Establish 8.0 Mbps rate via 15 successive PCR intervals
	// 8.0 Mbps = 5319.14 pps
	for seq := 0; seq < 15; seq++ {
		pcrSec := 1.000 + float64(seq)*0.100
		pkt := createTestPacketWithPCR(100, pcrSec, false)
		norm.pcr.FeedPacket(pkt)
		for i := 0; i < 531; i++ {
			norm.pcr.FeedPacket(dummyPkt)
		}
	}

	currentRate := norm.pcr.PacketsPerSecond()
	if currentRate < 5200.0 {
		t.Fatalf("expected established rate near 5319 pps, got %.1f", currentRate)
	}

	// 2. Trigger severe PCR discontinuity jump: timestamp jumps back to 0.050s with discontinuity flag
	discontinuityPkt := createTestPacketWithPCR(100, 0.050, true)
	norm.pcr.FeedPacket(discontinuityPkt)

	rateAfterDiscontinuity := norm.pcr.PacketsPerSecond()
	if rateAfterDiscontinuity != currentRate {
		t.Fatalf("PCR discontinuity corrupted smoothed rate estimate: expected %.1f, got %.1f",
			currentRate, rateAfterDiscontinuity)
	}
}

// 5. Program-Specific PCR PID Filtering
func TestNormalizer_TargetPCRPID_Filtering(t *testing.T) {
	norm, _ := NewStreamNormalizer(DefaultConfig(), nil)
	defer norm.Close()

	norm.SetPCRPID(100) // Explicit target PCR PID

	// Feed PCR packets on unrelated PID 200
	pktUnrelated := createTestPacketWithPCR(200, 50.0, false)
	norm.pcr.FeedPacket(pktUnrelated)

	if norm.pcr.HasValidPCR() {
		t.Fatalf("estimator locked onto unrelated PCR PID 200 when target is 100")
	}

	// Feed PCR packet on PID 100
	pktTarget := createTestPacketWithPCR(100, 10.0, false)
	norm.pcr.FeedPacket(pktTarget)

	if !norm.pcr.HasValidPCR() {
		t.Fatalf("estimator failed to lock onto target PCR PID 100")
	}
}

// 6. Staging Buffer Overflow on Stalled Sink: fails closed safely
func TestNormalizer_SinkStall_ErrStagingBufferOverflow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StagingBufferCapacity = 500 * TSPacketSize // Small 500-packet buffer

	norm, _ := NewStreamNormalizer(cfg, nil)
	defer norm.Close()

	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	var overflowData []byte
	for i := 0; i < 550; i++ {
		overflowData = append(overflowData, pkt...)
	}

	err := norm.Feed(overflowData)
	if !errors.Is(err, ErrStagingBufferOverflow) {
		t.Fatalf("expected ErrStagingBufferOverflow when sink is stalled, got %v", err)
	}
}

// 7. Decoupled Ingress & Egress: Socket stall does NOT freeze egress pacer
func TestNormalizer_DecoupledIngress_SocketStallDoesNotFreezePacer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 0.0 // Instant release

	var egressDispatched int64
	sink := func(ctx context.Context, chunk []byte) error {
		atomic.AddInt64(&egressDispatched, int64(len(chunk)/TSPacketSize))
		return nil
	}

	norm, err := NewStreamNormalizer(cfg, sink)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer norm.Close()

	pipeReader, pipeWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- norm.Run(ctx, pipeReader)
	}()

	// 1. Write 500 packets through socket
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	for i := 0; i < 500; i++ {
		if _, err := pipeWriter.Write(pkt); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	// 2. Intentionally pause socket reads (simulating Enigma2 network pause)
	time.Sleep(150 * time.Millisecond)

	// Pacer must have continuously dispatched buffered packets despite socket silence
	dispatched := atomic.LoadInt64(&egressDispatched)
	if dispatched == 0 {
		t.Fatalf("egress pacer stalled during socket silence")
	}

	cancel()
	_ = pipeWriter.Close()
	<-runErrCh
}

// 8. Blocking source.Read() terminates immediately when context is cancelled
func TestNormalizer_BlockingSourceRead_ContextCancellation(t *testing.T) {
	norm, err := NewStreamNormalizer(DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer norm.Close()

	pipeReader, _ := io.Pipe() // Never written to

	ctx, cancel := context.WithCancel(context.Background())

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- norm.Run(ctx, pipeReader)
	}()

	time.Sleep(50 * time.Millisecond) // Let Run() enter blocking pipeReader.Read()
	cancel()                          // Cancel context

	select {
	case err := <-runErrCh:
		if err != nil && err != context.Canceled && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("unexpected exit error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Run() hung on blocking source.Read after context cancellation")
	}
}

// 9. Blocking Sink with Context Cancellation terminates Run() without deadlock
func TestNormalizer_BlockingSink_ContextCancellationTerminates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 0.0 // Instant release

	// Sink blocks on context cancellation
	sink := func(ctx context.Context, chunk []byte) error {
		<-ctx.Done()
		return ctx.Err()
	}

	norm, err := NewStreamNormalizer(cfg, sink)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer norm.Close()

	pipeReader, pipeWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- norm.Run(ctx, pipeReader)
	}()

	// Feed initial packets so egress pacer enters sink()
	samplePkt := make([]byte, TSPacketSize)
	samplePkt[0] = SyncByte
	_, _ = pipeWriter.Write(samplePkt)

	time.Sleep(50 * time.Millisecond) // Pacer enters blocking sink
	cancel()                          // Cancel context

	select {
	case err := <-runErrCh:
		if err != nil && err != context.Canceled && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("unexpected exit error on cancelled blocking sink: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Run() hung on blocking sink after context cancellation (deadlock on wg.Wait)")
	}
	_ = pipeWriter.Close()
}

// 10. Concurrent Feed() Thread-Safety
func TestNormalizer_ConcurrentFeed_ThreadSafety(t *testing.T) {
	norm, err := NewStreamNormalizer(DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer norm.Close()

	samplePkt := make([]byte, TSPacketSize)
	samplePkt[0] = SyncByte

	var wg sync.WaitGroup
	const writers = 10
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = norm.Feed(samplePkt)
			}
		}()
	}
	wg.Wait()

	m := norm.Metrics()
	if m.PacketsIn != writers*50 {
		t.Fatalf("expected %d packets in, got %d", writers*50, m.PacketsIn)
	}
}

// 11. Real Broadcast Stream End-to-End: Normalizer -> MasterRing -> FFmpeg Decoding Proof
func TestNormalizer_RealBroadcast_EndToEnd(t *testing.T) {
	root := findProjectRoot(t)
	capturePath := filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts")

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read capture failed: %v", err)
	}

	// 1. Target MasterRing
	master := ring.NewMasterRing(20000 * TSPacketSize)
	defer master.Close()

	// 2. Normalizer feeding MasterRing.Push
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 50.0 // Fast start for test
	cfg.PacerIntervalMs = 5.0     // Fast 5ms ticks for accelerated test delivery
	cfg.InitialBitrateKbps = 20000.0

	norm, err := NewStreamNormalizer(cfg, func(ctx context.Context, chunk []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, pushErr := master.Push(chunk)
		return pushErr
	})
	if err != nil {
		t.Fatalf("create normalizer failed: %v", err)
	}
	defer norm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- norm.Run(ctx, bytes.NewReader(data))
	}()

	// Wait for normalizer to pump and pace all data
	select {
	case err := <-runErrCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("normalizer run failed: %v", err)
		}
	case <-time.After(8 * time.Second):
		cancel()
	}

	// 3. Verify MasterRing indexed the stream from Normalizer
	vPID, vCodec := master.VideoDetails()
	if vPID != 256 || vCodec != ring.CodecH264 {
		t.Fatalf("expected vPID=256, vCodec=h264, got %d, %s", vPID, vCodec)
	}

	kfOffset, ok := master.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("no keyframe was indexed in MasterRing via normalizer")
	}

	preamble := master.PATPMTPreamble()
	reader := master.NewSubscriberReader(0)
	defer reader.Close()

	if _, err := reader.SeekToLatestKeyframe(); err != nil {
		t.Fatalf("seek to keyframe failed: %v", err)
	}

	streamData := make([]byte, 20000*TSPacketSize)
	n, err := reader.Read(streamData)
	if err != nil && err != io.EOF {
		t.Fatalf("reader.Read failed: %v", err)
	}
	if n == 0 {
		t.Fatalf("reader returned 0 bytes")
	}

	primedStream := append(preamble, streamData[:n]...)

	// 4. Strict FFmpeg Decoding Gate
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(primedStream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("strict FFmpeg decoding failed: %v (stderr: %s)", err, stderr.String())
	}

	decodedFrames := countDecodedVideoFrames(t, primedStream)
	if decodedFrames <= 0 {
		t.Fatalf("expected at least 1 decoded video frame, got %d", decodedFrames)
	}

	t.Logf("✅ End-to-End Normalizer -> MasterRing -> FFmpeg Decoded SUCCESS: vPID=%d kfOffset=%d decodedFrames=%d",
		vPID, kfOffset, decodedFrames)
}
