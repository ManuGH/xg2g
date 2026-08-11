package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
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
	SeqNum   int
	Duration float64
	Filename string
	PTSStart float64
}

func main() {
	outDir := "/tmp/xg2g_abr_p61c"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	fmt.Printf("=== P6.1c 3-Tier HLS Manifest & Real FFprobe PTS Sanity Checker ===\n")
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

	if len(variants) != 3 {
		fmt.Printf("[FAIL] Expected 3 variants in master playlist, found %d\n", len(variants))
		os.Exit(1)
	}
	fmt.Printf("[PASS] Master playlist contains 3 variants:\n")
	for _, v := range variants {
		fmt.Printf("       - Rendition: %s | Bandwidth: %d bps | Path: %s\n", v.Name, v.Bandwidth, v.Path)
	}

	// 3. Verify sliding window and extract real FFprobe PTS timestamps
	renditions := []string{"1080p", "720p", "480p"}
	variantSegs := make(map[string][]SegmentInfo)

	for _, rend := range renditions {
		pPath := filepath.Join(outDir, rend, "index.m3u8")
		segs, err := parseVariantPlaylist(pPath)
		if err != nil {
			fmt.Printf("[FAIL] Could not parse %s variant playlist: %v\n", rend, err)
			os.Exit(1)
		}
		if len(segs) < 3 || len(segs) > 12 {
			fmt.Printf("[FAIL] %s sliding window count out of bounds (%d segments; expected 3-10)\n", rend, len(segs))
			os.Exit(1)
		}

		// Extract real PTS timestamps using ffprobe for each segment
		for i := range segs {
			tsPath := filepath.Join(outDir, rend, segs[i].Filename)
			pts, err := extractRealPTS(tsPath)
			if err != nil {
				fmt.Printf("[FAIL] Could not extract FFprobe PTS for %s: %v\n", tsPath, err)
				os.Exit(1)
			}
			segs[i].PTSStart = pts
		}

		variantSegs[rend] = segs
		fmt.Printf("[PASS] %s sliding window verified: %d segments, real FFprobe PTS extracted (first: %.3fs, last: %.3fs)\n",
			rend, len(segs), segs[0].PTSStart, segs[len(segs)-1].PTSStart)
	}

	// 4. Map segments by Sequence Number & compare REAL FFprobe PTS timestamps
	seqMap := make(map[int]map[string]SegmentInfo)
	for rend, segs := range variantSegs {
		for _, s := range segs {
			if _, ok := seqMap[s.SeqNum]; !ok {
				seqMap[s.SeqNum] = make(map[string]SegmentInfo)
			}
			seqMap[s.SeqNum][rend] = s
		}
	}

	const maxDurationToleranceSec = 0.050 // <= 50ms tolerance
	matchingSeqs := 0
	maxPTSDelta := 0.0

	for seq, m := range seqMap {
		if s1080, ok1080 := m["1080p"]; ok1080 {
			if s720, ok720 := m["720p"]; ok720 {
				if s480, ok480 := m["480p"]; ok480 {
					matchingSeqs++
					d1 := math.Abs(s1080.PTSStart - s720.PTSStart)
					d2 := math.Abs(s720.PTSStart - s480.PTSStart)
					d3 := math.Abs(s1080.PTSStart - s480.PTSStart)

					currMax := math.Max(d1, math.Max(d2, d3))
					if currMax > maxPTSDelta {
						maxPTSDelta = currMax
					}

					if currMax > maxDurationToleranceSec {
						fmt.Printf("[FAIL] Real FFprobe PTS start mismatch at seq_%05d.ts: 1080p=%.3fs, 720p=%.3fs, 480p=%.3fs (delta=%.2fms > 50ms)\n",
							seq, s1080.PTSStart, s720.PTSStart, s480.PTSStart, currMax*1000)
						os.Exit(1)
					}
				}
			}
		}
	}

	if matchingSeqs == 0 {
		fmt.Printf("[FAIL] Zero overlapping sequence numbers found across 3 variants\n")
		os.Exit(1)
	}

	fmt.Printf("[PASS] Real MPEG-TS Container PTS Start Alignment verified across %d overlapping segments via FFprobe (max delta: %.2fms <= 50ms tolerance)\n",
		matchingSeqs, maxPTSDelta*1000)

	// 5. Verify segment rolling growth over 3 seconds
	fmt.Println("Waiting 3s to verify sliding window rolling growth...")
	time.Sleep(3 * time.Second)

	segs1080New, _ := parseVariantPlaylist(filepath.Join(outDir, "1080p", "index.m3u8"))
	if len(segs1080New) == 0 || segs1080New[len(segs1080New)-1].SeqNum <= variantSegs["1080p"][len(variantSegs["1080p"])-1].SeqNum {
		fmt.Printf("[FAIL] Sliding window not advancing sequence numbers\n")
		os.Exit(1)
	}
	fmt.Printf("[PASS] Sliding window rolling growth verified: 1080p latest seq %d -> %d\n",
		variantSegs["1080p"][len(variantSegs["1080p"])-1].SeqNum, segs1080New[len(segs1080New)-1].SeqNum)

	fmt.Println("\n=== ALL 3-TIER MANIFEST & REAL FFPROBE PTS SANITY CHECKS PASSED ===")
}

// extractRealPTS calls ffprobe to read the actual first video packet PTS timestamp from a .ts file
func extractRealPTS(tsPath string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "packet=pts_time",
		"-select_streams", "v:0",
		"-read_intervals", "%+#1",
		"-of", "default=noprint_wrappers=1:nokey=1",
		tsPath,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(out.String())
	pts, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid pts_time %q: %w", raw, err)
	}
	return pts, nil
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
			parts := strings.Split(line, ",")
			for _, p := range parts {
				if strings.Contains(p, "BANDWIDTH=") {
					bwStr := p[strings.Index(p, "BANDWIDTH=")+len("BANDWIDTH="):]
					currentBW, _ = strconv.Atoi(strings.Trim(bwStr, "\""))
				}
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			name := "variant"
			if strings.Contains(line, "1080p") {
				name = "1080p"
			} else if strings.Contains(line, "720p") {
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
	var mediaSeq int

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			seqStr := strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")
			mediaSeq, _ = strconv.Atoi(seqStr)
		} else if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(durStr, ","); idx != -1 {
				durStr = durStr[:idx]
			}
			currentDuration, _ = strconv.ParseFloat(durStr, 64)
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			seqNo := mediaSeq + len(segments)
			if strings.HasPrefix(line, "seq_") {
				numStr := strings.TrimPrefix(line, "seq_")
				numStr = strings.TrimSuffix(numStr, ".ts")
				if parsedNum, err := strconv.Atoi(numStr); err == nil {
					seqNo = parsedNum
				}
			}

			segments = append(segments, SegmentInfo{
				SeqNum:   seqNo,
				Duration: currentDuration,
				Filename: line,
			})
		}
	}
	return segments, scanner.Err()
}
