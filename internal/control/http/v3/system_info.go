package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// SystemInfo represents the full system information response
type SystemInfo struct {
	Hardware HardwareInfo `json:"hardware"`
	Software SoftwareInfo `json:"software"`
	Tuners   []TunerInfo  `json:"tuners"`
	Network  NetworkInfo  `json:"network"`
	Storage  StorageInfo  `json:"storage"`
	Runtime  RuntimeInfo  `json:"runtime"`
	Resource ResourceInfo `json:"resource"`
}

// HardwareInfo represents hardware information
type HardwareInfo struct {
	Brand              string `json:"brand,omitempty"`
	Model              string `json:"model,omitempty"`
	Boxtype            string `json:"boxtype,omitempty"`
	Chipset            string `json:"chipset,omitempty"`
	ChipsetDescription string `json:"chipset_description,omitempty"`
}

// SoftwareInfo represents software versions
type SoftwareInfo struct {
	OEVersion     string `json:"oe_version,omitempty"`
	ImageDistro   string `json:"image_distro,omitempty"`
	ImageVersion  string `json:"image_version,omitempty"`
	EnigmaVersion string `json:"enigma_version,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`
	DriverDate    string `json:"driver_date,omitempty"`
	WebIFVersion  string `json:"webif_version,omitempty"`
}

// TunerInfo represents a single tuner
type TunerInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// NetworkInfo represents network configuration
type NetworkInfo struct {
	Interfaces []NetworkInterfaceInfo `json:"interfaces"`
}

// NetworkInterfaceInfo represents a network interface
type NetworkInterfaceInfo struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Speed string `json:"speed"`
	MAC   string `json:"mac"`
	IP    string `json:"ip"`
	IPv6  string `json:"ipv6"`
	DHCP  bool   `json:"dhcp"`
}

// StorageInfo represents storage devices and shares
type StorageInfo struct {
	Devices   *[]StorageItem `json:"devices,omitempty"`
	Locations *[]StorageItem `json:"locations,omitempty"`
}

// RuntimeInfo represents runtime information
type RuntimeInfo struct {
	Uptime string `json:"uptime"`
}

// ResourceInfo represents CPU and memory usage
type ResourceInfo struct {
	MemoryTotal     string `json:"memory_total"`
	MemoryAvailable string `json:"memory_available"`
	MemoryUsed      string `json:"memory_used"`
}

// GetSystemInfo implements the system info endpoint
// GET /api/v3/system/info
func (s *Server) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	// Wrap writer to track header status with transparent interface passthrough
	w, tracker := wrapResponseWriter(w)

	defer func() {
		if p := recover(); p != nil {
			log.L().Error().
				Interface("panic", p).
				Bytes("stack", debug.Stack()).
				Msg("recovered from panic in GetSystemInfo")

			if !tracker.WroteHeader() {
				writeProblem(w, r, http.StatusInternalServerError, "system/panic", "Internal Server Error", "PANIC", "A serious error occurred while processing system information", nil)
			}
		}
	}()
	ctx := r.Context()

	// Get OpenWebIF client using standard factory method
	owiClient := s.owi(s.cfg, s.snap)
	// Type assert to concrete client (owi returns interface)
	client, ok := owiClient.(*openwebif.Client)
	if !ok || client == nil {
		writeProblem(w, r, http.StatusServiceUnavailable,
			"system/client_unavailable",
			"OpenWebIF Client Unavailable",
			"CLIENT_UNAVAILABLE",
			"Cannot query receiver information: client not initialized", nil)
		return
	}

	// Query receiver info
	info, err := client.About(ctx)
	if err != nil {
		writeProblem(w, r, http.StatusBadGateway,
			"system/upstream_error",
			"Failed to Query Receiver",
			"UPSTREAM_ERROR",
			err.Error(), nil)
		return
	}
	if info == nil {
		writeProblem(w, r, http.StatusBadGateway,
			"system/upstream_error",
			"Empty Receiver Response",
			"UPSTREAM_EMPTY",
			"The receiver returned an empty response without an error", nil)
		return
	}

	// Query recording locations (bookmarks)
	locations, _ := client.GetLocations(ctx)
	locationItems := make([]StorageItem, 0)
	for _, loc := range locations {
		if loc.Path != "" {
			item := s.checkStorageItem(loc.Path, "Aufnahme-Verzeichnis", "")
			locationItems = append(locationItems, item)
		}
	}

	// Convert to API response
	// Note: We use empty strings instead of "N/A" to allow omitempty to work.
	resp := SystemInfo{
		Hardware: HardwareInfo{
			Brand:              info.Info.Brand,
			Model:              info.Info.Model,
			Boxtype:            info.Info.Boxtype,
			Chipset:            info.Info.Chipset,
			ChipsetDescription: info.Info.FriendlyChipsetText,
		},
		Software: SoftwareInfo{
			OEVersion:     info.Info.OEVer,
			ImageDistro:   orElse(info.Info.FriendlyImageDistro, info.Info.ImageDistro),
			ImageVersion:  info.Info.ImageVer,
			EnigmaVersion: info.Info.EnigmaVer,
			KernelVersion: info.Info.KernelVer,
			DriverDate:    info.Info.DriverDate,
			WebIFVersion:  info.Info.WebIFVer,
		},
		Tuners:  convertTuners(info.Info.Tuners),
		Network: convertNetwork(info.Info.IFaces),
		Storage: s.convertStorage(info.Info.HDD, locationItems),
		Runtime: RuntimeInfo{
			Uptime: info.Info.Uptime,
		},
		Resource: calculateMemory(info.Info.Mem1, info.Info.Mem2),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) getStoragePaths(ctx context.Context) []string {
	unique := make(map[string]struct{})

	// 1. OWI Locations (Truth of receiver paths)
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)
	if c, ok := client.(*openwebif.Client); ok && c != nil {
		if about, err := c.About(ctx); err == nil && about != nil {
			for _, hdd := range about.Info.HDD {
				if hdd.Mount != "" {
					unique[hdd.Mount] = struct{}{}
				}
			}
		}
		if locs, err := c.GetLocations(ctx); err == nil {
			for _, loc := range locs {
				if loc.Path != "" {
					unique[loc.Path] = struct{}{}
				}
			}
		}
	}

	paths := make([]string, 0, len(unique))
	for p := range unique {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// calculateMemory computes memory values from OpenWebIF data
// OpenWebIF provides: Mem1=MemTotal, Mem2=MemAvailable
// We calculate: MemUsed = MemTotal - MemAvailable
func calculateMemory(totalStr, availableStr string) ResourceInfo {
	total := parseMemKB(totalStr)
	available := parseMemKB(availableStr)

	used := total - available
	if used < 0 {
		used = 0
	}

	return ResourceInfo{
		MemoryTotal:     totalStr,
		MemoryAvailable: availableStr,
		MemoryUsed:      formatMemKB(used),
	}
}

// orElse returns fallback if value is empty

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// convertTuners converts AboutTuner to TunerInfo
func convertTuners(tuners []openwebif.AboutTuner) []TunerInfo {
	result := make([]TunerInfo, len(tuners))
	for i, tuner := range tuners {
		// Determine status based on which field is populated
		status := "idle"
		if tuner.Rec != "" {
			status = "recording"
		} else if tuner.Live != "" {
			status = "live"
		} else if tuner.Stream != "" {
			status = "streaming"
		}

		result[i] = TunerInfo{
			Name:   tuner.Name,
			Type:   tuner.Type,
			Status: status,
		}
	}
	return result
}

// convertNetwork converts NetworkInterface to NetworkInfo
func convertNetwork(ifaces []openwebif.NetworkInterface) NetworkInfo {
	interfaces := make([]NetworkInterfaceInfo, len(ifaces))
	for i, iface := range ifaces {
		interfaces[i] = NetworkInterfaceInfo{
			Name:  iface.Name,
			Type:  iface.FriendlyNIC,
			Speed: iface.LinkSpeed,
			MAC:   iface.MAC,
			IP:    iface.IP,
			IPv6:  iface.IPv6,
			DHCP:  iface.DHCP,
		}
	}
	return NetworkInfo{Interfaces: interfaces}
}

// convertStorage converts HDDInfo to StorageInfo
func (s *Server) convertStorage(devices []openwebif.HDDInfo, locations []StorageItem) StorageInfo {
	var devsPtr *[]StorageItem
	if len(devices) > 0 {
		devs := make([]StorageItem, len(devices))
		for i, dev := range devices {
			devs[i] = s.checkStorageItem(dev.Mount, dev.Model, dev.FriendlyCapacity)
		}
		devsPtr = &devs
	}

	var locsPtr *[]StorageItem
	if len(locations) > 0 {
		locsPtr = &locations
	}

	return StorageInfo{
		Devices:   devsPtr,
		Locations: locsPtr,
	}
}

// checkStorageItem performs accessibility checks and NAS detection
func (s *Server) checkStorageItem(mount, model, capacity string) StorageItem {
	item := StorageItem{}
	if mount != "" {
		item.Mount = &mount
	}
	if model != "" {
		item.Model = &model
	}
	if capacity != "" {
		item.Capacity = &capacity
	}

	var health StorageHealth
	if s.storageMonitor != nil {
		health = s.storageMonitor.GetHealth(mount)
	} else {
		health = StorageHealth{
			MountStatus:  StorageItemMountStatusUnknown,
			HealthStatus: StorageItemHealthStatusUnknown,
			Access:       StorageItemAccessNone,
		}
	}

	item.MountStatus = health.MountStatus
	item.HealthStatus = health.HealthStatus
	item.Access = health.Access

	if health.FsType != "" {
		item.FsType = &health.FsType
	}

	if !health.CheckedAt.IsZero() {
		item.CheckedAt = &health.CheckedAt
	}

	// NAS Detection (Heuristics + Mount Info)
	lowMount := strings.ToLower(mount)
	lowModel := strings.ToLower(model)

	if health.FsType != "" {
		item.IsNas = isNasFs(health.FsType)
	} else if strings.Contains(lowMount, "nfs") ||
		strings.Contains(lowMount, "smb") ||
		strings.Contains(lowMount, "cifs") ||
		strings.Contains(lowMount, "net") ||
		strings.Contains(lowMount, "mergerfs") ||
		strings.Contains(lowModel, "nas") ||
		strings.Contains(lowModel, "net") {
		item.IsNas = true
	} else {
		item.IsNas = false
	}

	return item
}

// parseMemKB extracts kB value from strings like "757824 kB"
func parseMemKB(s string) int64 {
	// Remove " kB" suffix and parse
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " kB")
	s = strings.TrimSuffix(s, "kB")
	s = strings.TrimSpace(s)

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// formatMemKB formats kB value to string
func formatMemKB(kB int64) string {
	return fmt.Sprintf("%d kB", kB)
}
