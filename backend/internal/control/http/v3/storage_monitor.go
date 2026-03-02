package v3

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// StorageHealth holds the cached health state for a mount point
type StorageHealth struct {
	MountStatus  StorageItemMountStatus
	HealthStatus StorageItemHealthStatus
	Access       StorageItemAccess
	FsType       string
	CheckedAt    time.Time
}

// StorageMonitor handles background storage health tracking
type StorageMonitor struct {
	mu     sync.RWMutex
	health map[string]StorageHealth
	// activeLimit limits concurrent probes to avoid system pressure
	activeLimit chan struct{}
	initOnce    sync.Once
}

func NewStorageMonitor() *StorageMonitor {
	m := &StorageMonitor{
		health: make(map[string]StorageHealth),
	}
	m.ensureInit()
	return m
}

func (m *StorageMonitor) ensureInit() {
	if m == nil {
		return
	}
	m.initOnce.Do(func() {
		if m.activeLimit == nil {
			m.activeLimit = make(chan struct{}, 8)
		}
	})
}

// Start runs the background monitoring loop
func (m *StorageMonitor) Start(ctx context.Context, interval time.Duration, s *Server) {
	if m == nil {
		return
	}
	m.ensureInit()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial check
	m.Refresh(ctx, s.getStoragePaths(ctx))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Refresh(ctx, s.getStoragePaths(ctx))
		}
	}
}

// Refresh triggers a full update of storage health for the provided paths
func (m *StorageMonitor) Refresh(ctx context.Context, paths []string) {
	if m == nil {
		return
	}
	m.ensureInit()

	defer func() {
		if r := recover(); r != nil {
			log.L().Error().
				Interface("panic", r).
				Bytes("stack", debug.Stack()).
				Msg("recovered from panic in StorageMonitor.Refresh")
		}
	}()

	// 1. Get current mounts from system (Source of Truth for mount_status)
	mounts, err := parseMountInfo()
	// If err != nil, we can't be sure about mount status via info
	parseFailed := (err != nil)

	newHealth := make(map[string]StorageHealth)

	var wg sync.WaitGroup
	var mu sync.Mutex

	log.L().Debug().Int("mounts_detected", len(mounts)).Msg("storage_monitor: mounts parsed")

	for _, path := range paths {
		if path == "" {
			continue
		}

		// Option A: Determine mount status via mountinfo FIRST
		mountInfo := findMountForPath(path, mounts)

		status := StorageItemMountStatusUnmounted
		if mountInfo.MountPoint != "" {
			status = StorageItemMountStatusMounted
		} else if parseFailed {
			status = StorageItemMountStatusUnknown
		}

		log.L().Debug().
			Str("path", path).
			Str("mount_point_found", mountInfo.MountPoint).
			Str("status", string(status)).
			Msg("storage_monitor: refresh check")

		wg.Add(1)
		go func(p string, mStatus StorageItemMountStatus, mInfo MountInfo) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.L().Error().
						Interface("panic", r).
						Bytes("stack", debug.Stack()).
						Str("path", p).
						Msg("recovered from panic in StorageMonitor probe worker")
				}
			}()

			h := StorageHealth{
				MountStatus:  mStatus,
				HealthStatus: StorageItemHealthStatusUnknown,
				Access:       StorageItemAccessNone,
				FsType:       mInfo.FsType,
				CheckedAt:    time.Now(),
			}

			if mStatus == StorageItemMountStatusUnmounted {
				// Unmounted is often expected, not an error
				h.HealthStatus = StorageItemHealthStatusUnknown
				mu.Lock()
				newHealth[p] = h
				mu.Unlock()
				return
			}

			// 2. Best-effort Probe (Lightweight I/O)
			// Concurrency control: try to get a slot.
			// Bounded pressure: If saturation occurs, we skip instead of queuing indefinitely.
			acqTimer := time.NewTimer(500 * time.Millisecond)
			defer acqTimer.Stop()

			select {
			case m.activeLimit <- struct{}{}:
				// Got a slot, perform probe
				defer func() { <-m.activeLimit }()
				res := m.probe(ctx, p)

				h.HealthStatus = res.HealthStatus
				h.Access = res.Access
			case <-acqTimer.C:
				// Too many active probes or system slow, skip this cycle
				// Distinguish between system congestion and probe failure
				h.HealthStatus = StorageItemHealthStatusSkipped
				log.L().Warn().Str("path", p).Msg("storage probe skipped: monitor busy")
			case <-ctx.Done():
				return
			}

			mu.Lock()
			newHealth[p] = h
			mu.Unlock()
		}(path, status, mountInfo)
	}

	wg.Wait()

	m.mu.Lock()
	m.health = newHealth
	m.mu.Unlock()
}

const storageProbeScript = `
p="$1"
if [ ! -d "$p" ]; then
	echo "stat_error"
	exit 0
fi

if ls "$p" >/dev/null 2>&1; then
	readable=1
else
	readable=0
fi

tmp="$p/.xg2g_probe_$$"
if (umask 077 && : > "$tmp") >/dev/null 2>&1; then
	rm -f "$tmp" >/dev/null 2>&1
	echo "rw"
	exit 0
fi

if [ "$readable" -eq 1 ]; then
	echo "ro"
else
	echo "none"
fi
`

// probe performs storage checks in a short-lived helper process.
// This avoids leaking goroutines in the main process when filesystem syscalls
// block (for example on stale network mounts).
func (m *StorageMonitor) probe(ctx context.Context, path string) ProbeResult {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	res := ProbeResult{
		HealthStatus: StorageItemHealthStatusOk,
		Access:       StorageItemAccessNone,
	}

	cmd := exec.CommandContext(probeCtx, "/bin/sh", "-c", storageProbeScript, "xg2g-storage-probe", path)
	out, err := cmd.Output()
	if err != nil {
		if probeCtx.Err() == context.DeadlineExceeded {
			res.HealthStatus = StorageItemHealthStatusTimeout
			return res
		}
		if probeCtx.Err() == context.Canceled || ctx.Err() == context.Canceled {
			res.HealthStatus = StorageItemHealthStatusUnknown
			return res
		}
		res.HealthStatus = StorageItemHealthStatusError
		return res
	}

	switch strings.TrimSpace(string(out)) {
	case "rw":
		res.Access = StorageItemAccessRw
	case "ro":
		res.Access = StorageItemAccessRo
	case "none", "stat_error":
		res.HealthStatus = StorageItemHealthStatusError
	default:
		res.HealthStatus = StorageItemHealthStatusTimeout
	}

	return res
}

type ProbeResult struct {
	HealthStatus StorageItemHealthStatus
	Access       StorageItemAccess
}

// GetHealth returns the cached health for a path
func (m *StorageMonitor) GetHealth(path string) StorageHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if h, ok := m.health[path]; ok {
		return h
	}
	return StorageHealth{
		MountStatus:  StorageItemMountStatusUnknown,
		HealthStatus: StorageItemHealthStatusUnknown,
		Access:       StorageItemAccessNone,
	}
}

// findMountForPath finds the longest prefix mount for a given path
func findMountForPath(path string, mounts map[string]MountInfo) MountInfo {
	path = filepath.Clean(path)

	var best Match
	best.Prefix = "" // Empty string length is 0

	for mountPoint, info := range mounts {
		mp := filepath.Clean(mountPoint)
		matched := false

		if path == mp {
			matched = true
		} else if mp == "/" {
			matched = true
		} else if strings.HasPrefix(path, mp+"/") {
			matched = true
		}

		if matched {
			if len(mp) > len(best.Prefix) {
				best = Match{Prefix: mp, Info: info}
			}
		}
	}

	return best.Info
}

type Match struct {
	Prefix string
	Info   MountInfo
}

func isNasFs(fs string) bool {
	fs = strings.ToLower(fs)
	return fs == "nfs" || fs == "cifs" || fs == "smb" || fs == "nfs4" || fs == "fuse.mergerfs"
}

// MountInfo represents a single entry in /proc/self/mountinfo
type MountInfo struct {
	MountPoint string
	FsType     string
	Options    []string
}

// parseMountInfo reads /proc/self/mountinfo to determine actual mounts
func parseMountInfo() (map[string]MountInfo, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	mounts := make(map[string]MountInfo)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		mountPoint := unescapeMountPath(fields[4])

		// Find separator
		separatorIdx := -1
		for i, f := range fields {
			if f == "-" {
				separatorIdx = i
				break
			}
		}

		if separatorIdx != -1 && len(fields) > separatorIdx+1 {
			fsType := fields[separatorIdx+1]
			mounts[mountPoint] = MountInfo{
				MountPoint: mountPoint,
				FsType:     fsType,
				Options:    strings.Split(fields[5], ","),
			}
		}
	}

	return mounts, scanner.Err()
}

// unescapeMountPath decodes octal escape sequences used in /proc/self/mountinfo
// (e.g., \040 -> space, \134 -> backslash).
func unescapeMountPath(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			// Check for exactly 3 octal digits
			isOctal := true
			for j := 1; j <= 3; j++ {
				if s[i+j] < '0' || s[i+j] > '7' {
					isOctal = false
					break
				}
			}
			if isOctal {
				if val, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
					sb.WriteByte(byte(val))
					i += 3
					continue
				}
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}
