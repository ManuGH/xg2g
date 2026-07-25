package ffmpeg

import (
	"os"
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

func TestMain(m *testing.M) {
	// Most legacy tests assume AMD VCN for VAAPI specific quirks (QVBR, geometry pad).
	// We globally mock the hardware detection to AMD for tests unless overridden.
	hardware.SetGPUVendor(hardware.VendorAMD)
	os.Exit(m.Run())
}
