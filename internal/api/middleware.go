package api

import (
	"net"
	"strings"
	"sync"
)

var (
	trustedIPNets     []*net.IPNet
	trustedIPNetsOnce sync.Once
)

// SetTrustedProxies configures the list of trusted proxies/CIDRs.
// This must be called at startup with configuration values.
func SetTrustedProxies(csv string) {
	trustedIPNetsOnce.Do(func() {
		if csv == "" {
			return
		}
		for _, part := range strings.Split(csv, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if _, ipnet, err := net.ParseCIDR(p); err == nil {
				trustedIPNets = append(trustedIPNets, ipnet)
			}
		}
	})
}
