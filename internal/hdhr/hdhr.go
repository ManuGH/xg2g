// SPDX-License-Identifier: MIT

// Package hdhr implements HDHomeRun protocol compatibility for the xg2g gateway.
package hdhr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Config holds HDHomeRun emulation configuration
type Config struct {
	Enabled      bool
	DeviceID     string
	FriendlyName string
	ModelName    string
	FirmwareName string
	BaseURL      string
	TunerCount   int
	Logger       zerolog.Logger
}

// Server implements HDHomeRun API endpoints
type Server struct {
	config Config
	logger zerolog.Logger
}

// NewServer creates a new HDHomeRun emulation server
func NewServer(config Config) *Server {
	// Generate device ID if not provided
	if config.DeviceID == "" {
		config.DeviceID = "XG2G1234"
	}

	// Set defaults
	if config.FriendlyName == "" {
		config.FriendlyName = "xg2g"
	}
	if config.ModelName == "" {
		config.ModelName = "HDHR-xg2g"
	}
	if config.FirmwareName == "" {
		config.FirmwareName = "xg2g-1.4.0"
	}
	if config.TunerCount == 0 {
		config.TunerCount = 4
	}

	return &Server{
		config: config,
		logger: config.Logger,
	}
}

// DiscoverResponse represents HDHomeRun discovery response
type DiscoverResponse struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// LineupStatus represents tuner status
type LineupStatus struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

// LineupEntry represents a channel in the lineup
type LineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

// HandleDiscover handles /discover.json endpoint
func (s *Server) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		// Try to determine base URL from request
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	response := DiscoverResponse{
		FriendlyName:    s.config.FriendlyName,
		ModelNumber:     s.config.ModelName,
		FirmwareName:    s.config.FirmwareName,
		FirmwareVersion: s.config.FirmwareName,
		DeviceID:        s.config.DeviceID,
		DeviceAuth:      "xg2g",
		BaseURL:         baseURL,
		LineupURL:       fmt.Sprintf("%s/lineup.json", baseURL),
		TunerCount:      s.config.TunerCount,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error().Err(err).Str("endpoint", "/discover.json").Msg("failed to encode HDHomeRun discovery response")
	}

	s.logger.Info().
		Str("endpoint", "/discover.json").
		Str("device_id", s.config.DeviceID).
		Msg("HDHomeRun discovery request")
}

// HandleLineupStatus handles /lineup_status.json endpoint
func (s *Server) HandleLineupStatus(w http.ResponseWriter, _ *http.Request) {
	response := LineupStatus{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Antenna",
		SourceList:     []string{"Antenna"},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error().Err(err).Str("endpoint", "/lineup_status.json").Msg("failed to encode HDHomeRun lineup status response")
	}

	s.logger.Debug().
		Str("endpoint", "/lineup_status.json").
		Msg("HDHomeRun lineup status request")
}

// HandleLineup handles /lineup.json endpoint
// This needs to be implemented to return actual channels
func (s *Server) HandleLineup(w http.ResponseWriter, _ *http.Request) {
	// This will be populated by the main API server with actual channels
	// For now, return empty array
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode([]LineupEntry{}); err != nil {
		s.logger.Error().Err(err).Str("endpoint", "/lineup.json").Msg("failed to encode HDHomeRun lineup response")
	}

	s.logger.Debug().
		Str("endpoint", "/lineup.json").
		Msg("HDHomeRun lineup request")
}

// HandleLineupPost handles POST /lineup.json (Plex scan)
func (s *Server) HandleLineupPost(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("scan")

	if action == "start" {
		s.logger.Info().Msg("HDHomeRun channel scan started (simulated)")
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetConfigFromEnv creates Config from environment variables
func GetConfigFromEnv(logger zerolog.Logger) Config {
	enabled := strings.ToLower(os.Getenv("XG2G_HDHR_ENABLED")) == "true"

	return Config{
		Enabled:      enabled,
		DeviceID:     os.Getenv("XG2G_HDHR_DEVICE_ID"),
		FriendlyName: getEnvDefault("XG2G_HDHR_FRIENDLY_NAME", "xg2g"),
		ModelName:    getEnvDefault("XG2G_HDHR_MODEL", "HDHR-xg2g"),
		FirmwareName: getEnvDefault("XG2G_HDHR_FIRMWARE", "xg2g-1.4.0"),
		BaseURL:      os.Getenv("XG2G_HDHR_BASE_URL"),
		TunerCount:   getEnvInt("XG2G_HDHR_TUNER_COUNT", 4),
		Logger:       logger,
	}
}

func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}

// StartSSDPAnnouncer starts SSDP announcements for automatic discovery
func (s *Server) StartSSDPAnnouncer(ctx context.Context) error {
	// SSDP multicast address
	multicastAddr := "239.255.255.250:1900"

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	// Create UDP connection
	lc := &net.ListenConfig{}
	conn, err := lc.ListenPacket(ctx, "udp4", ":1900")
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port 1900: %w", err)
	}

	// Join multicast group
	if p, ok := conn.(*net.UDPConn); ok {
		if err := p.SetReadBuffer(2048); err != nil {
			s.logger.Warn().Err(err).Msg("failed to set read buffer size")
		}
	}

	s.logger.Info().
		Str("multicast_addr", multicastAddr).
		Str("device_id", s.config.DeviceID).
		Msg("SSDP announcer started")

	// Listen for M-SEARCH requests
	go s.handleSSDPRequests(ctx, conn, addr)

	// Send periodic announcements
	go s.sendPeriodicAnnouncements(ctx, conn, addr)

	// Wait for context cancellation
	<-ctx.Done()
	if err := conn.Close(); err != nil {
		s.logger.Warn().Err(err).Msg("failed to close SSDP connection")
	}
	return nil
}

// handleSSDPRequests listens for SSDP M-SEARCH requests
func (s *Server) handleSSDPRequests(ctx context.Context, conn net.PacketConn, _ *net.UDPAddr) {
	buf := make([]byte, 2048)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				s.logger.Error().Err(err).Msg("failed to set SSDP read deadline")
				continue
			}
			n, remoteAddr, err := conn.ReadFrom(buf)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					continue
				}
				s.logger.Error().Err(err).Msg("failed to read SSDP packet")
				continue
			}

			msg := string(buf[:n])

			// Check if it's an M-SEARCH request for HDHomeRun
			if strings.Contains(msg, "M-SEARCH") &&
				(strings.Contains(msg, "ssdp:all") ||
					strings.Contains(msg, "urn:schemas-upnp-org:device:MediaServer:1")) {
				s.logger.Debug().
					Str("from", remoteAddr.String()).
					Msg("received SSDP M-SEARCH request")

				// Send response
				s.sendSSDPResponse(conn, remoteAddr)
			}
		}
	}
}

// sendSSDPResponse sends SSDP response to M-SEARCH
func (s *Server) sendSSDPResponse(conn net.PacketConn, addr net.Addr) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		// Get local IP
		if localIP := s.getLocalIP(); localIP != "" {
			baseURL = "http://" + net.JoinHostPort(localIP, "8080")
		} else {
			baseURL = "http://localhost:8080"
		}
	}

	response := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"CACHE-CONTROL: max-age=1800\r\n"+
			"EXT:\r\n"+
			"LOCATION: %s/device.xml\r\n"+
			"SERVER: Linux/2.6 UPnP/1.0 xg2g/1.4.0\r\n"+
			"ST: urn:schemas-upnp-org:device:MediaServer:1\r\n"+
			"USN: uuid:%s::urn:schemas-upnp-org:device:MediaServer:1\r\n"+
			"\r\n",
		baseURL,
		s.config.DeviceID,
	)

	if _, err := conn.WriteTo([]byte(response), addr); err != nil {
		s.logger.Error().Err(err).Msg("failed to send SSDP response")
	} else {
		s.logger.Debug().
			Str("to", addr.String()).
			Msg("sent SSDP response")
	}
}

// sendPeriodicAnnouncements sends NOTIFY announcements periodically
func (s *Server) sendPeriodicAnnouncements(ctx context.Context, conn net.PacketConn, addr *net.UDPAddr) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Send initial announcement
	s.sendSSDPNotify(conn, addr)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendSSDPNotify(conn, addr)
		}
	}
}

// sendSSDPNotify sends SSDP NOTIFY announcement
func (s *Server) sendSSDPNotify(conn net.PacketConn, addr *net.UDPAddr) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		if localIP := s.getLocalIP(); localIP != "" {
			baseURL = "http://" + net.JoinHostPort(localIP, "8080")
		} else {
			return // Can't announce without IP
		}
	}

	notify := fmt.Sprintf(
		"NOTIFY * HTTP/1.1\r\n"+
			"HOST: 239.255.255.250:1900\r\n"+
			"CACHE-CONTROL: max-age=1800\r\n"+
			"LOCATION: %s/device.xml\r\n"+
			"NT: urn:schemas-upnp-org:device:MediaServer:1\r\n"+
			"NTS: ssdp:alive\r\n"+
			"SERVER: Linux/2.6 UPnP/1.0 xg2g/1.4.0\r\n"+
			"USN: uuid:%s::urn:schemas-upnp-org:device:MediaServer:1\r\n"+
			"\r\n",
		baseURL,
		s.config.DeviceID,
	)

	if _, err := conn.WriteTo([]byte(notify), addr); err != nil {
		s.logger.Error().Err(err).Msg("failed to send SSDP NOTIFY")
	} else {
		s.logger.Debug().Msg("sent SSDP NOTIFY announcement")
	}
}

// getLocalIP gets the local IP address
func (s *Server) getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// HandleDeviceXML handles /device.xml endpoint for UPnP/SSDP discovery
func (s *Server) HandleDeviceXML(w http.ResponseWriter, r *http.Request) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
    <friendlyName>%s</friendlyName>
    <manufacturer>Silicondust</manufacturer>
    <manufacturerURL>http://www.silicondust.com/</manufacturerURL>
    <modelDescription>HDHomeRun ATSC Tuner</modelDescription>
    <modelName>%s</modelName>
    <modelNumber>%s</modelNumber>
    <modelURL>http://www.silicondust.com/</modelURL>
    <serialNumber></serialNumber>
    <UDN>uuid:%s</UDN>
    <presentationURL>%s</presentationURL>
  </device>
</root>`,
		s.config.FriendlyName,
		s.config.ModelName,
		s.config.ModelName,
		s.config.DeviceID,
		baseURL,
	)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if _, err := w.Write([]byte(xml)); err != nil {
		s.logger.Error().Err(err).Str("endpoint", "/device.xml").Msg("failed to write HDHomeRun device XML response")
	}

	s.logger.Debug().
		Str("endpoint", "/device.xml").
		Msg("HDHomeRun device.xml request")
}
