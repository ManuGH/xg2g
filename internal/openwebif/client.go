// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package openwebif provides a client for interacting with Enigma2 OpenWebIF API.
package openwebif

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/resilience"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

const (
	// maxDrainBytes caps the amount of data we drain from a response body
	// before closing it. This prevents goroutine stalls on unbounded streams
	// or buggy receivers. 4KB is sufficient to clear TCP buffers for small responses.
	maxDrainBytes = 4096

	// maxErrBody caps the amount of response body we read for error reporting (non-200).
	// We want enough context for stack traces but not unbounded memory usage.
	maxErrBody = 8 * 1024

	defaultServicesCapTTL = time.Hour
)

// Client is an OpenWebIF HTTP client for communicating with Enigma2 receivers.
type Client struct {
	base            string
	port            int
	http            *http.Client
	log             zerolog.Logger
	useWebIFStreams bool
	host            string
	timeout         time.Duration
	maxRetries      int
	backoff         time.Duration
	maxBackoff      time.Duration
	username        string
	password        string
	streamBaseURL   string

	// Request caching (v1.3.0+)

	// Timer Change Capabilities (v2.1.0+)
	timerChangeCap atomic.Value // stores *TimerChangeCap

	cache    Cacher
	cacheTTL time.Duration

	servicesCapMu  sync.RWMutex
	servicesCaps   map[string]servicesCapability
	servicesCapTTL time.Duration

	// Rate limiting for receiver protection (v1.7.0+)
	// Protects the Enigma2 receiver from being overwhelmed by requests
	receiverLimiter *rate.Limiter

	// Circuit Breaker for fault tolerance (v2.2.0+)
	cb *resilience.CircuitBreaker
}

// Cacher provides caching capabilities for OpenWebIF requests.
// This interface allows for different cache implementations (memory, Redis, etc.).
type Cacher interface {
	Get(key string) (any, bool)
	Set(key string, value any, ttl time.Duration)
	Delete(key string)
	Clear()
}

type servicesCapability struct {
	PreferFlat bool
	ExpiresAt  time.Time
}

// Options configures the OpenWebIF client behavior.
type Options struct {
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	MaxRetries            int
	Backoff               time.Duration
	MaxBackoff            time.Duration
	Username              string
	Password              string
	UseWebIFStreams       bool
	StreamBaseURL         string // Optional: override direct stream base URL (scheme+host[:port])

	// HTTP transport tuning (optional; defaults are safe).
	HTTPMaxConnsPerHost int

	// Caching options (v1.3.0+)
	Cache    Cacher        // Optional cache implementation
	CacheTTL time.Duration // Cache TTL (default: 5 minutes)

	// Rate limiting options (v1.7.0+)
	ReceiverRateLimit rate.Limit // Max requests/sec to receiver (default: 10)
	ReceiverBurst     int        // Burst capacity (default: 20)
}

// AboutInfo represents a subset of /api/about metadata.
type AboutInfo struct {
	Info struct {
		// Basic identification
		Model        string `json:"model"`
		Boxtype      string `json:"boxtype"`
		Brand        string `json:"brand"`
		MachineBuild string `json:"machinebuild"`

		// Hardware details
		Chipset                    string `json:"chipset"`
		FriendlyChipsetDescription string `json:"friendlychipsetdescription"`
		FriendlyChipsetText        string `json:"friendlychipsettext"`

		// Memory (OpenWebIF returns these in a non-intuitive order!)
		Mem1 string `json:"mem1"` // FREE memory (not used!) e.g., "577 MB"
		Mem2 string `json:"mem2"` // USED memory (not free!) e.g., "13 MB"
		Mem3 string `json:"mem3"` // TOTAL memory e.g., "590 MB"

		// Software versions
		OEVer               string      `json:"oever"`               // OE-Alliance version
		ImageVer            string      `json:"imagever"`            // OpenATV version
		ImageDistro         string      `json:"imagedistro"`         // "openatv"
		FriendlyImageDistro string      `json:"friendlyimagedistro"` // "OpenATV"
		EnigmaVer           string      `json:"enigmaver"`           // Enigma2 date
		KernelVer           string      `json:"kernelver"`           // Kernel version
		DriverDate          string      `json:"driverdate"`          // Driver date
		WebIFVer            string      `json:"webifver"`            // OWIF version
		FPVersion           interface{} `json:"fp_version"`          // Front panel version (often null)

		// Runtime
		Uptime string `json:"uptime"` // e.g., "1d 07:40"

		// Tuners
		Tuners      []AboutTuner `json:"tuners"`
		TunersCount int          `json:"tuners_count"`
		FBCTuners   []AboutTuner `json:"fbc_tuners"`

		// Capabilities
		LCD              int  `json:"lcd"`              // Has LCD display
		GrabPIP          int  `json:"grabpip"`          // Can grab PIP
		Transcoding      bool `json:"transcoding"`      // Supports transcoding
		TextInputSupport bool `json:"textinputsupport"` // Text input support

		// Storage and Network
		HDD    []HDDInfo          `json:"hdd"`    // Storage devices
		IFaces []NetworkInterface `json:"ifaces"` // Network interfaces
		Shares []interface{}      `json:"shares"` // Network shares (often empty)

		// Additional fields
		Streams interface{} `json:"streams"` // Active streams
	} `json:"info"`
	Service interface{} `json:"service"` // Current service info
}

// AboutTuner represents a tuner entry in /api/about.
type AboutTuner struct {
	Name   string `json:"name"`   // "Tuner A"
	Type   string `json:"type"`   // "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"
	Live   string `json:"live"`   // Service ref if tuner is live
	Rec    string `json:"rec"`    // Service ref if tuner is recording
	Stream string `json:"stream"` // Service ref if tuner is streaming
}

// HDDInfo represents storage device information.
type HDDInfo struct {
	Model            string `json:"model"`
	Capacity         string `json:"capacity"`
	LabelledCapacity string `json:"labelled_capacity"`
	Free             string `json:"free"`
	Mount            string `json:"mount"`
	FriendlyCapacity string `json:"friendlycapacity"` // "27.7 GB frei / 28.6 GB insgesamt"
}

// NetworkInterface represents a network interface.
type NetworkInterface struct {
	Name        string      `json:"name"`        // "eth0"
	FriendlyNIC string      `json:"friendlynic"` // "Broadcom Gigabit Ethernet"
	LinkSpeed   string      `json:"linkspeed"`   // "1 GBit/s"
	MAC         string      `json:"mac"`         // "00:1d:ec:0f:e3:ed"
	DHCP        bool        `json:"dhcp"`        // true if DHCP enabled
	IP          string      `json:"ip"`          // IPv4 address
	Mask        string      `json:"mask"`        // Netmask
	Gateway     string      `json:"gw"`          // Gateway
	IPv6        string      `json:"ipv6"`        // IPv6 address
	IPMethod    string      `json:"ipmethod"`    // IP method
	IPv4Method  string      `json:"ipv4method"`  // IPv4 method
	V4Prefix    int         `json:"v4prefix"`    // IPv4 prefix
	FirstPublic interface{} `json:"firstpublic"` // First public IP (often null)
}

const (
	defaultStreamPort    = 8001
	defaultTimeout       = 10 * time.Second
	defaultRetries       = 3
	defaultBackoff       = 500 * time.Millisecond
	maxTimeout           = 60 * time.Second
	maxRetries           = 10
	maxBackoff           = 30 * time.Second
	defaultReceiverRPS   = 10 // 10 requests/sec to receiver
	defaultReceiverBurst = 20 // Allow bursts up to 20
)

var (
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_openwebif_request_duration_seconds",
		Help:    "Duration of OpenWebIF HTTP requests per attempt",
		Buckets: prometheus.ExponentialBuckets(0.05, 2.0, 8),
	}, []string{"operation", "status", "attempt"})

	requestRetries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_retries_total",
		Help: "Number of OpenWebIF request retries performed",
	}, []string{"operation"})

	requestFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_failures_total",
		Help: "Number of failed OpenWebIF requests by error class",
	}, []string{"operation", "error_class"})

	requestSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_success_total",
		Help: "Number of successful OpenWebIF requests",
	}, []string{"operation"})
)

// New creates a new OpenWebIF client with the given options.
func New(base string) *Client {
	return NewWithPort(base, 0, Options{})
}

// NewWithPort creates a new OpenWebIF client with a specific port.
func NewWithPort(base string, streamPort int, opts Options) *Client {
	trimmedBase := strings.TrimRight(strings.TrimSpace(base), "/")
	port := resolveStreamPort(streamPort)
	host := extractHost(trimmedBase)
	logger := xglog.WithComponent("openwebif").With().Str("host", host).Logger()
	nopts := normalizeOptions(opts)

	dialerTimeout := 5 * time.Second
	tlsHandshakeTimeout := 5 * time.Second
	responseHeaderTimeout := 10 * time.Second
	if opts.ResponseHeaderTimeout > 0 {
		responseHeaderTimeout = opts.ResponseHeaderTimeout
	}

	maxConnsPerHost := opts.HTTPMaxConnsPerHost
	if maxConnsPerHost <= 0 {
		maxConnsPerHost = 50
	}

	// Create a dedicated, hardened transport with optimized pooling.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   dialerTimeout, // Connection timeout
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout, // Time to receive headers

		// Connection pooling / reuse
		DisableKeepAlives: true,            // HARDENED: Prevent connection reuse (fragile receivers)
		ForceAttemptHTTP2: false,           // HARDENED: Disable HTTP/2 to prevent surprises
		MaxConnsPerHost:   maxConnsPerHost, // cap total per-host conns to avoid exhaustion
	}

	// Create a client with the hardened transport.
	// The per-request timeout is handled by the context passed to Do().
	hardenedClient := &http.Client{
		Transport: transport,
		// Safety net: overall cap per attempt to prevent slow body hangs.
		Timeout: 30 * time.Second,
	}

	// Timeout configuration
	if opts.Timeout > 0 {
		// Warn if configured timeout exceeds the client's hard cap
		if opts.Timeout > hardenedClient.Timeout {
			logger.Warn().
				Dur("configured", opts.Timeout).
				Dur("hard_cap", hardenedClient.Timeout).
				Msg("Configured timeout > client hard cap; effective timeout is capped")
		}
	}

	// Default cache TTL
	cacheTTL := 5 * time.Minute
	if opts.CacheTTL > 0 {
		cacheTTL = opts.CacheTTL
	}

	// Configure receiver rate limiting (v1.7.0+)
	receiverRPS := rate.Limit(defaultReceiverRPS)
	if opts.ReceiverRateLimit > 0 {
		receiverRPS = opts.ReceiverRateLimit
	}
	receiverBurst := defaultReceiverBurst
	if opts.ReceiverBurst > 0 {
		receiverBurst = opts.ReceiverBurst
	}

	client := &Client{
		base:            trimmedBase,
		port:            port,
		http:            hardenedClient,
		log:             logger,
		host:            host,
		timeout:         nopts.Timeout,
		maxRetries:      nopts.MaxRetries,
		backoff:         nopts.Backoff,
		maxBackoff:      nopts.MaxBackoff,
		username:        opts.Username,
		password:        opts.Password,
		useWebIFStreams: nopts.UseWebIFStreams,
		streamBaseURL:   strings.TrimSpace(opts.StreamBaseURL),
		cache:           opts.Cache, // Optional cache (nil = no caching)
		cacheTTL:        cacheTTL,
		servicesCaps:    make(map[string]servicesCapability),
		servicesCapTTL:  defaultServicesCapTTL,
		receiverLimiter: rate.NewLimiter(receiverRPS, receiverBurst),
		cb:              resilience.NewCircuitBreaker("openwebif", 5, 10, 60*time.Second, 30*time.Second),
	}

	// Log cache status
	if client.cache != nil {
		logger.Info().Dur("ttl", cacheTTL).Msg("request caching enabled")
	}

	// Log rate limiting configuration
	logger.Info().
		Float64("rate_limit_rps", float64(receiverRPS)).
		Int("burst", receiverBurst).
		Msg("receiver rate limiting enabled")

	return client
}

func normalizeOptions(opts Options) Options {
	if opts.Timeout <= 0 || opts.Timeout > maxTimeout {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = defaultRetries
	}
	if opts.MaxRetries > maxRetries {
		opts.MaxRetries = maxRetries
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaultBackoff
	}
	if opts.Backoff > maxBackoff {
		opts.Backoff = maxBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 2 * time.Second
	}
	if opts.MaxBackoff > maxBackoff {
		opts.MaxBackoff = maxBackoff
	}
	// Ensure MaxBackoff >= Backoff
	if opts.MaxBackoff < opts.Backoff {
		opts.MaxBackoff = opts.Backoff
	}
	return opts
}

// About fetches receiver metadata from /api/about (best-effort).
func (c *Client) About(ctx context.Context) (*AboutInfo, error) {
	body, err := c.get(ctx, "/api/about", "about", nil)
	if err != nil {
		return nil, err
	}
	var info AboutInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse about response: %w", err)
	}
	return &info, nil
}

// Bouquets retrieves all available bouquets from the receiver.
func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	const cacheKey = "bouquets"

	// Check cache if available
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.(map[string]string); ok {
				c.loggerFor(ctx).Debug().Str("event", "cache.hit").Str("key", cacheKey).Msg("serving from cache")
				return result, nil
			}
		}
	}

	const path = "/api/bouquets"
	body, err := c.get(ctx, path, "bouquets", nil)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Bouquets [][]string `json:"bouquets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.loggerFor(ctx).Error().Err(err).Str("event", "openwebif.decode").Str("operation", "bouquets").Msg("failed to decode bouquets response")
		return nil, err
	}

	out := make(map[string]string, len(payload.Bouquets))
	for _, b := range payload.Bouquets {
		if len(b) == 2 {
			out[b[1]] = b[0]
		} // name -> ref
	}

	// Store in cache if available
	if c.cache != nil {
		c.cache.Set(cacheKey, out, c.cacheTTL)
		c.loggerFor(ctx).Debug().Str("event", "cache.set").Str("key", cacheKey).Dur("ttl", c.cacheTTL).Msg("cached result")
	}

	c.loggerFor(ctx).Info().Str("event", "openwebif.bouquets").Int("count", len(out)).Msg("fetched bouquets")
	return out, nil
}

// /api/bouquets: [["<ref>","<name>"], ...]

type svcPayload struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"servicereference"`
	} `json:"services"`
}

// svcPayloadFlat models the getallservices shape where the bouquet container
// contains a "subservices" array with the actual channel entries.
type svcPayloadFlat struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"servicereference"`
		Subservices []struct {
			ServiceName string `json:"servicename"`
			ServiceRef  string `json:"servicereference"`
		} `json:"subservices"`
	} `json:"services"`
}

var errServicesSchemaMismatch = errors.New("openwebif: services schema mismatch")

// EPGEvent represents a single programme entry from OpenWebIF EPG API
type EPGEvent struct {
	ID                  int    `json:"id"`
	Title               string `json:"title"`
	Description         string `json:"shortdesc"`
	DescriptionFallback string `json:"description"`
	LongDesc            string `json:"longdesc"`
	LongDescFallback    string `json:"descriptionextended"`
	Begin               int64  `json:"begin_timestamp"`
	Duration            int64  `json:"duration_sec"`
	SRef                string `json:"sref"`
}

// EPGResponse represents the OpenWebIF EPG API response structure
type EPGResponse struct {
	Events []EPGEvent `json:"events"`
	Result bool       `json:"result"`
}

// Timer represents an Enigma2 timer entry
type Timer struct {
	ServiceRef  string `json:"serviceref"`
	ServiceName string `json:"servicename"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Disabled    int    `json:"disabled"`
	JustPlay    int    `json:"justplay"`
	Begin       int64  `json:"begin"`
	End         int64  `json:"end"`
	Duration    int64  `json:"duration"`
	State       int    `json:"state"`
	Filename    string `json:"filename,omitempty"`
}

// TimerListResponse represents the response from /api/timerlist
type TimerListResponse struct {
	Result bool    `json:"result"`
	Timers []Timer `json:"timers"`
}

// TimerOpResponse represents the response from /api/timeradd or /api/timerdelete
type TimerOpResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

// Services retrieves all services for a given bouquet.
func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	// Check cache if available
	cacheKey := "services:" + bouquetRef
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.([][2]string); ok {
				c.loggerFor(ctx).Debug().Str("event", "cache.hit").Str("key", cacheKey).Msg("serving from cache")
				return result, nil
			}
		}
	}

	maskedRef := maskValue(bouquetRef)
	decorate := func(zc *zerolog.Context) {
		zc.Str("bouquet_ref", maskedRef)
	}
	type endpointSpec struct {
		path       string
		operation  string
		preferFlat bool
	}
	try := func(ep endpointSpec) ([][2]string, error) {
		body, err := c.get(ctx, ep.path, ep.operation, decorate)
		if err != nil {
			return nil, err
		}

		// Fast schema check: ensure "services" key exists and is not null.
		var shape struct {
			Services json.RawMessage `json:"services"`
		}
		if err := json.Unmarshal(body, &shape); err != nil {
			c.loggerFor(ctx).Error().Err(err).
				Str("event", "openwebif.decode").
				Str("operation", ep.operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response shape")
			return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
		}
		trimmedServices := strings.TrimSpace(string(shape.Services))
		if trimmedServices == "" || trimmedServices == "null" {
			// Legacy receivers may respond to getallservices with only a bouquets payload.
			// When this happens during fallback, treat it as an empty service list.
			if ep.preferFlat {
				var legacyShape struct {
					Bouquets json.RawMessage `json:"bouquets"`
				}
				if err := json.Unmarshal(body, &legacyShape); err == nil {
					trimmedBouquets := strings.TrimSpace(string(legacyShape.Bouquets))
					if trimmedBouquets != "" && trimmedBouquets != "null" {
						return [][2]string{}, nil
					}
				}
			}
			return nil, fmt.Errorf("%w: missing services field", errServicesSchemaMismatch)
		}

		// Flat endpoint: decode preserving subservices.
		if ep.preferFlat {
			var flat svcPayloadFlat
			if err := json.Unmarshal(body, &flat); err != nil {
				c.loggerFor(ctx).Error().Err(err).
					Str("event", "openwebif.decode").
					Str("operation", ep.operation).
					Str("bouquet_ref", maskedRef).
					Msg("failed to decode services response (flat)")
				return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
			}
			out := make([][2]string, 0, len(flat.Services)*4)
			for _, s := range flat.Services {
				// Check if this is a bouquet container with subservices
				if len(s.Subservices) > 0 {
					c.loggerFor(ctx).Debug().
						Str("container", s.ServiceName).
						Int("subservices_count", len(s.Subservices)).
						Msg("expanding bouquet container")
					for _, ch := range s.Subservices {
						// Skip any nested containers or invalid entries
						if strings.HasPrefix(ch.ServiceRef, "1:7:") || ch.ServiceRef == "" {
							continue
						}
						out = append(out, [2]string{ch.ServiceName, ch.ServiceRef})
					}
				} else if !strings.HasPrefix(s.ServiceRef, "1:7:") && s.ServiceRef != "" {
					// Regular service (not a container)
					out = append(out, [2]string{s.ServiceName, s.ServiceRef})
				}
			}
			return out, nil
		}

		// Nested endpoint: standard decode
		var p svcPayload
		if err := json.Unmarshal(body, &p); err != nil {
			c.loggerFor(ctx).Error().Err(err).
				Str("event", "openwebif.decode").
				Str("operation", ep.operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response")
			return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
		}
		out := make([][2]string, 0, len(p.Services))
		for _, s := range p.Services {
			// 1:7:* = Bouquet-Container; skip these in nested endpoint
			if strings.HasPrefix(s.ServiceRef, "1:7:") {
				continue
			}
			out = append(out, [2]string{s.ServiceName, s.ServiceRef})
		}
		return out, nil
	}

	endpoints := []endpointSpec{
		{
			path:       "/api/getservices?sRef=" + url.QueryEscape(bouquetRef),
			operation:  "services.nested",
			preferFlat: false,
		},
		{
			path:       "/api/getallservices?sRef=" + url.QueryEscape(bouquetRef),
			operation:  "services.flat",
			preferFlat: true,
		},
	}
	if preferFlat, ok := c.servicesCapabilityGet(); ok && preferFlat {
		endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
	}

	var empty [][2]string
	var successfulEndpoint string

	for i, ep := range endpoints {
		out, err := try(ep)
		if err != nil {
			if i == 0 && shouldTryServicesFallback(err) {
				c.loggerFor(ctx).Warn().
					Err(err).
					Str("event", "openwebif.services").
					Str("bouquet_ref", maskedRef).
					Str("from", ep.operation).
					Str("to", endpoints[1].operation).
					Msg("services fetch incompatible, trying fallback endpoint")
				continue
			}
			return nil, err
		}

		successfulEndpoint = ep.operation
		c.servicesCapabilitySet(ep.preferFlat)
		if len(out) > 0 {
			if c.cache != nil {
				c.cache.Set(cacheKey, out, c.cacheTTL)
				c.loggerFor(ctx).Debug().Str("event", "cache.set").Str("key", cacheKey).Dur("ttl", c.cacheTTL).Msg("cached result")
			}
			c.loggerFor(ctx).Info().
				Str("event", "openwebif.services").
				Str("bouquet_ref", maskedRef).
				Str("operation", ep.operation).
				Int("count", len(out)).
				Msg("fetched services")
			return out, nil
		}

		empty = out
		if i == 0 {
			continue
		}
	}

	// No services found but at least one endpoint returned a compatible payload.
	if successfulEndpoint != "" {
		if empty == nil {
			empty = [][2]string{}
		}
		if c.cache != nil {
			c.cache.Set(cacheKey, empty, c.cacheTTL)
		}
		c.loggerFor(ctx).Warn().
			Str("event", "openwebif.services").
			Str("bouquet_ref", maskedRef).
			Str("operation", successfulEndpoint).
			Msg("no services found for bouquet")
		return empty, nil
	}

	return nil, fmt.Errorf("%w: no compatible services endpoint", errServicesSchemaMismatch)
}

func shouldTryServicesFallback(err error) bool {
	return errors.Is(err, errServicesSchemaMismatch) || errors.Is(err, ErrNotFound)
}

func (c *Client) servicesCapabilityKey() string {
	return c.host
}

func (c *Client) servicesCapabilityGet() (bool, bool) {
	key := c.servicesCapabilityKey()
	now := time.Now()

	c.servicesCapMu.RLock()
	cap, ok := c.servicesCaps[key]
	c.servicesCapMu.RUnlock()
	if ok && now.Before(cap.ExpiresAt) {
		return cap.PreferFlat, true
	}

	// Entry is missing or expired. Re-check under write lock to avoid deleting
	// a freshly refreshed capability.
	c.servicesCapMu.Lock()
	defer c.servicesCapMu.Unlock()
	cap, ok = c.servicesCaps[key]
	if ok && time.Now().Before(cap.ExpiresAt) {
		return cap.PreferFlat, true
	}
	if ok {
		delete(c.servicesCaps, key)
	}
	return false, false
}

func (c *Client) servicesCapabilitySet(preferFlat bool) {
	if c.servicesCapTTL <= 0 {
		return
	}
	key := c.servicesCapabilityKey()
	exp := time.Now().Add(c.servicesCapTTL)

	c.servicesCapMu.Lock()
	c.servicesCaps[key] = servicesCapability{
		PreferFlat: preferFlat,
		ExpiresAt:  exp,
	}
	c.servicesCapMu.Unlock()
}

// GetTimers retrieves the list of timers from the receiver.
func (c *Client) GetTimers(ctx context.Context) ([]Timer, error) {
	// Timers change frequently, so we don't cache them aggressively
	// or we use a very short TTL if we did. For now, no caching.
	body, err := c.get(ctx, "/api/timerlist", "timers.list", nil)
	if err != nil {
		return nil, err
	}

	var payload TimerListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		c.loggerFor(ctx).Error().Err(err).Str("event", "openwebif.decode").Str("operation", "timers.list").Msg("failed to decode timer list")
		return nil, err
	}

	return payload.Timers, nil
}

// AddTimer schedules a new recording.
func (c *Client) AddTimer(ctx context.Context, sRef string, begin, end int64, name, description string) error {
	// URL Encode parameters
	params := url.Values{}
	params.Set("sRef", sRef)
	params.Set("begin", strconv.FormatInt(begin, 10))
	params.Set("end", strconv.FormatInt(end, 10))
	params.Set("name", name)
	params.Set("description", description)
	// Defaults
	params.Set("disabled", "0")
	params.Set("justplay", "0")
	params.Set("after_event", "3") // Auto

	path := "/api/timeradd?" + params.Encode()
	body, err := c.get(ctx, path, "timers.add", nil)
	if err != nil {
		return err
	}

	var resp TimerOpResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode timer add response: %w", err)
	}

	if !resp.Result {
		return timerOperationError("timers.add", resp.Message)
	}

	return nil
}

// DeleteTimer removes an existing timer.
// Enigma2 requires exact matching of sRef, begin, and end.
func (c *Client) DeleteTimer(ctx context.Context, sRef string, begin, end int64) error {
	params := url.Values{}
	params.Set("sRef", sRef)
	params.Set("begin", strconv.FormatInt(begin, 10))
	params.Set("end", strconv.FormatInt(end, 10))

	path := "/api/timerdelete?" + params.Encode()
	body, err := c.get(ctx, path, "timers.delete", nil)
	if err != nil {
		return err
	}

	var resp TimerOpResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode timer delete response: %w", err)
	}

	if !resp.Result {
		return timerOperationError("timers.delete", resp.Message)
	}

	return nil
}

// UpdateTimer updates a timer using the best available strategy.
// Strategy:
// 1. Detect capabilities (Cached).
// 2. If supported, try Flavor A (most common for changes).
// 3. If Flavor A rejected (specific error), Promote to Flavor B and retry once.
// 4. If unsupported or failed, use Fail-Closed Fallback (Add -> Delete).
func (c *Client) UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error {
	// 0. Detect
	cap, err := c.DetectTimerChange(ctx)
	if err != nil {
		observeTimerUpdate("terminal_failure", "none", TimerChangeFlavorUnknown, TimerChangeCap{})
		return err
	}

	result := "terminal_failure"
	reason := "none"
	nativeFlavor := TimerChangeFlavorUnknown

	defer func() {
		observeTimerUpdate(result, reason, nativeFlavor, cap)
	}()

	var fallbackReason string
	if !cap.Supported {
		fallbackReason = "unsupported"
	}

	// 1. Check Forbidden
	if cap.Forbidden {
		return ErrForbidden
	}

	// 2. Supported Path
	if cap.Supported {
		// Determine Flavor
		flavor := cap.Flavor
		if flavor == TimerChangeFlavorUnknown {
			flavor = TimerChangeFlavorA // Default start
		}
		nativeFlavor = flavor

		// Helper to execute update
		doUpdate := func(f TimerChangeFlavor) error {
			// 2b. Flavor B (Strict & In-Place Only)
			// User Requirement: "Flavor B is in-place property update only. Must use old identity."
			var params url.Values
			if f == TimerChangeFlavorB {
				// Identity Guard: If identity changes, we CANNOT use Flavor B (because it uses old identity to target).
				// We must fall back to Add+Delete.
				identityChanged := oldSRef != newSRef || oldBegin != newBegin || oldEnd != newEnd
				if identityChanged {
					fallbackReason = "identity_mismatch"
					// Return synthetic error to break out and hit fallback
					return fmt.Errorf("flavor B does not support identity changes")
				}
				params = c.buildTimerChangeFlavorB(oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, name, description, enabled)
			} else {
				// Flavor A (Channel + Change Params)
				params = c.buildTimerChangeFlavorA(oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, name, description)
			}

			path := "/api/timerchange?" + params.Encode()
			// Use a shorter timeout for the update attempt? Currently inheriting ctx.
			body, err := c.get(ctx, path, "timers.op.change", nil)
			if err != nil {
				// Technical error (Network/500). DO NOT Promote.
				// User Requirement: "Never promote on technical errors"
				return err
			}

			// Decode Response
			var resp TimerOpResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return fmt.Errorf("failed to decode timer update response: %w", err)
			}
			if !resp.Result {
				// Logic Failure (200 OK but Result=false).
				// Convert to typed timer operation error for classification.
				return timerOperationError("timers.change", resp.Message)
			}
			return nil
		}

		// Attempt 1 (Native)
		err := doUpdate(flavor)
		if err == nil {
			// Success!
			result = "success"
			// If we started Unknown, update cache to confirmed flavor
			if cap.Flavor == TimerChangeFlavorUnknown {
				cap.Flavor = flavor // Confirm A or whatever we used
				cap.DetectedAt = time.Now()
				var copy = cap
				c.timerChangeCap.Store(&copy)
			}
			return nil
		}

		// --- ELIGIBILITY CHECK FOR PROMOTION OR FALLBACK ---

		// 2.1 Technical Error Check (Terminal)
		// User requirement 2: "Technical errors must be terminal (no fallback)"
		if c.isTechnicalError(err) {
			return err
		}

		// 2.2 Conflict Check (Terminal)
		// User requirement 3: "Conflicts are semantic and terminal (no fallback)"
		if c.isConflictError(err) {
			return err
		}

		// 2.3 Promotion Check (A -> B)
		// Only if we are at A and it's a param rejection
		if flavor == TimerChangeFlavorA {
			var owiErr *OWIError
			if errors.As(err, &owiErr) && c.shouldPromoteAToB(owiErr) {
				// Promote
				c.log.Warn().Err(err).Msg("UpdateTimer: promoting to Flavor B based on receiver feedback")
				errRetry := doUpdate(TimerChangeFlavorB)
				if errRetry == nil {
					// Success on B! Cache it.
					result = "success"
					cap.Flavor = TimerChangeFlavorB
					cap.DetectedAt = time.Now()
					var copy = cap
					c.timerChangeCap.Store(&copy)
					return nil
				}

				// Retry on B failed.
				// Re-evaluate eligibility for fallback from this new error.
				if c.isTechnicalError(errRetry) || c.isConflictError(errRetry) {
					return errRetry
				}
				err = errRetry // Continue to fallback if allowed
			}
		}

		// 2.4 Final Fallback Eligibility Check
		// User requirement 1.3: "Param-rejection fallback allowed (when appropriate)"
		var owiErr *OWIError
		if errors.As(err, &owiErr) {
			if !c.isSafeForFallback(owiErr) {
				// Not a known safe fallback condition
				return err
			}
			fallbackReason = "param_rejection"
		} else if fallbackReason == "" {
			// If it's not an OWIError and not identity mismatch (which sets reason),
			// and we reach here, it's likely an unhandled error type that is NOT technical.
			// But according to rule 1.3, fallback is ONLY allowed for specific OWIError matches.
			return err
		}

		c.loggerFor(ctx).Warn().
			Str("reason", fallbackReason).
			Str("native_flavor", string(flavor)).
			Bool("cap_supported", cap.Supported).
			Str("cap_flavor", string(cap.Flavor)).
			Err(err).
			Msg("UpdateTimer: native update skipped/failed, using selective fallback")

		reason = fallbackReason
	}

	// 3. Fallback (Add First, Then Delete)
	// Fail-Closed: If Add Preflight fails, abort.

	// Preflight Validation (Pure)
	if newBegin >= newEnd {
		return fmt.Errorf("invalid timer period: begin >= end")
	}
	if newSRef == "" {
		return fmt.Errorf("missing service reference")
	}

	// Step A: Add Timer using standard method
	if err := c.AddTimer(ctx, newSRef, newBegin, newEnd, name, description); err != nil {
		return fmt.Errorf("fallback add failed: %w", err)
	}

	// Step B: Add Succeeded -> Delete Old using standard method
	// If delete fails now, we have a DUPLICATE.
	if err := c.DeleteTimer(ctx, oldSRef, oldBegin, oldEnd); err != nil {
		result = "partial_failure"
		// CRITICAL: Partial Failure.
		c.log.Error().
			Str("old_sref", oldSRef).
			Int64("old_begin", oldBegin).
			Msg("UpdateTimer: Fallback partial failure! Added new timer but failed to delete old one. Duplicate risk.")

		return ErrTimerUpdatePartial
	}

	result = "fallback_success"
	return nil
}

// shouldPromoteAToB decides if we should switch from Flavor A to Flavor B
// based on the error response from the server.
func (c *Client) shouldPromoteAToB(owiErr *OWIError) bool {
	// Rule: Limit to 400, or 200 (logic error).
	if owiErr.Status != http.StatusBadRequest && owiErr.Status != http.StatusOK {
		return false
	}

	msg := strings.ToLower(owiErr.Body)

	// User Requirement 1.3: "AND message does NOT match conflict tokens"
	if c.isConflict(owiErr.Body) {
		return false
	}

	// Whitelist check: indicates receiver didn't understand "channel" or "change_*" params.
	keys := []string{"channel", "change_"}
	signals := []string{"unknown parameter", "unknown argument"}

	for _, s := range signals {
		if strings.Contains(msg, s) {
			for _, k := range keys {
				if strings.Contains(msg, k) {
					return true
				}
			}
		}
	}
	return false
}

// isSafeForFallback determines if a logic error (OWIError) is "safe" to trigger
// the Add-then-Delete fallback. Safe means it implies the receiver did not apply the change.
func (c *Client) isSafeForFallback(owiErr *OWIError) bool {
	// Rule 1.3: Param-rejection logic error (Status 400 or 200/Result=false)
	if owiErr.Status != http.StatusBadRequest && owiErr.Status != http.StatusOK {
		return false
	}

	// Must NOT be a conflict
	if c.isConflict(owiErr.Body) {
		return false
	}

	// Whitelist match (same as promotion signals)
	msg := strings.ToLower(owiErr.Body)
	signals := []string{"unknown parameter", "unknown argument"}
	keys := []string{"channel", "change_", "sref", "begin", "end", "disabled"}

	for _, s := range signals {
		if strings.Contains(msg, s) {
			for _, k := range keys {
				if strings.Contains(msg, k) {
					return true
				}
			}
		}
	}

	// Also allow if it's the synthetic "identity change" error
	if strings.Contains(msg, "flavor b does not support identity changes") {
		return true
	}

	return false
}

func (c *Client) isTechnicalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		if owiErr.Status >= 500 {
			return true
		}
	}
	return false
}

func (c *Client) isConflictError(err error) bool {
	return IsTimerConflict(err)
}

func (c *Client) isConflict(msg string) bool {
	return timerMessageHasAnyToken(msg, timerConflictTokens)
}

// buildTimerChangeFlavorA builds parameters for Flavor A (channel + change_*).
// Used by: some distributions like openATV (variant).
// Keys: sRef, begin, end, channel, change_begin, change_end, change_name, change_description, deleteOldOnSave
func (c *Client) buildTimerChangeFlavorA(oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string) url.Values {
	params := url.Values{}
	// Identity
	params.Set("sRef", oldSRef)
	params.Set("begin", strconv.FormatInt(oldBegin, 10))
	params.Set("end", strconv.FormatInt(oldEnd, 10))

	// Changes
	params.Set("channel", newSRef)
	params.Set("change_begin", strconv.FormatInt(newBegin, 10))
	params.Set("change_end", strconv.FormatInt(newEnd, 10))
	params.Set("change_name", name)
	params.Set("change_description", description)

	params.Set("deleteOldOnSave", "1")
	return params
}

// buildTimerChangeFlavorB constructs parameters for "modern" OpenWebIF (e.g. Dreambox/forks).
// Behavior:
//   - Uses "sRef", "begin", "end" to identify the timer (based on OLD identity).
//   - Uses "name", "description" for property updates.
//   - Maps "enabled" bool to "disabled" param (0=enabled, 1=disabled).
//   - Does NOT use "channel" or "change_*" keys.
//   - NOTE: This flavor supports in-place property updates ONLY. Identity changes (move/reschedule)
//     MUST use the fallback add/delete mechanism.
func (c *Client) buildTimerChangeFlavorB(oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, desc string, enabled bool) url.Values {
	v := url.Values{}

	// Identity: Uses OLD identity to target the timer
	v.Set("sRef", oldSRef)
	v.Set("begin", strconv.FormatInt(oldBegin, 10))
	v.Set("end", strconv.FormatInt(oldEnd, 10))

	// Properties
	v.Set("name", name)
	v.Set("description", desc)

	// Enabled State (Inverted Logic: disabled=0 is enabled)
	if enabled {
		v.Set("disabled", "0")
	} else {
		v.Set("disabled", "1")
	}

	// Strictly NO other parameters to avoid confusion
	return v
}

// HasTimerChange checks if the receiver supports /api/timerchange
// DetectTimerChange checks for /api/timerchange support using a dedicated probe.
// It caches results according to strict rules:
// - Supported (200/400) -> cache
// - Forbidden (401/403) -> cache (short TTL)
// - MethodNotAllowed (405) -> Supported (cache) (Endpoint exists but GET disallowed)
// - Missing (404) -> cache
// - Unknown (5xx/Network) -> DO NOT cache
func (c *Client) DetectTimerChange(ctx context.Context) (TimerChangeCap, error) {
	// 1. Check Cache
	if val := c.timerChangeCap.Load(); val != nil {
		cap := val.(*TimerChangeCap)
		if cap != nil && !cap.DetectedAt.IsZero() {
			ttl := 10 * time.Minute
			if cap.Forbidden {
				ttl = 1 * time.Minute
			}
			if time.Since(cap.DetectedAt) < ttl {
				return *cap, nil
			}
		}
	}

	// 2. Probe
	// We use a safe GET request. "timers.change.detect" label.
	_, err := c.get(ctx, "/api/timerchange?__probe=1", "timers.change.detect", nil)

	cap := TimerChangeCap{
		DetectedAt: time.Now(),
	}

	// Helper to store cache
	store := func(cap TimerChangeCap) {
		cap.DetectedAt = time.Now()
		// Store as pointer to handle cache invalidation semantics (nil check)
		var copy = cap
		c.timerChangeCap.Store(&copy)
	}

	if err == nil {
		// 200 OK (Exits).
		cap.Supported = true
		cap.Flavor = TimerChangeFlavorUnknown // Will be promoted later
		store(cap)
		return cap, nil
	}

	// 3. Status Error Handling
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		switch owiErr.Status {
		case http.StatusNotFound:
			// 404 -> Missing. Supported=false. Cache it.
			cap.Supported = false
			store(cap)
			return cap, nil

		case http.StatusUnauthorized, http.StatusForbidden:
			// 401/403 -> Exists but forbidden. Supported=true, Forbidden=true. Cache it.
			cap.Supported = true
			cap.Forbidden = true
			store(cap)
			return cap, nil

		case http.StatusMethodNotAllowed:
			// 405 -> Method Not Allowed (e.g. GET forbidden). Supported=true. Cache it.
			cap.Supported = true
			store(cap)
			return cap, nil

		case http.StatusBadRequest:
			// 400 -> Exists (bad params). Supported=true. Cache it.
			cap.Supported = true
			cap.Flavor = TimerChangeFlavorUnknown
			store(cap)
			return cap, nil
		}
	}

	// 4. Unknown/Network Error (5xx or transport error)
	// User requirement: "Unknown nie cachen."
	return TimerChangeCap{}, err
}

// StreamURL builds a streaming URL for the given service reference.
// Context is used for smart stream detection and tracing.
func (c *Client) StreamURL(ctx context.Context, ref, name string) (string, error) {
	_ = ctx

	parsed, err := parseOpenWebIFBaseURL(c.base)
	if err != nil {
		return "", err
	}

	if c.useWebIFStreams {
		return buildWebIFStreamURL(parsed, ref, name, c.username, c.password)
	}

	if u, ok := buildDirectStreamOverrideURL(c.streamBaseURL, ref); ok {
		return u, nil
	}

	return buildDirectTSStreamURL(parsed, ref, c.port)
}

func parseOpenWebIFBaseURL(rawBase string) (*url.URL, error) {
	base := strings.TrimSpace(rawBase)
	if base == "" {
		return nil, fmt.Errorf("openwebif base URL is empty")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse openwebif base URL %q: %w", base, err)
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("openwebif base URL %q missing host", base)
	}
	return parsed, nil
}

func buildWebIFStreamURL(parsed *url.URL, ref, name, username, password string) (string, error) {
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("openwebif base URL %q missing hostname", parsed.String())
	}

	q := url.Values{}
	q.Set("ref", ref)
	q.Set("name", name)

	u := &url.URL{
		Scheme:   parsed.Scheme,
		Host:     parsed.Host,
		Path:     "/web/stream.m3u",
		RawQuery: q.Encode(),
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String(), nil
}

func buildDirectStreamOverrideURL(rawStreamBase, ref string) (string, bool) {
	streamBase := strings.TrimSpace(rawStreamBase)
	if streamBase == "" {
		return "", false
	}

	streamParsed, err := url.Parse(streamBase)
	if err != nil || streamParsed.Host == "" {
		return "", false
	}

	u := &url.URL{
		Scheme: streamParsed.Scheme,
		Host:   streamParsed.Host,
		Path:   "/" + ref,
	}
	return u.String(), true
}

func buildDirectTSStreamURL(parsed *url.URL, ref string, streamPort int) (string, error) {
	host, err := resolveDirectTSHost(parsed, streamPort)
	if err != nil {
		return "", err
	}

	u := &url.URL{
		Scheme: parsed.Scheme,
		Host:   host,
		Path:   "/" + ref,
	}
	return u.String(), nil
}

func resolveDirectTSHost(parsed *url.URL, streamPort int) (string, error) {
	host := parsed.Host
	if host == "" {
		return "", fmt.Errorf("openwebif base URL %q missing host", parsed.String())
	}

	_, existingPort, err := net.SplitHostPort(host)
	if err == nil && existingPort != "" {
		return host, nil
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("openwebif base URL %q missing hostname", parsed.String())
	}

	if streamPort <= 0 {
		streamPort = defaultStreamPort
	}
	return net.JoinHostPort(hostname, strconv.Itoa(streamPort)), nil
}

// StatusInfo represents the receiver status from /api/statusinfo
type StatusInfo struct {
	Result      bool   `json:"result"`
	InStandby   string `json:"inStandby"`   // "true" or "false"
	IsRecording string `json:"isRecording"` // "true" or "false"
	ServiceName string `json:"currservice_name"`
	ServiceRef  string `json:"currservice_serviceref"`
}

// CurrentInfo represents the detailed service info from /api/getcurrent
type CurrentInfo struct {
	Result bool `json:"result"`
	Info   struct {
		ServiceRef  string `json:"serviceref"`
		ServiceName string `json:"name"`
		VideoPID    int    `json:"vpid,omitempty"`
		AudioPID    int    `json:"apid,omitempty"`
		PMTPID      int    `json:"pmtpid,omitempty"`
		PCRPID      int    `json:"pcrpid,omitempty"`
		TXT         int    `json:"txtpid,omitempty"`
		TSID        int    `json:"tsid,omitempty"`
		ONID        int    `json:"onid,omitempty"`
		SID         int    `json:"sid,omitempty"`
	} `json:"info"`
	Now struct {
		EventTitle               string `json:"title"`
		EventDescription         string `json:"shortdesc"`
		EventDescriptionExtended string `json:"longdesc"`
		EventStart               int64  `json:"begin_timestamp"`
		EventDuration            int    `json:"duration_sec"`
	} `json:"now"`
	Next struct {
		EventTitle               string `json:"title"`
		EventDescription         string `json:"shortdesc"`
		EventDescriptionExtended string `json:"longdesc"`
		EventStart               int64  `json:"begin_timestamp"`
		EventDuration            int    `json:"duration_sec"`
	} `json:"next"`
}

// SignalInfo represents the tuner signal stats from /api/signal
type SignalInfo struct {
	Result    bool   `json:"result"`
	SNR       int    `json:"snr"`
	AGC       int    `json:"agc"`
	BER       int    `json:"ber"`
	InStandby string `json:"inStandby"` // "true"/"false" (API inconsistency)
}

// GetStatusInfo fetches current receiver status (recording, standby, service).
func (c *Client) GetStatusInfo(ctx context.Context) (*StatusInfo, error) {
	// Short cache TTL (1s) to avoid hammering but allow rapid UI updates
	const cacheKey = "statusinfo"
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.(*StatusInfo); ok {
				return result, nil
			}
		}
	}

	body, err := c.get(ctx, "/api/statusinfo", "status.info", nil)
	if err != nil {
		return nil, err
	}

	var info StatusInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode status info: %w", err)
	}

	if c.cache != nil {
		c.cache.Set(cacheKey, &info, 2*time.Second) // 2s
	}

	return &info, nil
}

// GetCurrent fetches detailed current service information (PIDs, etc).
func (c *Client) GetCurrent(ctx context.Context) (*CurrentInfo, error) {
	// Status data changes rapidly, so very short TTL or no cache
	// We want fresh data for readiness checks.
	body, err := c.get(ctx, "/api/getcurrent", "get.current", nil)
	if err != nil {
		return nil, err
	}

	var info CurrentInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode current info: %w", err)
	}
	return &info, nil
}

// GetSignal fetches signal statistics (SNR, BER, etc).
func (c *Client) GetSignal(ctx context.Context) (*SignalInfo, error) {
	body, err := c.get(ctx, "/api/signal", "get.signal", nil)
	if err != nil {
		return nil, err
	}

	var info SignalInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode signal info: %w", err)
	}
	return &info, nil
}

func resolveStreamPort(port int) int {
	if port > 0 && port <= 65535 {
		return port
	}
	return defaultStreamPort
}

func (c *Client) backoffDuration(attempt int) time.Duration {
	if c.backoff <= 0 {
		return 0
	}
	factor := 1 << (attempt - 1)
	d := time.Duration(factor) * c.backoff
	if d > c.maxBackoff {
		d = c.maxBackoff
	}
	return d
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return netErr.Timeout()
		}
		return true
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 {
		return true
	}
	return false
}

func classifyError(err error, status int) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() {
				return "timeout"
			}
			return "network"
		}
		return "error"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status >= 400 {
		return "http_4xx"
	}
	if status == 0 {
		return "unknown"
	}
	return "ok"
}

func wrapError(operation string, err error, status int, body []byte) error {
	var sentinel error
	var snippet string

	// Append body snippet if available (useful for Enigma2 stack traces)
	if len(body) > 0 {
		limit := 500
		if len(body) < limit {
			limit = len(body)
		}
		snippet = string(body[:limit])
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = strings.ReplaceAll(snippet, "\r", "")

		// Crude redaction for sensitive patterns (tokens, passwords, session keys)
		sensitivePatterns := []string{"token=", "password=", "secret=", "key=", "sid="}
		for _, pattern := range sensitivePatterns {
			if idx := strings.Index(strings.ToLower(snippet), pattern); idx != -1 {
				// Redact 16 chars or until space after the pattern
				start := idx + len(pattern)
				end := start + 16
				if end > len(snippet) {
					end = len(snippet)
				}
				if spaceIdx := strings.Index(snippet[start:end], " "); spaceIdx != -1 {
					end = start + spaceIdx
				}
				snippet = snippet[:start] + "[REDACTED]" + snippet[end:]
			}
		}
	}

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			sentinel = ErrTimeout
		} else {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				sentinel = ErrTimeout
			} else {
				sentinel = ErrUpstreamUnavailable
			}
		}
		return &OWIError{
			Sentinel:  sentinel,
			Operation: operation,
			Status:    status,
			Body:      snippet,
			Err:       err,
		}
	}

	switch status {
	case http.StatusNotFound:
		sentinel = ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		sentinel = ErrForbidden
	default:
		if status >= 500 {
			sentinel = ErrUpstreamError
		} else if status >= 400 {
			sentinel = ErrUpstreamBadResponse
		} else {
			sentinel = ErrUpstreamBadResponse // Treat non-200/4xx/5xx as bad response
		}
	}

	return &OWIError{
		Sentinel:  sentinel,
		Operation: operation,
		Status:    status,
		Body:      snippet,
	}
}

func logPath(path string) string {
	endpoint := path
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return endpoint
}

func (c *Client) logAttempt(
	ctx context.Context,
	operation string,
	path string,
	attempt, maxAttempts, status int,
	duration time.Duration,
	err error,
	errClass string,
	retry bool,
	decorate func(*zerolog.Context),
) {
	builder := c.loggerFor(ctx).With().
		Str("event", "openwebif.request").
		Str("operation", operation).
		Str("method", http.MethodGet).
		Str("endpoint", logPath(path)).
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Int64("duration_ms", duration.Milliseconds()).
		Str("error_class", errClass)
	if status > 0 {
		builder = builder.Int("status", status)
	}
	if decorate != nil {
		decorate(&builder)
	}
	logger := builder.Logger()
	if err == nil && status == http.StatusOK {
		logger.Info().Msg("openwebif request completed")
		return
	}
	if retry {
		logger.Warn().Err(err).Msg("openwebif request retry")
		return
	}
	logger.Error().Err(err).Msg("openwebif request failed")
}

func recordAttemptMetrics(operation string, attempt, status int, duration time.Duration, success bool, errClass string, retry bool) {
	statusLabel := "0"
	if status > 0 {
		statusLabel = strconv.Itoa(status)
	}
	requestDuration.WithLabelValues(operation, statusLabel, strconv.Itoa(attempt)).Observe(duration.Seconds())
	if success {
		requestSuccess.WithLabelValues(operation).Inc()
		return
	}
	requestFailures.WithLabelValues(operation, errClass).Inc()
	if retry {
		requestRetries.WithLabelValues(operation).Inc()
	}
}

func (c *Client) get(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) ([]byte, error) {
	// Apply receiver rate limiting before making request
	if err := c.receiverLimiter.Wait(ctx); err != nil {
		c.loggerFor(ctx).Warn().
			Err(err).
			Str("event", "openwebif.rate_limit").
			Str("operation", operation).
			Msg("rate limit wait cancelled")
		return nil, fmt.Errorf("rate limit wait cancelled: %w", err)
	}

	// Wrap request in Circuit Breaker
	if !c.cb.AllowRequest() {
		c.loggerFor(ctx).Warn().
			Str("event", "circuit_breaker.open").
			Str("operation", operation).
			Msg("request blocked by circuit breaker")
		return nil, resilience.ErrCircuitOpen
	}

	result, err := c.doGet(ctx, path, operation, decorate)
	if err != nil {
		// Only record technical failures (network, timeout, etc.)
		if isTechnicalError(err) {
			c.cb.RecordTechnicalFailure()
		}
		return nil, err
	}

	c.cb.RecordSuccess()
	return result, nil
}

func isTechnicalError(err error) bool {
	if err == nil {
		return false
	}
	// Connection errors, timeouts, and context cancellations are technical failures
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// For HTTP, we might want to check status code if wrapped
	return false
}

// doGet performs the actual HTTP request with retries (extracted from get)
func (c *Client) doGet(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) ([]byte, error) {

	maxAttempts := c.maxRetries + 1
	var lastErr error
	var lastStatus int
	var lastData []byte
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var res *http.Response
		var err error
		var status int
		var duration time.Duration
		var data []byte

		func() {
			attemptCtx := ctx
			var cancel context.CancelFunc
			if c.timeout > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, c.timeout)
				defer cancel() // Ensure cancel is always called
			}

			req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, c.base+path, nil)
			if reqErr != nil {
				c.loggerFor(ctx).Error().Err(reqErr).
					Str("event", "openwebif.request.build").
					Str("operation", operation).
					Msg("failed to build OpenWebIF request")
				err = reqErr
				return
			}

			// Add HTTP Basic Auth if credentials are provided
			if c.username != "" && c.password != "" {
				req.SetBasicAuth(c.username, c.password)
			}

			// HYGIENE: Enforce connection closure
			req.Close = true
			req.Header.Set("Connection", "close")

			start := time.Now()
			res, err = c.http.Do(req)
			duration = time.Since(start)
			if res != nil {
				status = res.StatusCode
				defer func() {
					if res.Body != nil {
						// HYGIENE: Drain body safely to ensure TCP socket hygiene
						// Limit drain to maxDrainBytes to prevent unbounded reads on stuck streams
						// EOF is expected and ignored.
						_, _ = io.CopyN(io.Discard, res.Body, maxDrainBytes)
						_ = res.Body.Close()
					}
				}()
			}

			if err == nil {
				if status == http.StatusOK {
					// Read body fully while attemptCtx is still active
					var readErr error

					// Check Content-Type header for charset
					contentType := res.Header.Get("Content-Type")

					// Read raw bytes first
					rawData, readErr := io.ReadAll(res.Body)
					if readErr != nil {
						err = readErr
						return
					}

					// Handle encoding if needed (e.g., ISO-8859-1)
					if needsLatin1Conversion(rawData, contentType) {
						data = convertLatin1ToUTF8(rawData)
					} else {
						data = rawData
					}
				} else if res.Body != nil {
					// HYGIENE: For non-200 responses, read a bounded snippet for context/logging.
					// This ensures we have debug data (e.g. Enigma2 stack traces) without risking huge reads.
					// The rest will be drained by the defer.
					// Applies the same charset conversion logic as success path (e.g., ISO-8859-1).
					rawSnippet, _ := io.ReadAll(io.LimitReader(res.Body, maxErrBody))

					contentType := res.Header.Get("Content-Type")
					if needsLatin1Conversion(rawSnippet, contentType) {
						data = convertLatin1ToUTF8(rawSnippet)
					} else {
						data = rawSnippet
					}
				}
			}
		}()

		// Metrics & Logging
		success := err == nil && status == http.StatusOK
		errClass := classifyError(err, status)
		retry := !success && attempt < maxAttempts && shouldRetry(status, err)

		c.logAttempt(ctx, operation, path, attempt, maxAttempts, status, duration, err, errClass, retry, decorate)
		recordAttemptMetrics(operation, attempt, status, duration, success, errClass, retry)

		if success {
			return data, nil
		}

		lastErr = err
		lastStatus = status
		lastData = data

		if !retry {
			break
		}

		// Wait before retry
		if attempt < maxAttempts {
			sleep := c.backoffDuration(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(sleep):
				continue
			}
		}
	}

	return nil, wrapError(operation, lastErr, lastStatus, lastData)
}

func (c *Client) loggerFor(ctx context.Context) *zerolog.Logger {
	logger := xglog.WithContext(ctx, c.log)
	return &logger
}

func extractHost(base string) string {
	if base == "" {
		return ""
	}
	if strings.Contains(base, "://") {
		if u, err := url.Parse(base); err == nil && u.Host != "" {
			// Extract only hostname, strip port if present
			// This ensures smart stream detection can add ports correctly
			hostname := u.Hostname()
			if hostname != "" {
				return hostname
			}
			return u.Host
		}
	}
	if idx := strings.Index(base, "/"); idx >= 0 {
		return base[:idx]
	}
	return base
}

func maskValue(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "***"
	}
	if len(v) <= 8 {
		return v[:2] + "***" + v[len(v)-2:]
	}
	return v[:4] + "***" + v[len(v)-3:]
}

// GetEPG retrieves EPG data for a specific service reference over specified days
func (c *Client) GetEPG(ctx context.Context, sRef string, days int) ([]EPGEvent, error) {
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Note: OpenWebIF /web/epgservice doesn't properly support endTime parameter
	// Using time=-1 returns all available EPG data (typically 7-14 days)
	// We rely on the receiver's EPG database having sufficient data

	// Try primary endpoint: /api/epgservice
	primaryURL := fmt.Sprintf("/api/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, err := c.fetchEPGFromURL(ctx, primaryURL)
	if err == nil && len(events) > 0 {
		return events, nil
	}

	// Log primary failure and try fallback
	c.log.Debug().
		Err(err).
		Str("sref", maskValue(sRef)).
		Str("endpoint", "api").
		Msg("primary EPG endpoint failed, trying fallback")

	// Fallback: /web/epgservice
	fallbackURL := fmt.Sprintf("/web/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, fallbackErr := c.fetchEPGFromURL(ctx, fallbackURL)
	if fallbackErr != nil {
		combinedErr := errors.Join(err, fallbackErr)
		return nil, fmt.Errorf("both EPG endpoints failed: %w", combinedErr)
	}

	return events, nil
}

// GetBouquetEPG fetches EPG events for an entire bouquet
func (c *Client) GetBouquetEPG(ctx context.Context, bouquetRef string, days int) ([]EPGEvent, error) {
	if bouquetRef == "" {
		return nil, fmt.Errorf("bouquet reference cannot be empty")
	}
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Use bouquet EPG endpoint
	epgURL := fmt.Sprintf("/api/epgbouquet?bRef=%s", url.QueryEscape(bouquetRef))

	events, err := c.fetchEPGFromURL(ctx, epgURL)
	if err != nil {
		return nil, fmt.Errorf("bouquet EPG request failed: %w", err)
	}

	return events, nil
}

// needsLatin1Conversion checks if data needs to be converted from Latin-1/ISO-8859-1 to UTF-8
func needsLatin1Conversion(data []byte, contentType string) bool {
	// If Content-Type explicitly mentions UTF-8, don't convert
	if strings.Contains(strings.ToLower(contentType), "utf-8") {
		return false
	}

	// If Content-Type explicitly mentions ISO-8859-1 or Latin-1, convert
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "iso-8859-1") || strings.Contains(ct, "latin1") {
		return true
	}

	// Heuristic: Check for invalid UTF-8 sequences that look like Latin-1
	// Look for byte patterns like 0xF6 (ö in Latin-1) that would be invalid in UTF-8
	for _, b := range data {
		// Bytes 0x80-0xFF in Latin-1 are single-byte characters
		// But in UTF-8, they must be part of multi-byte sequences
		if b >= 0x80 {
			// Check if this is a valid UTF-8 continuation
			if !utf8.Valid(data) {
				return true
			}
			break
		}
	}

	return false
}

// convertLatin1ToUTF8 converts Latin-1/ISO-8859-1 encoded bytes to UTF-8
func convertLatin1ToUTF8(latin1 []byte) []byte {
	// Allocate buffer with enough space (worst case: every byte becomes 2 bytes in UTF-8)
	buf := make([]byte, 0, len(latin1)*2)

	for _, b := range latin1 {
		if b < 0x80 {
			// ASCII range, copy directly
			buf = append(buf, b)
		} else {
			// Latin-1 byte 0x80-0xFF maps to Unicode U+0080-U+00FF
			// In UTF-8, these are encoded as two bytes: 110xxxxx 10xxxxxx
			buf = append(buf, 0xC0|(b>>6), 0x80|(b&0x3F))
		}
	}

	return buf
}

// GetServiceEPG fetches EPG events for a specific service reference
func (c *Client) GetServiceEPG(ctx context.Context, serviceRef string) ([]EPGEvent, error) {
	// API expects sRef parameter
	// e.g. /api/epgservice?sRef=1:0:19:132F:3EF:1:C00000:0:0:0:
	params := url.Values{}
	params.Set("sRef", serviceRef)
	urlPath := fmt.Sprintf("/api/epgservice?%s", params.Encode())

	return c.fetchEPGFromURL(ctx, urlPath)
}

func (c *Client) fetchEPGFromURL(ctx context.Context, urlPath string) ([]EPGEvent, error) {
	decorate := func(zc *zerolog.Context) {
		zc.Str("path", urlPath)
	}

	body, err := c.get(ctx, urlPath, "epg", decorate)
	if err != nil {
		return nil, fmt.Errorf("EPG request failed: %w", err)
	}

	// Check if response starts with JSON or XML
	trimmed := bytes.TrimLeft(body, " \t\n\r")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// If it starts with '<', it's XML (web endpoint)
	if trimmed[0] == '<' {
		return c.parseEPGXML(body)
	}

	// Otherwise try JSON (api endpoint)
	var epgResp EPGResponse
	if err := json.Unmarshal(body, &epgResp); err != nil {
		return nil, fmt.Errorf("parsing EPG response: %w", err)
	}

	// Note: Some OpenWebIF endpoints don't return a "result" field at all
	// so we check for events directly rather than validating Result

	// Filter out invalid events and unescape HTML entities
	validEvents := make([]EPGEvent, 0, len(epgResp.Events))
	for _, event := range epgResp.Events {
		if event.Title != "" && event.Begin > 0 {
			// Resolve fallbacks
			if event.Description == "" {
				event.Description = event.DescriptionFallback
			}
			if event.LongDesc == "" {
				event.LongDesc = event.LongDescFallback
			}

			// Sanitize strings: OpenWebIF often sends HTML entities (e.g. &#x27;) in JSON
			// We must unescape them here so the XMLTV generator doesn't double-escape them.
			event.Title = html.UnescapeString(event.Title)
			event.Description = html.UnescapeString(event.Description)
			event.LongDesc = html.UnescapeString(event.LongDesc)
			validEvents = append(validEvents, event)
		}
	}

	return validEvents, nil
}

// XML structures for /web/epgservice response
type xmlEPGResponse struct {
	XMLName xml.Name      `xml:"e2eventlist"`
	Events  []xmlEPGEvent `xml:"e2event"`
}

type xmlEPGEvent struct {
	ID              int    `xml:"e2eventid"`
	Title           string `xml:"e2eventtitle"`
	Description     string `xml:"e2eventdescription"`
	LongDescription string `xml:"e2eventdescriptionextended"`
	Start           int64  `xml:"e2eventstart"`
	Duration        int    `xml:"e2eventduration"`
	ServiceRef      string `xml:"e2eventservicereference"`
	Genre           string `xml:"e2eventgenre"`
}

func (c *Client) parseEPGXML(body []byte) ([]EPGEvent, error) {
	var xmlResp xmlEPGResponse
	if err := xml.Unmarshal(body, &xmlResp); err != nil {
		return nil, fmt.Errorf("parsing XML EPG response: %w", err)
	}

	// Convert XML events to EPGEvent format
	events := make([]EPGEvent, 0, len(xmlResp.Events))
	for _, xmlEvent := range xmlResp.Events {
		if xmlEvent.Title != "" && xmlEvent.Start > 0 {
			event := EPGEvent{
				ID:          xmlEvent.ID,
				Title:       html.UnescapeString(xmlEvent.Title),
				Description: html.UnescapeString(xmlEvent.Description),
				LongDesc:    html.UnescapeString(xmlEvent.LongDescription),
				Begin:       xmlEvent.Start,
				Duration:    int64(xmlEvent.Duration),
				SRef:        xmlEvent.ServiceRef,
			}
			events = append(events, event)
		}
	}

	return events, nil
}
