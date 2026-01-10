package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/http/v3/problem"
)

type LANGuardConfig struct {
	// allowedCIDRs defines which CLIENT IPs can access the protected resource.
	// If empty, defaults to RFC1918 ranges.
	AllowedCIDRs []string

	// trustedProxyCIDRs defines which UPSTREAM IPs (RemoteAddr) are trusted to set X-Forwarded-For.
	// If empty, X-Forwarded-For is ALWAYS IGNORED.
	TrustedProxyCIDRs []string
}

type LANGuard struct {
	allowedClient []*net.IPNet
	trustedProxy  []*net.IPNet
}

func NewLANGuard(cfg LANGuardConfig) (*LANGuard, error) {
	// Parse Allowed Clients (LAN)
	clientCIDRs := cfg.AllowedCIDRs
	if len(clientCIDRs) == 0 {
		clientCIDRs = []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8", // Localhost IPv4
			"::1/128",     // Localhost IPv6
			"fe80::/10",   // Link-Local IPv6
		}
	}
	allowedClients, err := parseCIDRs(clientCIDRs)
	if err != nil {
		return nil, err
	}

	// Parse Trusted Proxies
	trustedProxies, err := parseCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}

	return &LANGuard{allowedClient: allowedClients, trustedProxy: trustedProxies}, nil
}

func parseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, c := range cidrs {
		if strings.TrimSpace(c) == "" {
			continue
		}
		// Try CIDR first
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			nets = append(nets, n)
			continue
		}

		// Try single IP
		ip := net.ParseIP(strings.TrimSpace(c))
		if ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			mask := net.CIDRMask(bits, bits)
			n = &net.IPNet{IP: ip, Mask: mask}
			nets = append(nets, n)
			continue
		}

		return nil, fmt.Errorf("invalid CIDR or IP: %s", c)
	}
	return nets, nil
}

func (g *LANGuard) RequireLAN(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := g.resolveClientIP(r)

		if ip == nil {
			problem.Write(w, r, http.StatusForbidden, "auth/forbidden", "Forbidden", "FORBIDDEN", "Failed to parse remote IP", nil)
			return
		}

		if !g.isIPAllowed(ip, g.allowedClient) {
			problem.Write(w, r, http.StatusForbidden, "authz/forbidden", "Forbidden", "FORBIDDEN", "LAN access required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *LANGuard) isIPAllowed(ip net.IP, subnets []*net.IPNet) bool {
	ip = ip.To16()
	if ip == nil {
		return false
	}
	for _, n := range subnets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resolveClientIP determines the true client IP using right-to-left traversal.
// Policy:
// 1. Start with RemoteAddr.
// 2. If RemoteAddr is NOT in TrustedProxyCIDRs, return RemoteAddr.
// 3. If RemoteAddr IS Trusted, parse X-Forwarded-For.
// 4. Iterate XFF IPs from Right-to-Left.
// 5. Skip IPs that are present in TrustedProxyCIDRs.
// 6. Return the first non-trusted IP found.
// 7. If all XFF IPs are trusted, fallback to RemoteAddr (or the leftmost trusted).
func (g *LANGuard) resolveClientIP(r *http.Request) net.IP {
	remoteIPStr, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		remoteIPStr = strings.TrimSpace(r.RemoteAddr)
	}
	remoteIP := net.ParseIP(remoteIPStr)
	if remoteIP == nil {
		return nil
	}

	// If RemoteAddr is NOT trusted, we must ignore headers
	if !g.isIPAllowed(remoteIP, g.trustedProxy) {
		return remoteIP
	}

	// Remote is trusted; inspect XFF
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		// Try X-Real-IP as a fallback for single-proxy setups
		// But only if XFF is missing, to avoid confusion
		xrip := r.Header.Get("X-Real-IP")
		if xrip != "" {
			ip := net.ParseIP(strings.TrimSpace(xrip))
			if ip != nil {
				return ip
			}
		}
		return remoteIP
	}

	// Parse XFF list
	parts := strings.Split(xff, ",")
	// Traverse Right-to-Left
	for i := len(parts) - 1; i >= 0; i-- {
		ipPart := strings.TrimSpace(parts[i])
		ip := net.ParseIP(ipPart)
		if ip == nil {
			continue
		}

		// If this IP is trusted, it's just another proxy hop -> skip
		if g.isIPAllowed(ip, g.trustedProxy) {
			continue
		}

		// Found the first non-trusted IP -> The Real Client
		return ip
	}

	// If we get here, all IPs in XFF were trusted.
	// Fallback to RemoteAddr (or could be construed as the 'edge' trusted proxy).
	return remoteIP
}
