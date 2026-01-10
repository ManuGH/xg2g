package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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
	Brand              string `json:"brand"`
	Model              string `json:"model"`
	Boxtype            string `json:"boxtype"`
	Chipset            string `json:"chipset"`
	ChipsetDescription string `json:"chipset_description"`
}

// SoftwareInfo represents software versions
type SoftwareInfo struct {
	OEVersion     string `json:"oe_version"`
	ImageDistro   string `json:"image_distro"`
	ImageVersion  string `json:"image_version"`
	EnigmaVersion string `json:"enigma_version"`
	KernelVersion string `json:"kernel_version"`
	DriverDate    string `json:"driver_date"`
	WebIFVersion  string `json:"webif_version"`
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
	Devices []StorageDeviceInfo `json:"devices"`
}

// StorageDeviceInfo represents a storage device
type StorageDeviceInfo struct {
	Model    string `json:"model"`
	Capacity string `json:"capacity"`
	Mount    string `json:"mount"`
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

	// Convert to API response
	resp := SystemInfo{
		Hardware: HardwareInfo{
			Brand:              orElse(info.Info.Brand, "N/A"),
			Model:              orElse(info.Info.Model, "Unknown"),
			Boxtype:            orElse(info.Info.Boxtype, "N/A"),
			Chipset:            orElse(info.Info.Chipset, "N/A"),
			ChipsetDescription: orElse(info.Info.FriendlyChipsetText, info.Info.Chipset),
		},
		Software: SoftwareInfo{
			OEVersion:     orElse(info.Info.OEVer, "N/A"),
			ImageDistro:   orElse(info.Info.FriendlyImageDistro, info.Info.ImageDistro),
			ImageVersion:  orElse(info.Info.ImageVer, "N/A"),
			EnigmaVersion: orElse(info.Info.EnigmaVer, "N/A"),
			KernelVersion: orElse(info.Info.KernelVer, "N/A"),
			DriverDate:    orElse(info.Info.DriverDate, "N/A"),
			WebIFVersion:  orElse(info.Info.WebIFVer, "N/A"),
		},
		Tuners:  convertTuners(info.Info.Tuners),
		Network: convertNetwork(info.Info.IFaces),
		Storage: convertStorage(info.Info.HDD),
		Runtime: RuntimeInfo{
			Uptime: orElse(info.Info.Uptime, "N/A"),
		},
		Resource: calculateMemory(info.Info.Mem1, info.Info.Mem2),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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
func convertStorage(devices []openwebif.HDDInfo) StorageInfo {
	devs := make([]StorageDeviceInfo, len(devices))
	for i, dev := range devices {
		devs[i] = StorageDeviceInfo{
			Model:    dev.Model,
			Capacity: dev.FriendlyCapacity,
			Mount:    dev.Mount,
		}
	}
	return StorageInfo{Devices: devs}
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
