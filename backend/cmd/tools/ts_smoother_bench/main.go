// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/smoother"
)

// TraceChunk records one burst with its exact inter-arrival delay.
type TraceChunk struct {
	DelayNs int64
	Data    []byte
}

func main() {
	mode := flag.String("mode", "live", "Mode: 'live' (direct to Vu+), 'record' (save timed trace), 'replay' (deterministic replay)")
	streamURL := flag.String("url", "http://10.10.55.64:8001/1:0:19:132F:3EF:1:C00000:0:0:0:", "Enigma2 stream URL")
	traceFile := flag.String("trace", "orf1_trace.bin", "Trace file path for record/replay")
	duration := flag.Duration("duration", 30*time.Second, "Capture/Benchmark duration")
	reservoirMs := flag.Float64("reservoir", 650.0, "Startup reservoir in milliseconds")
	pacerMs := flag.Float64("pacer", 20.0, "Pacer slice interval in milliseconds")
	matrix := flag.Bool("matrix", false, "Run full reservoir cliff matrix [250, 400, 500, 575, 650, 750, 850ms]")
	flag.Parse()

	switch *mode {
	case "record":
		recordTrace(*streamURL, *traceFile, *duration)
	case "replay":
		if *matrix {
			runReplayMatrix(*traceFile, *pacerMs)
		} else {
			runReplaySingle(*traceFile, *reservoirMs, *pacerMs)
		}
	case "live":
		runLive(*streamURL, *duration, *reservoirMs, *pacerMs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

func recordTrace(url, tracePath string, duration time.Duration) {
	fmt.Printf("🔴 Recording %v live trace from %s -> %s\n", duration, url, tracePath)
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create request: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	// #nosec G304 -- developer bench tool; the trace path is the operator's own flag
	f, err := os.Create(tracePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create trace file: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 64*1024)
	lastArrival := time.Now()
	var totalBytes int64
	var chunks int

	for {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			now := time.Now()
			delay := now.Sub(lastArrival).Nanoseconds()
			lastArrival = now

			// Write: [int64 delayNs][int32 dataLen][bytes]
			_ = binary.Write(f, binary.BigEndian, delay)
			_ = binary.Write(f, binary.BigEndian, int32(n)) // #nosec G115 -- n is a read length bounded by len(buf)
			_, _ = f.Write(buf[:n])

			totalBytes += int64(n)
			chunks++
		}
		if rErr != nil {
			break
		}
	}

	fmt.Printf("✅ Recording finished: %d chunks, %.1f KiB recorded in %s\n", chunks, float64(totalBytes)/1024.0, tracePath)
}

// TimedTraceReader replays recorded chunks with exact original arrival timing.
type TimedTraceReader struct {
	chunks []TraceChunk
	idx    int
}

func LoadTrace(tracePath string) ([]TraceChunk, error) {
	// #nosec G304 -- developer bench tool; the trace path is the operator's own flag
	data, err := os.ReadFile(tracePath)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	var chunks []TraceChunk

	for r.Len() > 0 {
		var delayNs int64
		var dataLen int32
		if err := binary.Read(r, binary.BigEndian, &delayNs); err != nil {
			break
		}
		if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
			break
		}
		chunkData := make([]byte, dataLen)
		if _, err := io.ReadFull(r, chunkData); err != nil {
			break
		}
		chunks = append(chunks, TraceChunk{DelayNs: delayNs, Data: chunkData})
	}

	return chunks, nil
}

func (tr *TimedTraceReader) Read(p []byte) (int, error) {
	if tr.idx >= len(tr.chunks) {
		return 0, io.EOF
	}

	c := tr.chunks[tr.idx]
	tr.idx++

	if c.DelayNs > 0 && c.DelayNs < int64(5*time.Second) {
		time.Sleep(time.Duration(c.DelayNs))
	}

	n := copy(p, c.Data)
	return n, nil
}

func runReplaySingle(tracePath string, reservoirMs, pacerMs float64) {
	chunks, err := LoadTrace(tracePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load trace: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("▶️ Replaying deterministic trace (%d chunks) with reservoir=%.0fms, pacer=%.0fms\n",
		len(chunks), reservoirMs, pacerMs)

	reader := &TimedTraceReader{chunks: chunks}
	cfg := smoother.DefaultConfig()
	cfg.StartupReservoirMs = reservoirMs
	cfg.PacerIntervalMs = pacerMs

	report, err := smoother.SmoothStream(context.Background(), reader, io.Discard, cfg)
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if report != nil {
		fmt.Println(report.FormatReport())
	}
}

func runReplayMatrix(tracePath string, pacerMs float64) {
	chunks, err := LoadTrace(tracePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load trace: %v\n", err)
		os.Exit(1)
	}

	reservoirs := []float64{250.0, 400.0, 500.0, 575.0, 650.0, 750.0, 850.0}
	reports := make([]*smoother.SessionReport, len(reservoirs))
	var wg sync.WaitGroup

	fmt.Println("=========================================================================================")
	fmt.Printf("DETERMINISTIC RESERVOIR CLIFF MATRIX (%d recorded chunks, 100%% identical input)\n", len(chunks))
	fmt.Println("=========================================================================================")
	fmt.Printf("⏳ Running 7 deterministic simulations concurrently in parallel...\n")

	for i, res := range reservoirs {
		wg.Add(1)
		go func(idx int, r float64) {
			defer wg.Done()
			reader := &TimedTraceReader{chunks: chunks}
			cfg := smoother.DefaultConfig()
			cfg.StartupReservoirMs = r
			cfg.PacerIntervalMs = pacerMs

			rep, sErr := smoother.SmoothStream(context.Background(), reader, io.Discard, cfg)
			if sErr == nil || sErr == io.EOF {
				reports[idx] = rep
			}
		}(i, res)
	}

	wg.Wait()

	fmt.Printf("%-12s | %-12s | %-12s | %-12s | %-12s | %-10s\n",
		"Reservoir", "Underruns", "Egress p99", "Added Latency", "Rate Ratio", "Verdict")
	fmt.Println("-------------+--------------+--------------+---------------+--------------+-----------")

	for i, res := range reservoirs {
		rep := reports[i]
		if rep == nil {
			fmt.Printf("%-10.0fms | ERROR\n", res)
			continue
		}

		verdict := "✅ PASS"
		if rep.Underruns > 0 {
			verdict = "❌ FAIL"
		}

		fmt.Printf("%-10.0fms | %-12d | %-10.1fms | %-11.1fms | %-12.4f | %-10s\n",
			res, rep.Underruns, rep.OutputGapP99Ms, rep.FirstByteDelayMs, rep.RateRatio, verdict)
	}
	fmt.Println("=========================================================================================")
}

func runLive(url string, duration time.Duration, reservoirMs, pacerMs float64) {
	fmt.Printf("🎯 Starting Live TS Smoother Benchmark against:\n   URL: %s\n   Duration: %v | Reservoir: %.0fms | Pacer: %.0fms\n\n",
		url, duration, reservoirMs, pacerMs)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create request: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	cfg := smoother.DefaultConfig()
	cfg.StartupReservoirMs = reservoirMs
	cfg.PacerIntervalMs = pacerMs

	report, err := smoother.SmoothStream(ctx, resp.Body, io.Discard, cfg)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if report != nil {
		fmt.Println(report.FormatReport())
	}
}
