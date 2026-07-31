package hardware

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRenderNode builds a minimal sysfs fixture: /<root>/renderD<n>/device/{vendor,device}
// plus a driver symlink, mirroring the real /sys/class/drm layout.
func writeRenderNode(t *testing.T, root, node, vendorID, deviceID, driver string) {
	t.Helper()
	devDir := filepath.Join(root, node, "device")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if vendorID != "" {
		if err := os.WriteFile(filepath.Join(devDir, "vendor"), []byte(vendorID+"\n"), 0o600); err != nil {
			t.Fatalf("write vendor: %v", err)
		}
	}
	if deviceID != "" {
		if err := os.WriteFile(filepath.Join(devDir, "device"), []byte(deviceID+"\n"), 0o600); err != nil {
			t.Fatalf("write device: %v", err)
		}
	}
	if driver != "" {
		driverDir := filepath.Join(root, "drivers", driver)
		if err := os.MkdirAll(driverDir, 0o755); err != nil {
			t.Fatalf("mkdir driver: %v", err)
		}
		if err := os.Symlink(driverDir, filepath.Join(devDir, "driver")); err != nil {
			t.Fatalf("symlink driver: %v", err)
		}
	}
}

func withDRMRoot(t *testing.T, root string) {
	t.Helper()
	previous := sysfsDRMRoot
	sysfsDRMRoot = root
	SetGPUVendor(GPUVendorInfo{})
	t.Cleanup(func() {
		sysfsDRMRoot = previous
		SetGPUVendor(GPUVendorInfo{})
	})
}

func TestDetectGPUVendor(t *testing.T) {
	tests := []struct {
		name       string
		vendorID   string
		deviceID   string
		driver     string
		wantVendor GPUVendor
	}{
		// The Arrow Lake-S node this project actually runs on since the 2026-07 move.
		{name: "intel arrow lake", vendorID: "0x8086", deviceID: "0x7d67", driver: "i915", wantVendor: GPUVendorIntel},
		{name: "intel xe driver", vendorID: "0x8086", deviceID: "0xe20b", driver: "xe", wantVendor: GPUVendorIntel},
		{name: "amd phoenix", vendorID: "0x1002", deviceID: "0x15bf", driver: "amdgpu", wantVendor: GPUVendorAMD},
		{name: "nvidia", vendorID: "0x10de", deviceID: "0x2504", driver: "nvidia-drm", wantVendor: GPUVendorNVIDIA},
		// No PCI vendor id exposed (virtualised topology): fall back to the driver.
		{name: "driver fallback", vendorID: "", deviceID: "", driver: "amdgpu", wantVendor: GPUVendorAMD},
		{name: "unreadable", vendorID: "", deviceID: "", driver: "", wantVendor: GPUVendorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			withDRMRoot(t, root)
			writeRenderNode(t, root, "renderD128", tt.vendorID, tt.deviceID, tt.driver)

			got := DetectGPUVendor()
			if got.Vendor != tt.wantVendor {
				t.Fatalf("vendor = %q, want %q (info %+v)", got.Vendor, tt.wantVendor, got)
			}
			if tt.deviceID != "" && got.DeviceID != tt.deviceID {
				t.Fatalf("deviceID = %q, want %q", got.DeviceID, tt.deviceID)
			}
			if tt.driver != "" && got.Driver != tt.driver {
				t.Fatalf("driver = %q, want %q", got.Driver, tt.driver)
			}
		})
	}
}

func TestDetectGPUVendorNoRenderNode(t *testing.T) {
	withDRMRoot(t, t.TempDir())
	if got := DetectGPUVendor(); got.Vendor != GPUVendorUnknown {
		t.Fatalf("vendor = %q, want unknown", got.Vendor)
	}
}

func TestIsGPUVendorNeverMatchesOnUnknown(t *testing.T) {
	withDRMRoot(t, t.TempDir())
	for _, vendor := range []GPUVendor{GPUVendorAMD, GPUVendorIntel, GPUVendorNVIDIA} {
		if IsGPUVendor(vendor) {
			t.Fatalf("unknown hardware must not match %q", vendor)
		}
	}
}

func TestDetectGPUVendorCachesResult(t *testing.T) {
	root := t.TempDir()
	withDRMRoot(t, root)
	writeRenderNode(t, root, "renderD128", "0x8086", "0x7d67", "i915")

	first := DetectGPUVendor()
	if err := os.RemoveAll(filepath.Join(root, "renderD128")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if second := DetectGPUVendor(); second != first {
		t.Fatalf("detection is not cached: %+v then %+v", first, second)
	}
}
