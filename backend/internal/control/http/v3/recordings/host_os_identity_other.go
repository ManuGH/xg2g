//go:build !linux && !darwin

package recordings

import "runtime"

func loadHostOSIdentity() (string, string) {
	return runtime.GOOS, ""
}
