//go:build darwin

package recordings

import (
	"strings"

	"golang.org/x/sys/unix"
)

func loadHostOSIdentity() (string, string) {
	version, err := unix.Sysctl("kern.osproductversion")
	if err != nil || strings.TrimSpace(version) == "" {
		return "macos", ""
	}
	return "macos", strings.TrimSpace(version)
}
