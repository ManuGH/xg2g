package hardware

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// GPUVendor identifies the silicon behind the VAAPI render node. Encoder tuning
// is vendor-specific - rate-control modes, level signalling and bitrate
// headroom differ between AMD, Intel and NVIDIA - so the pipeline must never
// assume one of them. Everything that guesses is a bug waiting for the next
// hardware swap.
type GPUVendor string

const (
	GPUVendorAMD     GPUVendor = "amd"
	GPUVendorIntel   GPUVendor = "intel"
	GPUVendorNVIDIA  GPUVendor = "nvidia"
	GPUVendorUnknown GPUVendor = "unknown"
)

// PCI vendor IDs as exposed by /sys/class/drm/<node>/device/vendor.
const (
	pciVendorIntel  = "0x8086"
	pciVendorAMD    = "0x1002"
	pciVendorATI    = "0x1022" // AMD host bridge id, seen on some APU topologies
	pciVendorNVIDIA = "0x10de"
)

var (
	gpuVendorMu       sync.RWMutex
	gpuVendorChecked  bool
	gpuVendorDetected GPUVendor = GPUVendorUnknown
	gpuVendorDevice   string
	gpuVendorDriver   string

	// sysfsDRMRoot is a variable so tests can point detection at a fixture tree.
	sysfsDRMRoot = "/sys/class/drm"
)

// GPUVendorInfo is the immutable detection result, safe to log and to embed in
// host snapshots.
type GPUVendorInfo struct {
	Vendor GPUVendor
	// DeviceID is the PCI device id (e.g. "0x7d67"), empty when unknown.
	DeviceID string
	// Driver is the kernel driver bound to the node (e.g. "i915", "xe",
	// "amdgpu", "nvidia"), empty when it cannot be resolved.
	Driver string
}

// DetectGPUVendor resolves the vendor of the primary VAAPI render node. The
// result is cached: the render node cannot change without a restart, and every
// caller in the encode path must see one consistent answer.
func DetectGPUVendor() GPUVendorInfo {
	gpuVendorMu.RLock()
	if gpuVendorChecked {
		info := GPUVendorInfo{Vendor: gpuVendorDetected, DeviceID: gpuVendorDevice, Driver: gpuVendorDriver}
		gpuVendorMu.RUnlock()
		return info
	}
	gpuVendorMu.RUnlock()

	info := probeGPUVendor()

	gpuVendorMu.Lock()
	gpuVendorChecked = true
	gpuVendorDetected = info.Vendor
	gpuVendorDevice = info.DeviceID
	gpuVendorDriver = info.Driver
	gpuVendorMu.Unlock()

	return info
}

// SetGPUVendor overrides detection. Used by tests and by an operator escape
// hatch for exotic setups where sysfs is not readable (e.g. a restricted
// container). An empty vendor resets to "not yet detected".
func SetGPUVendor(info GPUVendorInfo) {
	gpuVendorMu.Lock()
	defer gpuVendorMu.Unlock()
	if info.Vendor == "" {
		gpuVendorChecked = false
		gpuVendorDetected = GPUVendorUnknown
		gpuVendorDevice = ""
		gpuVendorDriver = ""
		return
	}
	gpuVendorChecked = true
	gpuVendorDetected = info.Vendor
	gpuVendorDevice = info.DeviceID
	gpuVendorDriver = info.Driver
}

// IsGPUVendor reports whether the detected vendor matches. Unknown never
// matches: a vendor-specific tweak must stay off when we cannot prove the
// hardware, because the failure mode of a wrong guess is an encoder that
// refuses to open.
func IsGPUVendor(want GPUVendor) bool {
	return DetectGPUVendor().Vendor == want
}

func probeGPUVendor() GPUVendorInfo {
	// NVENC lives on a different device tree entirely; when the NVIDIA runtime
	// exposed its control nodes the answer is unambiguous.
	if HasNVENC() {
		return GPUVendorInfo{Vendor: GPUVendorNVIDIA, Driver: "nvidia"}
	}

	nodes, err := filepath.Glob(filepath.Join(sysfsDRMRoot, "renderD*"))
	if err != nil || len(nodes) == 0 {
		return GPUVendorInfo{Vendor: GPUVendorUnknown}
	}

	for _, node := range nodes {
		info := vendorForRenderNode(node)
		if info.Vendor != GPUVendorUnknown {
			return info
		}
	}
	return GPUVendorInfo{Vendor: GPUVendorUnknown}
}

func vendorForRenderNode(node string) GPUVendorInfo {
	info := GPUVendorInfo{
		Vendor:   GPUVendorUnknown,
		DeviceID: readSysfsToken(filepath.Join(node, "device", "device")),
		Driver:   driverNameForRenderNode(node),
	}

	switch readSysfsToken(filepath.Join(node, "device", "vendor")) {
	case pciVendorIntel:
		info.Vendor = GPUVendorIntel
	case pciVendorAMD, pciVendorATI:
		info.Vendor = GPUVendorAMD
	case pciVendorNVIDIA:
		info.Vendor = GPUVendorNVIDIA
	}
	if info.Vendor != GPUVendorUnknown {
		return info
	}

	// Fall back to the bound kernel driver: virtualised or non-PCI display
	// topologies do not always expose a PCI vendor id.
	switch info.Driver {
	case "i915", "xe":
		info.Vendor = GPUVendorIntel
	case "amdgpu", "radeon":
		info.Vendor = GPUVendorAMD
	case "nvidia", "nvidia-drm":
		info.Vendor = GPUVendorNVIDIA
	}
	return info
}

func driverNameForRenderNode(node string) string {
	resolved, err := filepath.EvalSymlinks(filepath.Join(node, "device", "driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(resolved)
}

func readSysfsToken(path string) string {
	raw, err := os.ReadFile(path) // #nosec G304 -- sysfs path built from a fixed root
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(raw)))
}
