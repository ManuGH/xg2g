package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type VariantInfo struct {
	Name      string
	Bandwidth int
	Path      string
}

type SegmentInfo struct {
	Duration float64
	Filename string
}

func main() {
	outDir := "/tmp/xg2g_abr_p61b"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	fmt.Printf("=== P6.1b HLS Manifest Sanity Checker ===\n")
	fmt.Printf("Checking directory: %s\n\n", outDir)

	// 1. Verify FFmpeg process state
	pidFile := filepath.Join(outDir, "ffmpeg.pid")
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Printf("[FAIL] Could not read PID file: %v\n", err)
		os.Exit(1)
	}
	pidStr := strings.TrimSpace(string(pidBytes))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		fmt.Printf("[FAIL] Invalid PID %q: %v\n", pidStr, err)
		os.Exit(1)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("[FAIL] FFmpeg process PID %d not found: %v\n", pid, err)
		os.Exit(1)
	}
	// On Unix, signal 0 tests process existence
	if err := process.Signal(syscall.Signal(0)); err != nil {
		fmt.Printf("[FAIL] FFmpeg process PID %d is not running: %v\n", pid, err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] FFmpeg process running with PID %d\n", pid)

	// 2. Parse master.m3u8
	masterPath := filepath.Join(outDir, "master.m3u8")
	variants, err := parseMasterPlaylist(masterPath)
	if err != nil {
		fmt.Printf("[FAIL] Master playlist parse error: %v\n", err)
		os.Exit(1)
	}

	if len(variants) != 2 {
		fmt.Printf("[FAIL] Expected 2 variants in master playlist, found %d\n", len(variants))
		os.Exit(1)
	}
	fmt.Printf("[PASS] Master playlist contains %d variants:\n", len(variants))
	for _, v := range variants {
		fmt.Printf("       - Rendition: %s | Bandwidth: %d bps | Path: %s\n", v.Name, v.Bandwidth, v.Path)
	}

	// 3. Verify variant segment synchronization
	var720pPath := filepath.Join(outDir, "720p", "index.m3u8")
	var480pPath := filepath.Join(outDir, "480p", "index.m3u8")

	segs720p, err := parseVariantPlaylist(var720pPath)
	if err != nil {
		fmt.Printf("[FAIL] Could not parse 720p variant playlist: %v\n", err)
		os.Exit(1)
	}
	segs480p, err := parseVariantPlaylist(var480pPath)
	if err != nil {
		fmt.Printf("[FAIL] Could not parse 480p variant playlist: %v\n", err)
		os.Exit(1)
	}

	if len(segs720p) == 0 || len(segs480p) == 0 {
		fmt.Printf("[FAIL] Variant playlists contain zero segments (720p: %d, 480p: %d)\n", len(segs720p), len(segs480p))
		os.Exit(1)
	}
	fmt.Printf("[PASS] Initial segment counts: 720p=%d, 480p=%d\n", len(segs720p), len(segs480p))

	// Compare segment durations between matching indices (tolerance <= 50ms = 0.050s)
	const maxDurationToleranceSec = 0.050
	minSegCount := len(segs720p)
	if len(segs480p) < minSegCount {
		minSegCount = len(segs480p)
	}

	maxDiff := 0.0
	for i := 0; i < minSegCount; i++ {
		diff := math.Abs(segs720p[i].Duration - segs480p[i].Duration)
		if diff > maxDiff {
			maxDiff = diff
		}
		if diff > maxDurationToleranceSec {
			fmt.Printf("[FAIL] Segment #%d duration mismatch: 720p=%.3fs vs 480p=%.3fs (diff=%.3fms > 50ms tolerance)\n",
				i, segs720p[i].Duration, segs480p[i].Duration, diff*1000)
			os.Exit(1)
		}
	}
	fmt.Printf("[PASS] Segment duration alignment verified across %d segments (max delta: %.2fms <= 50ms tolerance)\n",
		minSegCount, maxDiff*1000)

	// 4. Verify segment count growth over 3 seconds
	fmt.Println("Waiting 3s to verify segment growth...")
	time.Sleep(3 * time.Second)

	segs720pNew, _ := parseVariantPlaylist(var720pPath)
	segs480pNew, _ := parseVariantPlaylist(var480pPath)

	if len(segs720pNew) <= len(segs720p) || len(segs480pNew) <= len(segs480p) {
		fmt.Printf("[FAIL] Variant playlists not appending segments: 720p (%d -> %d), 480p (%d -> %d)\n",
			len(segs720p), len(segs720pNew), len(segs480p), len(segs480pNew))
		os.Exit(1)
	}
	fmt.Printf("[PASS] Segment growth verified: 720p (%d -> %d), 480p (%d -> %d)\n",
		len(segs720p), len(segs720pNew), len(segs480p), len(segs480pNew))

	fmt.Println("\n=== ALL MANIFEST PRE-FLIGHT SANITY CHECKS PASSED ===")
}

func parseMasterPlaylist(path string) ([]VariantInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var variants []VariantInfo
	scanner := bufio.NewScanner(file)
	var currentBW int

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			// Extract BANDWIDTH
			parts := strings.Split(line, ",")
			for _, p := range parts {
				if strings.HasPrefix(p, "BANDWIDTH=") || strings.Contains(p, "BANDWIDTH=") {
					bwStr := strings.TrimPrefix(p, "BANDWIDTH=")
					if idx := strings.Index(p, "BANDWIDTH="); idx != -1 {
						bwStr = p[idx+len("BANDWIDTH="):]
					}
					currentBW, _ = strconv.Atoi(strings.Trim(bwStr, "\""))
				}
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			name := "variant"
			if strings.Contains(line, "720p") {
				name = "720p"
			} else if strings.Contains(line, "480p") {
				name = "480p"
			}
			variants = append(variants, VariantInfo{
				Name:      name,
				Bandwidth: currentBW,
				Path:      line,
			})
		}
	}
	return variants, scanner.Err()
}

func parseVariantPlaylist(path string) ([]SegmentInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var segments []SegmentInfo
	scanner := bufio.NewScanner(file)
	var currentDuration float64

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(durStr, ","); idx != -1 {
				durStr = durStr[:idx]
			}
			currentDuration, _ = strconv.ParseFloat(durStr, 64)
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			segments = append(segments, SegmentInfo{
				Duration: currentDuration,
				Filename: line,
			})
		}
	}
	return segments, scanner.Err()
}
