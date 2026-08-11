package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var globalThrottleKbps int64 = 0 // 0 = unthrottled, >0 = target kbps

func main() {
	port := "8899"
	hlsDir := "/tmp/xg2g_abr_p61b"

	if len(os.Args) > 1 {
		hlsDir = os.Args[1]
	}
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	mux := http.NewServeMux()

	// Control API to adjust bandwidth throttle dynamically
	mux.HandleFunc("/api/throttle", func(w http.ResponseWriter, r *http.Request) {
		kbpsStr := r.URL.Query().Get("kbps")
		kbps, _ := strconv.ParseInt(kbpsStr, 10, 64)
		atomic.StoreInt64(&globalThrottleKbps, kbps)
		w.Header().Set("Content-Type", "application/json")
		fmt.Printf("[SERVER] Throttling set to %d kbps\n", kbps)
		fmt.Fprintf(w, `{"status":"ok","kbps":%d}`, kbps)
	})

	// HLS File Server with explicit chunked bandwidth throttling for .ts segments
	fileServer := http.FileServer(http.Dir(hlsDir))
	mux.HandleFunc("/hls/", func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/hls/")
		filePath := filepath.Join(hlsDir, relPath)

		currentKbps := atomic.LoadInt64(&globalThrottleKbps)
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
	spikeDir := "/Users/manuel/StudioProjects/xg2g/hack/spikes/abr_p61b"
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

	fmt.Printf("=== P6.1b ABR Spike Server Running ===\n")
	fmt.Printf("Listening on: http://localhost:%s\n", port)
	fmt.Printf("HLS Stream:   http://localhost:%s/hls/master.m3u8\n", port)
	fmt.Printf("hls.js Test:  http://localhost:%s/\n", port)
	fmt.Printf("Safari Test:  http://localhost:%s/safari.html\n", port)

	if err := http.ListenAndServe(":"+port, corsHandler); err != nil {
		fmt.Printf("[FAIL] Server stopped: %v\n", err)
		os.Exit(1)
	}
}
