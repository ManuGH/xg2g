// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"net"
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
)

// ParseServerConfig reads server configuration from environment variables.
// It returns a ServerConfig with sensible defaults that can be overridden via ENV.
func ParseServerConfig() ServerConfig {
	listen := ParseString("XG2G_LISTEN", ":8080")

	shutdownTimeout := ParseDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if shutdownTimeout < 3*time.Second {
		shutdownTimeout = 3 * time.Second
	}

	return ServerConfig{
		ListenAddr:      listen,
		ReadTimeout:     ParseDuration("XG2G_SERVER_READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:    ParseDuration("XG2G_SERVER_WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:     ParseDuration("XG2G_SERVER_IDLE_TIMEOUT", defaultIdleTimeout),
		MaxHeaderBytes:  ParseInt("XG2G_SERVER_MAX_HEADER_BYTES", defaultMaxHeaderBytes),
		ShutdownTimeout: shutdownTimeout,
	}
}

// ParseMetricsAddr reads metrics server address from environment variables.
// Returns empty string if metrics should be disabled.
func ParseMetricsAddr() string {
	return ParseString("XG2G_METRICS_LISTEN", "")
}
