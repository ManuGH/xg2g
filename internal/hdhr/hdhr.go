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
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	xgconfig "github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
)

// Config holds HDHomeRun emulation configuration
type Config struct {
	Enabled          bool
	DeviceID         string
	FriendlyName     string
	ModelName        string
	FirmwareName     string
	BaseURL          string
	TunerCount       int
	PlexForceHLS     bool // Force HLS URLs in lineup.json for Plex iOS compatibility
	SSDPPort         int
	PlaylistFilename string
	DataDir          string
	Logger           zerolog.Logger
}

// Server implements HDHomeRun API endpoints
type Server struct {
	config         Config
	logger         zerolog.Logger
	channelManager *channels.Manager
}

// NewServer creates a new HDHomeRun emulation server
func NewServer(config Config, cm *channels.Manager) *Server {
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
	if config.SSDPPort == 0 {
		config.SSDPPort = 1900
	}

	return &Server{
		config:         config,
		logger:         config.Logger,
		channelManager: cm,
	}
}

// PlexForceHLS returns whether HLS URLs should be forced in lineup.json for Plex iOS compatibility
func (s *Server) PlexForceHLS() bool {
	return s.config.PlexForceHLS
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
func (s *Server) HandleLineup(w http.ResponseWriter, _ *http.Request) {
	// Read M3U playlist
	playlistName := s.config.PlaylistFilename
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Join(s.config.DataDir, playlistName)

	// #nosec G304 -- path is constructed from safe config
	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to read playlist for lineup")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse channels
	allChannels := m3u.Parse(string(data))
	var lineup []LineupEntry

	for _, ch := range allChannels {
		// Check if channel is enabled
		// Use TvgID as stable identifier, fallback to Name
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}

		if s.channelManager != nil && !s.channelManager.IsEnabled(id) {
			continue
		}

		lineup = append(lineup, LineupEntry{
			GuideNumber: ch.Number,
			GuideName:   ch.Name,
			URL:         ch.URL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lineup); err != nil {
		s.logger.Error().Err(err).Str("endpoint", "/lineup.json").Msg("failed to encode HDHomeRun lineup response")
	}

	s.logger.Debug().
		Str("endpoint", "/lineup.json").
		Int("channels", len(lineup)).
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
func GetConfigFromEnv(logger zerolog.Logger, dataDir string) Config {
	enabled := xgconfig.ParseBool("XG2G_HDHR_ENABLED", false)
	plexForceHLS := xgconfig.ParseBool("XG2G_PLEX_FORCE_HLS", false)

	return Config{
		Enabled:          enabled,
		DeviceID:         xgconfig.ParseString("XG2G_HDHR_DEVICE_ID", ""),
		FriendlyName:     xgconfig.ParseString("XG2G_HDHR_FRIENDLY_NAME", "xg2g"),
		ModelName:        xgconfig.ParseString("XG2G_HDHR_MODEL", "HDHR-xg2g"),
		FirmwareName:     xgconfig.ParseString("XG2G_HDHR_FIRMWARE", "xg2g-1.4.0"),
		BaseURL:          xgconfig.ParseString("XG2G_HDHR_BASE_URL", ""),
		TunerCount:       xgconfig.ParseInt("XG2G_HDHR_TUNER_COUNT", 4),
		PlexForceHLS:     plexForceHLS,
		PlaylistFilename: xgconfig.ParseString("XG2G_PLAYLIST_FILENAME", "playlist.m3u"),
		DataDir:          dataDir,
		Logger:           logger,
	}
}

// StartSSDPAnnouncer starts SSDP announcements for automatic discovery
func (s *Server) StartSSDPAnnouncer(ctx context.Context) error {
	ssdpPort := s.config.SSDPPort
	if ssdpPort == 0 {
		ssdpPort = 1900
	}

	// SSDP multicast address
	multicastAddr := fmt.Sprintf("239.255.255.250:%d", ssdpPort)

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	// Create UDP connection
	lc := &net.ListenConfig{}
	conn, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", ssdpPort))
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", ssdpPort, err)
	}

	// Join multicast group using ipv4.PacketConn
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		return fmt.Errorf("failed to cast connection to *net.UDPConn")
	}

	// Set read buffer size
	if err := udpConn.SetReadBuffer(2048); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set read buffer size")
	}

	// Wrap in ipv4.PacketConn for multicast operations
	p := ipv4.NewPacketConn(udpConn)

	// Set multicast options for better compatibility
	if err := p.SetMulticastTTL(2); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set multicast TTL")
	}
	if err := p.SetMulticastLoopback(true); err != nil {
		s.logger.Warn().Err(err).Msg("failed to set multicast loopback")
	}

	// Get all network interfaces and join multicast group on each
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to get network interfaces: %w", err)
	}

	// Parse multicast group IP
	groupIP := net.IPv4(239, 255, 255, 250)

	joinedCount := 0
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip interfaces without multicast support
		if iface.Flags&net.FlagMulticast == 0 {
			s.logger.Debug().
				Str("interface", iface.Name).
				Msg("skipping interface without multicast support")
			continue
		}

		// Try to join multicast group on this interface
		if err := p.JoinGroup(&iface, &net.UDPAddr{IP: groupIP}); err != nil {
			s.logger.Debug().
				Err(err).
				Str("interface", iface.Name).
				Msg("failed to join multicast group on interface")
		} else {
			s.logger.Info().
				Str("interface", iface.Name).
				Str("multicast_addr", multicastAddr).
				Msg("joined SSDP multicast group")
			joinedCount++

			// Set interface for multicast sending
			if err := p.SetMulticastInterface(&iface); err != nil {
				s.logger.Warn().
					Err(err).
					Str("interface", iface.Name).
					Msg("failed to set multicast interface")
			}
		}
	}

	if joinedCount == 0 {
		s.logger.Warn().Msg("failed to join multicast group on any interface, SSDP discovery may not work")
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
				// Check if context is done, if so, ignore error and return
				select {
				case <-ctx.Done():
					return
				default:
				}

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
