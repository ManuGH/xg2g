package recordings

import (
	"runtime"
	"strings"
	"sync"
)

var (
	hostOSIdentityOnce  sync.Once
	cachedHostOSName    string
	cachedHostOSVersion string
)

func resolveHostOSIdentity() (string, string) {
	hostOSIdentityOnce.Do(func() {
		cachedHostOSName, cachedHostOSVersion = loadHostOSIdentity()
		if strings.TrimSpace(cachedHostOSName) == "" {
			cachedHostOSName = runtime.GOOS
		}
		cachedHostOSVersion = strings.TrimSpace(cachedHostOSVersion)
	})
	return cachedHostOSName, cachedHostOSVersion
}
