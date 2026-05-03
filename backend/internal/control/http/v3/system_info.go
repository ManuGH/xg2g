package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/problemcode"
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
	ChipsetDescription string `json:"chipsetDescription,omitempty"`
}

// SoftwareInfo represents software versions
type SoftwareInfo struct {
	OEVersion     string `json:"oeVersion,omitempty"`
	ImageDistro   string `json:"imageDistro,omitempty"`
	ImageVersion  string `json:"imageVersion,omitempty"`
	EnigmaVersion string `json:"enigmaVersion,omitempty"`
	KernelVersion string `json:"kernelVersion,omitempty"`
	DriverDate    string `json:"driverDate,omitempty"`
	WebIFVersion  string `json:"webifVersion,omitempty"`
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

type storageOriginHint string

const (
	storageOriginReceiver storageOriginHint = "receiver"
	storageOriginXG2G     storageOriginHint = "xg2g"
)

type storageDescriptor struct {
	Path     string
	Model    string
	Capacity string
	Origin   storageOriginHint
	TypeHint string
}

// RuntimeInfo represents runtime information
type RuntimeInfo struct {
	Uptime string `json:"uptime"`
}

// ResourceInfo represents CPU and memory usage
type ResourceInfo struct {
	MemoryTotal     string `json:"memoryTotal"`
	MemoryAvailable string `json:"memoryAvailable"`
	MemoryUsed      string `json:"memoryUsed"`
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
				writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/panic", "Internal Server Error", problemcode.CodePanic, "A serious error occurred while processing system information", nil)
			}
		}
	}()
	ctx := r.Context()

	// Get OpenWebIF client using standard factory method
	owiClient := s.owi(s.cfg, s.snap)
	// Type assert to concrete client (owi returns interface)
	client, ok := owiClient.(*openwebif.Client)
	if !ok || client == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable,
			"system/client_unavailable",
			"OpenWebIF Client Unavailable",
			problemcode.CodeClientUnavailable,
			"Cannot query receiver information: client not initialized", nil)
		return
	}

	// Use a short shared deadline and fan out the upstream calls in parallel so
	// one slow endpoint does not starve the others inside the same 2s budget.
	upstreamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var (
		info         *openwebif.AboutInfo
		infoErr      error
		statusInfo   *openwebif.StatusInfo
		statusErr    error
		locations    []openwebif.MovieLocation
		locationsErr error
		wg           sync.WaitGroup
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		info, infoErr = client.About(upstreamCtx)
	}()

	go func() {
		defer wg.Done()
		statusInfo, statusErr = client.GetStatusInfo(upstreamCtx)
	}()

	go func() {
		defer wg.Done()
		locations, locationsErr = client.GetLocations(upstreamCtx)
	}()

	wg.Wait()

	if infoErr != nil {
		writeRegisteredProblem(w, r, http.StatusBadGateway,
			"system/upstream_error",
			"Failed to Query Receiver",
			problemcode.CodeUpstreamError,
			infoErr.Error(), nil)
		return
	}
	if info == nil {
		writeRegisteredProblem(w, r, http.StatusBadGateway,
			"system/upstream_error",
			"Empty Receiver Response",
			problemcode.CodeUpstreamEmpty,
			"The receiver returned an empty response without an error", nil)
		return
	}

	// Cross-check streaming state with the lighter status endpoint.
	// Some receivers briefly leave stale tuner.stream values in /api/about
	// after a client disconnects, while /api/statusinfo already reports
	// isStreaming=false and /api/about info.streams is empty.
	if statusErr != nil {
		log.L().Debug().Err(statusErr).Msg("system_info: failed to fetch statusinfo; falling back to tuner-only stream state")
		statusInfo = nil
	}

	// Query recording locations (bookmarks)
	if locationsErr != nil {
		log.L().Debug().Err(locationsErr).Msg("system_info: failed to fetch locations; continuing without recording locations")
		locations = nil
	}

	deviceItems := make([]StorageItem, 0, len(info.Info.HDD))
	for _, dev := range info.Info.HDD {
		deviceItems = append(deviceItems, s.checkStorageItem(storageDescriptor{
			Path:     dev.Mount,
			Model:    dev.Model,
			Capacity: dev.FriendlyCapacity,
			Origin:   storageOriginReceiver,
		}))
	}
	locationItems := s.collectStorageLocationItems(locations)

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
		Tuners:  convertTuners(info.Info.Tuners, info.Info.Streams, statusInfo),
		Network: convertNetwork(info.Info.IFaces),
		Storage: s.convertStorage(deviceItems, locationItems),
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
		about, err := c.About(ctx)
		if err != nil {
			log.L().Error().Err(err).Msg("storage_monitor: failed to get About info")
		} else if about != nil {
			log.L().Debug().Int("hdd_count", len(about.Info.HDD)).Msg("storage_monitor: found HDDs in About")
			for _, hdd := range about.Info.HDD {
				if hdd.Mount != "" {
					unique[hdd.Mount] = struct{}{}
				}
			}
		}

		locs, err := c.GetLocations(ctx)
		if err != nil {
			log.L().Error().Err(err).Msg("storage_monitor: failed to get Locations")
		} else {
			log.L().Debug().Int("location_count", len(locs)).Msg("storage_monitor: found locations")
			for _, loc := range locs {
				if loc.Path != "" {
					unique[loc.Path] = struct{}{}
				}
			}
		}
	} else {
		log.L().Warn().Msg("storage_monitor: OpenWebIF client not available or wrong type")
	}

	for _, desc := range collectConfiguredStorageDescriptors(&cfg) {
		if desc.Path != "" {
			unique[desc.Path] = struct{}{}
		}
	}

	log.L().Debug().Int("unique_paths", len(unique)).Msg("storage_monitor: getStoragePaths result")

	paths := make([]string, 0, len(unique))
	for p := range unique {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func collectConfiguredStorageDescriptors(cfg *config.AppConfig) []storageDescriptor {
	if cfg == nil {
		return nil
	}

	merged := make(map[string]storageDescriptor)
	add := func(desc storageDescriptor) {
		cleanPath := strings.TrimSpace(desc.Path)
		if cleanPath == "" {
			return
		}
		cleanPath = filepath.Clean(cleanPath)
		key := string(desc.Origin) + "\x00" + cleanPath
		desc.Path = cleanPath
		if existing, ok := merged[key]; ok {
			if existing.TypeHint == "" && desc.TypeHint != "" {
				existing.TypeHint = desc.TypeHint
			}
			if existing.Model == "" && desc.Model != "" {
				existing.Model = desc.Model
			}
			if existing.Capacity == "" && desc.Capacity != "" {
				existing.Capacity = desc.Capacity
			}
			merged[key] = existing
			return
		}
		merged[key] = desc
	}

	for id, path := range cfg.RecordingRoots {
		add(storageDescriptor{
			Path:   path,
			Model:  strings.TrimSpace(id),
			Origin: storageOriginXG2G,
		})
	}
	for _, mapping := range cfg.RecordingPathMappings {
		add(storageDescriptor{
			Path:   mapping.LocalRoot,
			Origin: storageOriginXG2G,
		})
	}
	for _, root := range cfg.Library.Roots {
		add(storageDescriptor{
			Path:     root.Path,
			Model:    strings.TrimSpace(root.ID),
			Origin:   storageOriginXG2G,
			TypeHint: strings.TrimSpace(root.Type),
		})
	}

	out := make([]storageDescriptor, 0, len(merged))
	for _, desc := range merged {
		out = append(out, desc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Origin != out[j].Origin {
			return out[i].Origin < out[j].Origin
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func (s *Server) collectStorageLocationItems(receiverLocations []openwebif.MovieLocation) []StorageItem {
	descriptors := make([]storageDescriptor, 0, len(receiverLocations))
	for _, loc := range receiverLocations {
		if strings.TrimSpace(loc.Path) == "" {
			continue
		}
		descriptors = append(descriptors, storageDescriptor{
			Path:   loc.Path,
			Origin: storageOriginReceiver,
		})
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	descriptors = append(descriptors, collectConfiguredStorageDescriptors(&cfg)...)

	items := make([]StorageItem, 0, len(descriptors))
	for _, desc := range descriptors {
		items = append(items, s.checkStorageItem(desc))
	}
	return items
}

func deriveStoragePathType(origin storageOriginHint, mount, model, fsType, typeHint string) string {
	switch origin {
	case storageOriginReceiver:
		if isNetworkFs(fsType) || hasNetworkStorageHint(mount, model, typeHint) {
			return "receiver_share"
		}
		return "receiver_attached"
	case storageOriginXG2G:
		if isAggregateFs(fsType) || hasAggregateStorageHint(mount, model, typeHint) {
			return "xg2g_aggregate"
		}
		if isNetworkFs(fsType) || hasNetworkStorageHint(mount, model, typeHint) {
			return "xg2g_share"
		}
		if strings.TrimSpace(mount) != "" || strings.TrimSpace(model) != "" {
			return "xg2g_local"
		}
	}
	return "unknown"
}

func hasNetworkStorageHint(values ...string) bool {
	for _, value := range values {
		low := strings.ToLower(strings.TrimSpace(value))
		if low == "" {
			continue
		}
		if low == "nfs" || low == "smb" || low == "cifs" {
			return true
		}
		if strings.Contains(low, "/media/net") ||
			strings.Contains(low, "nfs") ||
			strings.Contains(low, "smb") ||
			strings.Contains(low, "cifs") ||
			strings.Contains(low, "network") ||
			strings.Contains(low, "remote") {
			return true
		}
	}
	return false
}

func hasAggregateStorageHint(values ...string) bool {
	for _, value := range values {
		low := strings.ToLower(strings.TrimSpace(value))
		if low == "" {
			continue
		}
		if strings.Contains(low, "mergerfs") ||
			strings.Contains(low, "mergefs") ||
			strings.Contains(low, "unionfs") ||
			strings.Contains(low, "overlay") {
			return true
		}
	}
	return false
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
func convertTuners(tuners []openwebif.AboutTuner, aboutStreams any, status *openwebif.StatusInfo) []TunerInfo {
	aboutKnown, aboutActive := aboutStreamsState(aboutStreams)
	statusKnown, statusActive := statusStreamingState(status)
	streamingSignalKnown := aboutKnown || statusKnown
	streamingActive := aboutActive || statusActive

	result := make([]TunerInfo, len(tuners))
	for i, tuner := range tuners {
		// Determine status based on which field is populated
		status := "idle"
		if tuner.Rec != "" {
			status = "recording"
		} else if tuner.Live != "" {
			status = "live"
		} else if tuner.Stream != "" {
			// Suppress stale per-tuner stream flags when the receiver globally
			// already reports that no stream is active.
			if !streamingSignalKnown || streamingActive {
				status = "streaming"
			}
		}

		result[i] = TunerInfo{
			Name:   tuner.Name,
			Type:   tuner.Type,
			Status: status,
		}
	}
	return result
}

func statusStreamingState(info *openwebif.StatusInfo) (known bool, active bool) {
	if info == nil {
		return false, false
	}

	return parseOWIBoolString(info.IsStreaming)
}

func aboutStreamsState(v any) (known bool, active bool) {
	switch streams := v.(type) {
	case nil:
		return false, false
	case []any:
		return true, len(streams) > 0
	case map[string]any:
		return true, len(streams) > 0
	case string:
		return true, strings.TrimSpace(streams) != ""
	case bool:
		return true, streams
	default:
		// Preserve legacy behavior for unexpected payloads by treating them as
		// an active/known signal instead of suppressing streaming.
		return true, true
	}
}

func parseOWIBoolString(v string) (known bool, value bool) {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return true, false
	default:
		return false, false
	}
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
func (s *Server) convertStorage(devices []StorageItem, locations []StorageItem) StorageInfo {
	var devsPtr *[]StorageItem
	if len(devices) > 0 {
		devs := append([]StorageItem(nil), devices...)
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
func (s *Server) checkStorageItem(desc storageDescriptor) StorageItem {
	item := StorageItem{}
	mount := strings.TrimSpace(desc.Path)
	model := strings.TrimSpace(desc.Model)
	capacity := strings.TrimSpace(desc.Capacity)
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

	origin := string(desc.Origin)
	if strings.TrimSpace(origin) != "" {
		item.Origin = &origin
	}

	pathType := deriveStoragePathType(desc.Origin, mount, model, health.FsType, desc.TypeHint)
	if pathType != "" {
		item.PathType = &pathType
	}

	item.IsNas = pathType == "receiver_share" || pathType == "xg2g_share"

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
