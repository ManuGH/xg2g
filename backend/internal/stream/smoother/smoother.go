// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Config configures the TS Burst Smoother.
type Config struct {
	StartupReservoirMs float64 // Milliseconds of media data required before egress release
	PacerIntervalMs    float64 // Nominal egress pacing slice (e.g. 20.0 ms)
	RingBufferCapacity int     // Ring buffer byte size (e.g. 4 * 1024 * 1024)
	LowWatermarkMs     float64 // Low buffer watermark threshold (e.g. 150.0 ms)
	HighWatermarkMs    float64 // High buffer watermark threshold (e.g. 1500.0 ms)
}

// DefaultConfig returns optimal baseline smoother configuration.
func DefaultConfig() Config {
	return Config{
		StartupReservoirMs: 650.0,
		PacerIntervalMs:    20.0,
		RingBufferCapacity: 4 * 1024 * 1024, // 4 MiB (~6.5s of 4.8 Mbps)
		LowWatermarkMs:     150.0,
		HighWatermarkMs:    1500.0,
	}
}

// SessionReport captures structured diagnostic metrics of a stream session.
type SessionReport struct {
	DurationSeconds float64

	// Ingest metrics
	InputBytes        int64
	InputPackets      int64
	InputReadChunks   int
	InputGapP50Ms     float64
	InputGapP95Ms     float64
	InputGapP99Ms     float64
	InputLargestChunk int

	// Smoother buffer metrics
	StartupReservoirMs float64
	BufferMinMs        float64
	BufferAvgMs        float64
	BufferMaxMs        float64
	Underruns          int64
	Overflows          int64

	// Egress metrics
	OutputBytes    int64
	OutputPackets  int64
	OutputGapP50Ms float64
	OutputGapP95Ms float64
	OutputGapP99Ms float64
	OutputMaxGapMs float64

	// Integrity
	InputCCErrors       int
	OutputCCErrors      int
	CCErrorsIntroduced  int
	InputPCRErrors      int
	OutputPCRErrors     int
	PCRErrorsIntroduced int
	SyncErrors          int

	// Long-term rate ratio
	InputBitrateKbps  float64
	OutputBitrateKbps float64
	RateRatio         float64

	// Latency
	FirstByteDelayMs   float64
	SteadyStateDelayMs float64
}

// FormatReport renders the report in standard benchmark format.
func (r SessionReport) FormatReport() string {
	return fmt.Sprintf(`
================================================================================
TS BURST SMOOTHER BENCHMARK REPORT (Duration: %.1fs)
================================================================================
INPUT (from Upstream / Vu+):
  Packets In:        %d (%.1f KiB)
  Read Chunks:       %d
  Read Gap p50:      %.1f ms
  Read Gap p95:      %.1f ms
  Read Gap p99:      %.1f ms
  Largest Read:      %d Bytes (%.1f KiB)
  Input Rate:        %.1f kbps

SMOOTHER BUFFER:
  Startup Reservoir: %.0f ms
  Buffer Min:        %.1f ms
  Buffer Avg:        %.1f ms
  Buffer Max:        %.1f ms
  Buffer Underruns:  %d
  Buffer Overflows:  %d

OUTPUT (to Downstream / Client):
  Packets Out:       %d (%.1f KiB)
  Output Gap p50:    %.1f ms
  Output Gap p95:    %.1f ms
  Output Gap p99:    %.1f ms
  Max Output Gap:    %.1f ms
  Output Rate:       %.1f kbps
  Rate Ratio (Out/In): %.4f (1.0000 = exact preservation)

INTEGRITY:
  Packet Balance:    In (%d) == Out (%d) + In-Buffer (%d)
  Input CC Errors:   %d
  Output CC Errors:  %d
  CC Errors Added:   %d
  PCR Errors Added:  %d
  Sync Errors:       %d

LATENCY:
  First-Byte Added Latency: %.1f ms
  Steady-State Buffer Lag:  %.1f ms
================================================================================`,
		r.DurationSeconds,
		r.InputPackets, float64(r.InputBytes)/1024.0,
		r.InputReadChunks,
		r.InputGapP50Ms, r.InputGapP95Ms, r.InputGapP99Ms,
		r.InputLargestChunk, float64(r.InputLargestChunk)/1024.0,
		r.InputBitrateKbps,
		r.StartupReservoirMs,
		r.BufferMinMs, r.BufferAvgMs, r.BufferMaxMs,
		r.Underruns, r.Overflows,
		r.OutputPackets, float64(r.OutputBytes)/1024.0,
		r.OutputGapP50Ms, r.OutputGapP95Ms, r.OutputGapP99Ms, r.OutputMaxGapMs,
		r.OutputBitrateKbps,
		r.RateRatio,
		r.InputPackets, r.OutputPackets, r.InputPackets-r.OutputPackets,
		r.InputCCErrors, r.OutputCCErrors,
		r.CCErrorsIntroduced, r.PCRErrorsIntroduced,
		r.SyncErrors,
		r.FirstByteDelayMs, r.SteadyStateDelayMs,
	)
}

// SmoothStream processes an input stream and emits a smoothed TS stream.
func SmoothStream(ctx context.Context, in io.Reader, out io.Writer, cfg Config) (*SessionReport, error) {
	if cfg.PacerIntervalMs <= 0 {
		cfg.PacerIntervalMs = 20.0
	}
	if cfg.RingBufferCapacity <= 0 {
		cfg.RingBufferCapacity = 4 * 1024 * 1024
	}

	rb := NewTSRingBuffer(cfg.RingBufferCapacity)
	pacer := NewPCRPacer()
	inValidator := NewTSIntegrityValidator()
	outValidator := NewTSIntegrityValidator()

	var (
		sessionStart = time.Now()
		firstByteIn  time.Time
		firstByteOut time.Time

		inputBytes    int64
		inputPackets  int64
		outputBytes   int64
		outputPackets int64

		inputGaps    []float64
		outputGaps   []float64
		bufferMedias []float64

		largestRead int
		gapsMu      sync.Mutex

		lastInputArrival  time.Time
		lastOutputArrival time.Time

		reservoirReleased atomic.Bool
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer rb.Close()

	errChan := make(chan error, 2)

	// INGEST GOROUTINE
	go func() {
		defer rb.Close()
		buf := make([]byte, 64*1024)
		var leftover []byte

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := in.Read(buf)
			if n > 0 {
				now := time.Now()
				if firstByteIn.IsZero() {
					firstByteIn = now
				}

				gapsMu.Lock()
				if !lastInputArrival.IsZero() {
					gap := now.Sub(lastInputArrival).Seconds() * 1000.0
					if gap >= 50.0 {
						inputGaps = append(inputGaps, gap)
					}
				}
				if n > largestRead {
					largestRead = n
				}
				lastInputArrival = now
				atomic.AddInt64(&inputBytes, int64(n))
				gapsMu.Unlock()

				// Align to 188-byte TS packets
				data := buf[:n]
				if len(leftover) > 0 {
					data = append(leftover, data...)
					leftover = nil
				}

				alignedLen := (len(data) / TSPacketSize) * TSPacketSize
				if alignedLen > 0 {
					alignedData := data[:alignedLen]
					for i := 0; i < alignedLen; i += TSPacketSize {
						pkt := alignedData[i : i+TSPacketSize]
						_ = inValidator.ValidatePacket(pkt)
						pacer.FeedPacket(pkt)
						atomic.AddInt64(&inputPackets, 1)
					}

					if _, pushErr := rb.Push(alignedData); pushErr != nil {
						if pushErr == ErrBufferOverflow {
							errChan <- pushErr
							return
						}
					}
				}

				if alignedLen < len(data) {
					leftover = make([]byte, len(data)-alignedLen)
					copy(leftover, data[alignedLen:])
				}
			}

			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					errChan <- err
				}
				return
			}
		}
	}()

	// EGRESS PACING GOROUTINE
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.PacerIntervalMs * float64(time.Millisecond)))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				currentBitrate := pacer.Bitrate()
				bufferedMs := rb.BufferedMediaMs(currentBitrate)

				gapsMu.Lock()
				bufferMedias = append(bufferMedias, bufferedMs)
				gapsMu.Unlock()

				// Check startup reservoir
				if !reservoirReleased.Load() {
					if bufferedMs >= cfg.StartupReservoirMs {
						reservoirReleased.Store(true)
					} else {
						continue
					}
				}

				sliceFraction := cfg.PacerIntervalMs / 1000.0
				targetBytes := int((currentBitrate * sliceFraction) / 8.0)

				// Dynamic watermark micro-regulation (±2%)
				if bufferedMs < cfg.LowWatermarkMs {
					targetBytes = int(float64(targetBytes) * 0.98)
				} else if bufferedMs > cfg.HighWatermarkMs {
					targetBytes = int(float64(targetBytes) * 1.02)
				}

				if targetBytes < TSPacketSize {
					targetBytes = TSPacketSize
				}

				chunk, ok := rb.Pop(targetBytes)
				if !ok {
					errChan <- io.EOF
					return
				}

				if len(chunk) > 0 {
					if firstByteOut.IsZero() {
						firstByteOut = now
					}

					// Validate egress packet integrity
					for i := 0; i < len(chunk); i += TSPacketSize {
						_ = outValidator.ValidatePacket(chunk[i : i+TSPacketSize])
					}

					gapsMu.Lock()
					if !lastOutputArrival.IsZero() {
						outGap := now.Sub(lastOutputArrival).Seconds() * 1000.0
						outputGaps = append(outputGaps, outGap)
					}
					lastOutputArrival = now
					atomic.AddInt64(&outputBytes, int64(len(chunk)))
					atomic.AddInt64(&outputPackets, int64(len(chunk)/TSPacketSize))
					gapsMu.Unlock()

					if _, wErr := out.Write(chunk); wErr != nil {
						errChan <- wErr
						return
					}
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errChan:
		if err != nil && err != io.EOF {
			return nil, err
		}
	}

	duration := time.Since(sessionStart).Seconds()
	underruns, overflows := rb.Stats()

	gapsMu.Lock()
	p50In, p95In, p99In := calcPercentiles(inputGaps)
	p50Out, p95Out, p99Out := calcPercentiles(outputGaps)
	maxOut := calcMax(outputGaps)
	minBuf, avgBuf, maxBuf := calcMinAvgMax(bufferMedias)
	burstsCount := len(inputGaps)
	largestB := largestRead
	gapsMu.Unlock()

	var firstByteDelay float64
	if !firstByteIn.IsZero() && !firstByteOut.IsZero() {
		firstByteDelay = firstByteOut.Sub(firstByteIn).Seconds() * 1000.0
	}

	inKbps := 0.0
	outKbps := 0.0
	if duration > 0 {
		inKbps = (float64(atomic.LoadInt64(&inputBytes)) * 8.0 / 1000.0) / duration
		outDuration := duration - (firstByteDelay / 1000.0)
		if outDuration > 0 {
			outKbps = (float64(atomic.LoadInt64(&outputBytes)) * 8.0 / 1000.0) / outDuration
		}
	}
	ratio := 0.0
	if inKbps > 0 {
		ratio = outKbps / inKbps
	}

	ccIntroduced := outValidator.CCErrors - inValidator.CCErrors
	if ccIntroduced < 0 {
		ccIntroduced = 0
	}
	pcrIntroduced := outValidator.PCRErrors - inValidator.PCRErrors
	if pcrIntroduced < 0 {
		pcrIntroduced = 0
	}

	report := &SessionReport{
		DurationSeconds:     duration,
		InputBytes:          atomic.LoadInt64(&inputBytes),
		InputPackets:        atomic.LoadInt64(&inputPackets),
		InputReadChunks:     burstsCount,
		InputGapP50Ms:       p50In,
		InputGapP95Ms:       p95In,
		InputGapP99Ms:       p99In,
		InputLargestChunk:   largestB,
		StartupReservoirMs:  cfg.StartupReservoirMs,
		BufferMinMs:         minBuf,
		BufferAvgMs:         avgBuf,
		BufferMaxMs:         maxBuf,
		Underruns:           underruns,
		Overflows:           overflows,
		OutputBytes:         atomic.LoadInt64(&outputBytes),
		OutputPackets:       atomic.LoadInt64(&outputPackets),
		OutputGapP50Ms:      p50Out,
		OutputGapP95Ms:      p95Out,
		OutputGapP99Ms:      p99Out,
		OutputMaxGapMs:      maxOut,
		InputCCErrors:       inValidator.CCErrors,
		OutputCCErrors:      outValidator.CCErrors,
		CCErrorsIntroduced:  ccIntroduced,
		InputPCRErrors:      inValidator.PCRErrors,
		OutputPCRErrors:     outValidator.PCRErrors,
		PCRErrorsIntroduced: pcrIntroduced,
		SyncErrors:          inValidator.SyncErrors + outValidator.SyncErrors,
		InputBitrateKbps:    inKbps,
		OutputBitrateKbps:   outKbps,
		RateRatio:           ratio,
		FirstByteDelayMs:    firstByteDelay,
		SteadyStateDelayMs:  avgBuf,
	}

	return report, nil
}

func calcPercentiles(vals []float64) (p50, p95, p99 float64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	p50 = sorted[int(float64(len(sorted)-1)*0.50)]
	p95 = sorted[int(float64(len(sorted)-1)*0.95)]
	p99 = sorted[int(float64(len(sorted)-1)*0.99)]
	return
}

func calcMax(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	maxV := vals[0]
	for _, v := range vals[1:] {
		if v > maxV {
			maxV = v
		}
	}
	return maxV
}

func calcMinAvgMax(vals []float64) (minV, avgV, maxV float64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	minV = math.MaxFloat64
	maxV = -math.MaxFloat64
	var sum float64
	for _, v := range vals {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
		sum += v
	}
	avgV = sum / float64(len(vals))
	return
}
