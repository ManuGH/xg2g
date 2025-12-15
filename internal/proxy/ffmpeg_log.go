package proxy

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func isValidFFmpegLogLevel(v string) bool {
	switch strings.ToLower(v) {
	case "quiet", "panic", "fatal", "error", "warning", "info", "verbose", "debug", "trace":
		return true
	default:
		return false
	}
}

func logLevelArgs(defaultLevel, override string) []string {
	level := defaultLevel
	if strings.TrimSpace(override) != "" && isValidFFmpegLogLevel(override) {
		level = override
	}
	return []string{"-loglevel", level}
}

// FFmpegStats holds parsed metrics from FFmpeg output.
type FFmpegStats struct {
	Speed       float64
	BitrateKBPS float64
	FPS         float64
	Frame       int
	Time        time.Duration
	Valid       bool
}

// ParseFFmpegStats parses a standard FFmpeg progress line into structured stats.
// Strategy: Robust field extraction (substring search) rather than strict regex.
// Acceptable line example: "frame=  123 fps= 25 q=28.0 size=    1234kB time=00:00:12.34 bitrate= 800.0kbits/s speed=1.0x"
// Returns nil if the line doesn't look like a progress line.
func ParseFFmpegStats(line string) *FFmpegStats {
	// Quick check: must have at least "frame=" or "time=" or "bitrate=" to be worth parsing
	if !strings.Contains(line, "frame=") && !strings.Contains(line, "time=") && !strings.Contains(line, "bitrate=") {
		return nil
	}

	stats := &FFmpegStats{}
	foundAny := false

	// Helper to extract value after key
	extract := func(key string) string {
		idx := strings.Index(line, key)
		if idx == -1 {
			return ""
		}
		// Start after key
		valStart := idx + len(key)
		if valStart >= len(line) {
			return ""
		}

		// Skip leading spaces
		rest := line[valStart:]
		val := strings.TrimLeft(rest, " ")
		if val == "" {
			return ""
		}

		// Read until space or end
		spaceIdx := strings.Index(val, " ")
		if spaceIdx == -1 {
			return val
		}
		return val[:spaceIdx]
	}

	// 1. Parse Speed (e.g., "speed=1.00x", "speed= 1.0x", "speed=N/A")
	if val := extract("speed="); val != "" {
		val = strings.TrimSuffix(val, "x")
		if val != "N/A" {
			if s, err := strconv.ParseFloat(val, 64); err == nil {
				stats.Speed = s
				foundAny = true
			}
		}
	}

	// 2. Parse Bitrate (e.g., "bitrate= 800.0kbits/s", "bitrate=N/A")
	if val := extract("bitrate="); val != "" {
		if val != "N/A" {
			// Remove units
			val = strings.TrimSuffix(val, "kbits/s")
			val = strings.TrimSuffix(val, "kb/s")
			if b, err := strconv.ParseFloat(val, 64); err == nil {
				stats.BitrateKBPS = b
				foundAny = true
			}
		}
	}

	// 3. Parse FPS (e.g., "fps= 25", "fps=24.5")
	if val := extract("fps="); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			stats.FPS = f
			foundAny = true
		}
	}

	// 4. Parse Frame (e.g., "frame=  123")
	if val := extract("frame="); val != "" {
		if f, err := strconv.Atoi(val); err == nil {
			stats.Frame = f
			foundAny = true
		}
	}

	// 5. Parse Time (e.g., "time=00:00:12.34")
	if val := extract("time="); val != "" {
		if val != "N/A" {
			if d, err := parseFFmpegTime(val); err == nil {
				stats.Time = d
				foundAny = true
			}
		}
	}

	if !foundAny {
		return nil
	}

	stats.Valid = true
	return stats
}

// parseFFmpegTime parses "HH:MM:SS.mm" format.
func parseFFmpegTime(val string) (time.Duration, error) {
	// Replace dot with comma if needed (though FFmpeg usually uses dot or nothing)
	// standard layout: 15:04:05.00
	// But time.ParseDuration expects "1h2m3s"
	// So we manually parse parts
	parts := strings.Split(val, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time format")
	}

	hours, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, err
	}
	mins, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, err
	}
	secs, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, err
	}

	totalSecs := hours*3600 + mins*60 + secs
	return time.Duration(totalSecs * float64(time.Second)), nil
}
