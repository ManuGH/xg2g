package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	globalThrottleKbps int64 = 0       // 0 = unthrottled, >0 = target kbps
	globalNetworkState       = "online" // "online" or "drop"
	stateMu            sync.RWMutex
)

func getNetworkState() (string, int64) {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return globalNetworkState, atomic.LoadInt64(&globalThrottleKbps)
}

func setNetworkState(state string, kbps int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	globalNetworkState = state
	atomic.StoreInt64(&globalThrottleKbps, kbps)
}

func main() {
	port := "8899"
	hlsDir := "/tmp/xg2g_abr_p61c"

	if len(os.Args) > 1 {
		hlsDir = os.Args[1]
	}
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	mux := http.NewServeMux()

	// Control API for driving network profile
	// /api/network?state=online&kbps=3500 OR /api/network?state=drop
	mux.HandleFunc("/api/network", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state == "" {
			state = "online"
		}
		kbpsStr := r.URL.Query().Get("kbps")
		kbps, _ := strconv.ParseInt(kbpsStr, 10, 64)

		setNetworkState(state, kbps)
		w.Header().Set("Content-Type", "application/json")
		fmt.Printf("[SERVER NETWORK API] State=%s, TargetKbps=%d\n", state, kbps)
		fmt.Fprintf(w, `{"status":"ok","state":"%s","kbps":%d}`, state, kbps)
	})

	// HLS File Server with Silent Socket Drop and Chunked Throttling for .ts media segments
	fileServer := http.FileServer(http.Dir(hlsDir))
	mux.HandleFunc("/hls/", func(w http.ResponseWriter, r *http.Request) {
		state, currentKbps := getNetworkState()

		// 1. Silent Tunnel Drop Simulation: block incoming segment requests without instant HTTP errors
		if state == "drop" && strings.HasSuffix(r.URL.Path, ".ts") {
			fmt.Printf("[SERVER DROP] Silent socket drop for request: %s (holding 12s)\n", r.URL.Path)
			// Sleep 12s to let client buffer exhaust and trigger genuine stall / request timeout
			select {
			case <-time.After(12 * time.Second):
			case <-r.Context().Done():
			}
			return
		}

		relPath := strings.TrimPrefix(r.URL.Path, "/hls/")
		filePath := filepath.Join(hlsDir, relPath)

		// 2. Chunked Bandwidth Throttling for .ts media segments
		if currentKbps > 0 && strings.HasSuffix(filePath, ".ts") {
			data, err := os.ReadFile(filePath)
			if err != nil {
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Content-Type", "video/mp2t")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))

			const chunkSize = 16 * 1024 // 16KB chunking
			flusher, _ := w.(http.Flusher)

			start := time.Now()
			for i := 0; i < len(data); i += chunkSize {
				// Re-check if network state turned to drop mid-stream
				st, _ := getNetworkState()
				if st == "drop" {
					fmt.Printf("[SERVER DROP] Mid-segment drop triggered for: %s\n", relPath)
					time.Sleep(10 * time.Second)
					return
				}

				end := i + chunkSize
				if end > len(data) {
					end = len(data)
				}
				n, err := w.Write(data[i:end])
				if err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				bits := int64(n * 8)
				delayNs := (bits * int64(time.Second)) / (currentKbps * 1000)
				if delayNs > 0 {
					time.Sleep(time.Duration(delayNs))
				}
			}
			elapsedSec := time.Since(start).Seconds()
			actualKbps := (float64(len(data)*8) / 1000.0) / elapsedSec
			fmt.Printf("[SERVER THROTTLE] Served %s (%d bytes) in %.2fs (effective rate: %.0f kbps, target: %d kbps)\n",
				relPath, len(data), elapsedSec, actualKbps, currentKbps)
			return
		}

		http.StripPrefix("/hls/", fileServer).ServeHTTP(w, r)
	})

	// Serve static spike assets (index.html, safari.html, hls.min.js)
	spikeDir := "/Users/manuel/StudioProjects/xg2g/hack/spikes/abr_p61c"
	if len(os.Args) > 3 {
		spikeDir = os.Args[3]
	}
	mux.Handle("/", http.FileServer(http.Dir(spikeDir)))

	// CORS Wrapper
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	fmt.Printf("=== P6.1c ABR Spike Server Running ===\n")
	fmt.Printf("Listening on: http://localhost:%s\n", port)
	fmt.Printf("HLS Stream:   http://localhost:%s/hls/master.m3u8\n", port)
	fmt.Printf("hls.js Test:  http://localhost:%s/\n", port)
	fmt.Printf("Safari Test:  http://localhost:%s/safari.html\n", port)

	if err := http.ListenAndServe(":"+port, corsHandler); err != nil {
		fmt.Printf("[FAIL] Server stopped: %v\n", err)
		os.Exit(1)
	}
}
