// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// BindListenAddr replaces the host part of a listen address when it is of the
// form ":PORT" or empty. Explicit host:port values are left untouched.
// Supports "if:<name>" to bind to the first non-loopback IPv4 of an interface.
func BindListenAddr(listenAddr, bind string) (string, error) {
	if bind == "" {
		return listenAddr, nil
	}

	if listenAddr == "" || listenAddr[0] == ':' {
		port := listenAddr
		if port == "" {
			port = ":0"
		}

		host := bind
		if len(bind) > 3 && bind[:3] == "if:" {
			ifName := bind[3:]
			iface, err := net.InterfaceByName(ifName)
			if err != nil {
				return "", fmt.Errorf("resolve interface %q: %w", ifName, err)
			}
			addrs, err := iface.Addrs()
			if err != nil {
				return "", fmt.Errorf("list addrs for %q: %w", ifName, err)
			}
			found := false
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() || ip.To4() == nil {
					continue
				}
				host = ip.String()
				found = true
				break
			}
			if !found {
				return "", fmt.Errorf("no suitable IPv4 on interface %q", ifName)
			}
		}

		return net.JoinHostPort(host, port[1:]), nil
	}

	return listenAddr, nil
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	// ListenAddr is the address to listen on (e.g., ":8080")
	ListenAddr string

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration

	// MaxHeaderBytes controls the maximum number of bytes the server will read parsing the request header's keys and values
	MaxHeaderBytes int

	// ShutdownTimeout is the maximum duration to wait for graceful shutdown
	ShutdownTimeout time.Duration
}

const (
	// Default server timeouts
	defaultReadTimeout     = 60 * time.Second
	defaultWriteTimeout    = 0 // 0 = no timeout (crucial for streaming)
	defaultIdleTimeout     = 120 * time.Second
	defaultMaxHeaderBytes  = 1 << 20 // 1 MB
	defaultShutdownTimeout = 15 * time.Second
	fallbackListenAddr     = ":8088"
)

// ParseServerConfig reads server configuration from environment variables.
// It returns a ServerConfig with sensible defaults that can be overridden via ENV.
func ParseServerConfig() ServerConfig {
	return ParseServerConfigForApp(AppConfig{})
}

// ParseServerConfigForApp resolves server config with explicit precedence:
// ENV > AppConfig (YAML + merged defaults) > Registry default.
func ParseServerConfigForApp(cfg AppConfig) ServerConfig {
	base := defaultServerRuntimeConfig()
	if cfg.Server.ReadTimeout > 0 {
		base.ReadTimeout = cfg.Server.ReadTimeout
	}
	if cfg.Server.WriteTimeout >= 0 {
		base.WriteTimeout = cfg.Server.WriteTimeout
	}
	if cfg.Server.IdleTimeout > 0 {
		base.IdleTimeout = cfg.Server.IdleTimeout
	}
	if cfg.Server.MaxHeaderBytes > 0 {
		base.MaxHeaderBytes = cfg.Server.MaxHeaderBytes
	}
	if cfg.Server.ShutdownTimeout > 0 {
		base.ShutdownTimeout = cfg.Server.ShutdownTimeout
	}

	listen := strings.TrimSpace(ParseString("XG2G_LISTEN", ""))
	if listen == "" {
		if strings.TrimSpace(cfg.APIListenAddr) != "" {
			listen = cfg.APIListenAddr
		} else {
			listen = defaultAPIListenAddr()
		}
	}

	maxHeaderBytes := ParseInt("XG2G_SERVER_MAX_HEADER_BYTES", base.MaxHeaderBytes)
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = base.MaxHeaderBytes
	}

	shutdownTimeout := ParseDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", base.ShutdownTimeout)
	if shutdownTimeout < 3*time.Second {
		shutdownTimeout = 3 * time.Second
	}

	return ServerConfig{
		ListenAddr:      listen,
		ReadTimeout:     ParseDuration("XG2G_SERVER_READ_TIMEOUT", base.ReadTimeout),
		WriteTimeout:    ParseDuration("XG2G_SERVER_WRITE_TIMEOUT", base.WriteTimeout),
		IdleTimeout:     ParseDuration("XG2G_SERVER_IDLE_TIMEOUT", base.IdleTimeout),
		MaxHeaderBytes:  maxHeaderBytes,
		ShutdownTimeout: shutdownTimeout,
	}
}

// ParseMetricsAddr reads metrics server address from environment variables.
// Returns empty string if metrics should be disabled.
func ParseMetricsAddr() string {
	return ParseString("XG2G_METRICS_LISTEN", "")
}

func defaultAPIListenAddr() string {
	reg, err := GetRegistry()
	if err == nil {
		if entry, ok := reg.ByPath["api.listenAddr"]; ok {
			if v, ok := entry.Default.(string); ok && strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return fallbackListenAddr
}

func defaultServerRuntimeConfig() ServerRuntimeConfig {
	out := ServerRuntimeConfig{
		ReadTimeout:     defaultReadTimeout,
		WriteTimeout:    defaultWriteTimeout,
		IdleTimeout:     defaultIdleTimeout,
		MaxHeaderBytes:  defaultMaxHeaderBytes,
		ShutdownTimeout: defaultShutdownTimeout,
	}

	reg, err := GetRegistry()
	if err != nil {
		return out
	}
	if entry, ok := reg.ByPath["server.readTimeout"]; ok {
		if v, ok := entry.Default.(time.Duration); ok && v > 0 {
			out.ReadTimeout = v
		}
	}
	if entry, ok := reg.ByPath["server.writeTimeout"]; ok {
		if v, ok := entry.Default.(time.Duration); ok && v >= 0 {
			out.WriteTimeout = v
		}
	}
	if entry, ok := reg.ByPath["server.idleTimeout"]; ok {
		if v, ok := entry.Default.(time.Duration); ok && v > 0 {
			out.IdleTimeout = v
		}
	}
	if entry, ok := reg.ByPath["server.maxHeaderBytes"]; ok {
		if v, ok := entry.Default.(int); ok && v > 0 {
			out.MaxHeaderBytes = v
		}
	}
	if entry, ok := reg.ByPath["server.shutdownTimeout"]; ok {
		if v, ok := entry.Default.(time.Duration); ok && v > 0 {
			out.ShutdownTimeout = v
		}
	}
	return out
}
