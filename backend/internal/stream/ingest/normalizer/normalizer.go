// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalizer

import (
	"context"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// SinkFunc is the consumer callback that receives time-regulated TS packets.
type SinkFunc func(chunk []byte) error

// Metrics captures diagnostic metrics of the normalizer session.
type Metrics struct {
	PacketsIn        int64
	PacketsOut       int64
	BytesIn          int64
	BytesOut         int64
	Underruns        int64
	BufferMinMs      float64
	BufferAvgMs      float64
	BufferMaxMs      float64
	CurrentWatermark float64
	BitrateKbps      float64
	CorrectionFactor float64
}

// StreamNormalizer implements the Closed-Loop PCR & Watermark Pacing Engine.
// It decouples bursty upstream network socket reads from internal distribution.
type StreamNormalizer struct {
	cfg       Config
	sink      SinkFunc
	pcr       *PCREstimator
	staging   *StagingBuffer
	ingressMu sync.Mutex
	syncAlign []byte

	// State (guarded by stateMu)
	stateMu          sync.Mutex
	isReleased       bool
	packetCredit     float64
	correctionFactor float64
	currentWatermark float64
	minWatermark     float64
	maxWatermark     float64
	totalWatermarkMs float64
	watermarkSamples int64

	// Atomic counters for lock-free telemetry
	packetsIn  int64
	packetsOut int64
	bytesIn    int64
	bytesOut   int64
	underruns  int64

	closed atomic.Bool
	stopCh chan struct{}
}

// NewStreamNormalizer constructs a new StreamNormalizer with the given configuration and packet sink.
func NewStreamNormalizer(cfg Config, sink SinkFunc) (*StreamNormalizer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if sink == nil {
		sink = func(chunk []byte) error { return nil }
	}

	sn := &StreamNormalizer{
		cfg:              cfg,
		sink:             sink,
		pcr:              NewPCREstimator(cfg.InitialBitrateKbps, 0),
		staging:          NewStagingBuffer(cfg.StagingBufferCapacity),
		correctionFactor: 1.0,
		minWatermark:     math.MaxFloat64,
		stopCh:           make(chan struct{}),
	}
	return sn, nil
}

// SetPCRPID configures a program-specific PCR PID to track on ingress.
func (sn *StreamNormalizer) SetPCRPID(pid uint16) {
	sn.pcr.SetPCRPID(pid)
}

// Feed ingests a slice of raw TS data into the normalizer.
// It is fully thread-safe, inspects PCR headers, maintains packet alignment, and buffers into the staging FIFO.
func (sn *StreamNormalizer) Feed(data []byte) error {
	if sn.closed.Load() {
		return ErrNormalizerClosed
	}
	if len(data) == 0 {
		return nil
	}

	sn.ingressMu.Lock()
	defer sn.ingressMu.Unlock()

	// 1. Packet alignment handling
	if len(sn.syncAlign) > 0 {
		combined := append(sn.syncAlign, data...)
		sn.syncAlign = nil
		data = combined
	}

	// Find first sync byte (0x47)
	syncIdx := -1
	for i := 0; i < len(data); i++ {
		if data[i] == SyncByte {
			// Verify next packet sync if data is long enough
			if i+TSPacketSize < len(data) && data[i+TSPacketSize] != SyncByte {
				continue
			}
			syncIdx = i
			break
		}
	}

	if syncIdx == -1 {
		// No sync byte found in chunk, discard
		return nil
	}

	data = data[syncIdx:]
	fullPacketsLen := (len(data) / TSPacketSize) * TSPacketSize
	aligned := data[:fullPacketsLen]
	remainder := data[fullPacketsLen:]

	if len(remainder) > 0 {
		sn.syncAlign = append(sn.syncAlign, remainder...)
	}

	if len(aligned) == 0 {
		return nil
	}

	// 2. Feed packets to PCR rate estimator
	for i := 0; i < len(aligned); i += TSPacketSize {
		pkt := aligned[i : i+TSPacketSize]
		sn.pcr.FeedPacket(pkt)
	}

	// 3. Write into bounded staging buffer (fails closed with ErrStagingBufferOverflow if full)
	n, err := sn.staging.Write(aligned)
	if err != nil {
		return err
	}

	atomic.AddInt64(&sn.bytesIn, int64(n))
	atomic.AddInt64(&sn.packetsIn, int64(n/TSPacketSize))
	return nil
}

// Run starts the decoupled normalizer engine:
// - If source implements io.Closer, it is watched and closed on context cancellation to unblock hanging reads immediately
// - A background egress pacer loop runs at the configured tick slice (20ms)
// - The main goroutine continuously pumps from source into Feed()
func (sn *StreamNormalizer) Run(ctx context.Context, source io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ensure blocking socket reads unblock immediately upon context cancellation
	if closer, ok := source.(io.Closer); ok {
		go func() {
			<-ctx.Done()
			_ = closer.Close()
		}()
	}

	errCh := make(chan error, 2)

	// Launch decoupled 20ms Egress Pacing goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := sn.startPacerLoop(ctx); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	// Ingress pump loop (reads up to 64 KiB from source)
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			sn.Close()
			wg.Wait()
			return ctx.Err()
		case <-sn.stopCh:
			wg.Wait()
			return nil
		case err := <-errCh:
			sn.Close()
			wg.Wait()
			return err
		default:
		}

		n, err := source.Read(buf)
		if n > 0 {
			if feedErr := sn.Feed(buf[:n]); feedErr != nil {
				cancel()
				sn.Close()
				wg.Wait()
				return feedErr
			}
		}
		if err != nil {
			if err == io.EOF {
				// Wait for staging buffer to drain to sink
				for sn.staging.BufferedBytes() > 0 {
					select {
					case <-ctx.Done():
						sn.Close()
						wg.Wait()
						return ctx.Err()
					case <-sn.stopCh:
						wg.Wait()
						return nil
					case err := <-errCh:
						sn.Close()
						wg.Wait()
						return err
					case <-time.After(10 * time.Millisecond):
					}
				}
				cancel()
				sn.Close()
				wg.Wait()
				return nil
			}
			cancel()
			sn.Close()
			wg.Wait()
			return err
		}
	}
}

// startPacerLoop executes the 20ms egress pacing and closed-loop regulation loop.
func (sn *StreamNormalizer) startPacerLoop(ctx context.Context) error {
	ticker := time.NewTicker(sn.cfg.PacerDuration())
	defer ticker.Stop()

	lastTick := time.Now()
	drainBuf := make([]byte, sn.cfg.StagingBufferCapacity)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sn.stopCh:
			return nil
		case now := <-ticker.C:
			dt := now.Sub(lastTick).Seconds()
			lastTick = now
			if dt <= 0 || dt > 1.0 {
				dt = sn.cfg.PacerIntervalMs / 1000.0
			}

			if err := sn.tickEgress(dt, drainBuf); err != nil {
				return err
			}
		}
	}
}

// tickEgress performs one regulation slice of egress pacing.
func (sn *StreamNormalizer) tickEgress(dt float64, drainBuf []byte) error {
	bufferedBytes := sn.staging.BufferedBytes()
	pps := sn.pcr.PacketsPerSecond()
	bitrateBps := pps * float64(TSPacketSize) * 8.0

	// Calculate current watermark in milliseconds
	var currentWatermarkMs float64
	if bitrateBps > 0 {
		currentWatermarkMs = (float64(bufferedBytes) * 8.0 / bitrateBps) * 1000.0
	}

	sn.stateMu.Lock()

	// 1. Startup Reservoir Protection
	if !sn.isReleased {
		if currentWatermarkMs >= sn.cfg.StartupReservoirMs {
			sn.isReleased = true
		} else {
			sn.currentWatermark = currentWatermarkMs
			sn.stateMu.Unlock()
			return nil
		}
	}

	// 2. Closed-Loop Watermark Error & Trim Calculation
	// Proven normalized proportional formula: trim = clamp(Kp * (excess/targetMs), maxTrim)
	targetMs := sn.cfg.TargetWatermarkMs
	if targetMs <= 0 {
		targetMs = 650.0
	}
	deadband := sn.cfg.DeadbandMs
	maxTrim := sn.cfg.MaxCorrectionTrim
	kp := sn.cfg.Kp

	errorMs := currentWatermarkMs - targetMs
	var trim float64

	if errorMs > deadband {
		excess := errorMs - deadband
		trim = math.Min(maxTrim, (excess/targetMs)*kp)
	} else if errorMs < -deadband {
		deficit := (-errorMs) - deadband
		trim = -math.Min(maxTrim, (deficit/targetMs)*kp)
	}

	correctionFactor := 1.0 + trim
	effectivePPS := pps * correctionFactor

	// 3. Fractional Packet Accumulator (Zero Quantization Drift)
	sn.packetCredit += effectivePPS * dt
	packetsToEmit := int(sn.packetCredit)
	sn.packetCredit -= float64(packetsToEmit)

	// Update telemetry
	sn.currentWatermark = currentWatermarkMs
	sn.correctionFactor = correctionFactor
	if currentWatermarkMs < sn.minWatermark {
		sn.minWatermark = currentWatermarkMs
	}
	if currentWatermarkMs > sn.maxWatermark {
		sn.maxWatermark = currentWatermarkMs
	}
	sn.totalWatermarkMs += currentWatermarkMs
	sn.watermarkSamples++

	sn.stateMu.Unlock()

	if packetsToEmit <= 0 {
		return nil
	}

	bytesNeeded := packetsToEmit * TSPacketSize
	if bytesNeeded > len(drainBuf) {
		bytesNeeded = len(drainBuf)
	}

	// 4. Drain paced packets from staging buffer
	n, err := sn.staging.Read(drainBuf[:bytesNeeded])
	if err != nil && err != ErrNormalizerClosed {
		return err
	}

	if n < bytesNeeded {
		atomic.AddInt64(&sn.underruns, 1)
	}

	if n > 0 {
		atomic.AddInt64(&sn.bytesOut, int64(n))
		atomic.AddInt64(&sn.packetsOut, int64(n/TSPacketSize))
		if err := sn.sink(drainBuf[:n]); err != nil {
			return err
		}
	}

	return nil
}

// Metrics returns a snapshot of runtime metrics.
func (sn *StreamNormalizer) Metrics() Metrics {
	sn.stateMu.Lock()
	defer sn.stateMu.Unlock()

	var avgWatermark float64
	if sn.watermarkSamples > 0 {
		avgWatermark = sn.totalWatermarkMs / float64(sn.watermarkSamples)
	}

	minW := sn.minWatermark
	if minW == math.MaxFloat64 {
		minW = 0
	}

	return Metrics{
		PacketsIn:        atomic.LoadInt64(&sn.packetsIn),
		PacketsOut:       atomic.LoadInt64(&sn.packetsOut),
		BytesIn:          atomic.LoadInt64(&sn.bytesIn),
		BytesOut:         atomic.LoadInt64(&sn.bytesOut),
		Underruns:        atomic.LoadInt64(&sn.underruns),
		BufferMinMs:      minW,
		BufferAvgMs:      avgWatermark,
		BufferMaxMs:      sn.maxWatermark,
		CurrentWatermark: sn.currentWatermark,
		BitrateKbps:      sn.pcr.BitrateKbps(),
		CorrectionFactor: sn.correctionFactor,
	}
}

// Close terminates the normalizer and releases resources.
func (sn *StreamNormalizer) Close() {
	if sn.closed.CompareAndSwap(false, true) {
		close(sn.stopCh)
		sn.staging.Close()
	}
}
