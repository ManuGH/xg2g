package openwebif

import (
	"encoding/xml"
	"errors"
	"fmt"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/resilience"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

		// Memory (live OpenWebIF returns total/available plus a friendly summary)
		Mem1 string `json:"mem1"` // TOTAL memory, e.g. "757484 kB"
		Mem2 string `json:"mem2"` // AVAILABLE/FREE memory, e.g. "548180 kB"
		Mem3 string `json:"mem3"` // Friendly summary, e.g. "548180 kB frei / 757484 kB insgesamt"

		// Software versions
		OEVer               string `json:"oever"`               // OE-Alliance version
		ImageVer            string `json:"imagever"`            // OpenATV version
		ImageDistro         string `json:"imagedistro"`         // "openatv"
		FriendlyImageDistro string `json:"friendlyimagedistro"` // "OpenATV"
		EnigmaVer           string `json:"enigmaver"`           // Enigma2 date
		KernelVer           string `json:"kernelver"`           // Kernel version
		DriverDate          string `json:"driverdate"`          // Driver date
		WebIFVer            string `json:"webifver"`            // OWIF version
		FPVersion           any    `json:"fp_version"`          // Front panel version (often null)

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
		Shares []any              `json:"shares"` // Network shares (often empty)

		// Additional fields
		Streams any `json:"streams"` // Active streams
	} `json:"info"`
	Service any `json:"service"` // Current service info
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
	Name        string `json:"name"`        // "eth0"
	FriendlyNIC string `json:"friendlynic"` // "Broadcom Gigabit Ethernet"
	LinkSpeed   string `json:"linkspeed"`   // "1 GBit/s"
	MAC         string `json:"mac"`         // "00:1d:ec:0f:e3:ed"
	DHCP        bool   `json:"dhcp"`        // true if DHCP enabled
	IP          string `json:"ip"`          // IPv4 address
	Mask        string `json:"mask"`        // Netmask
	Gateway     string `json:"gw"`          // Gateway
	IPv6        string `json:"ipv6"`        // IPv6 address
	IPMethod    string `json:"ipmethod"`    // IP method
	IPv4Method  string `json:"ipv4method"`  // IPv4 method
	V4Prefix    int    `json:"v4prefix"`    // IPv4 prefix
	FirstPublic any    `json:"firstpublic"` // First public IP (often null)
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

// StatusInfo represents the receiver status from /api/statusinfo
type StatusInfo struct {
	Result      bool   `json:"result"`
	InStandby   string `json:"inStandby"`   // "true" or "false"
	IsRecording string `json:"isRecording"` // "true" or "false"
	IsStreaming string `json:"isStreaming"` // "true" or "false"
	ServiceName string `json:"currservice_name"`
	ServiceRef  string `json:"currservice_serviceref"`
}

// CurrentInfo represents the detailed service info from /api/getcurrent
type CurrentInfo struct {
	Result bool `json:"result"`
	Info   struct {
		ServiceRef  string `json:"serviceref"`
		ServiceName string `json:"name"`
		VideoPID    any    `json:"vpid,omitempty"`   // Can be int or "N/A"
		AudioPID    any    `json:"apid,omitempty"`   // Can be int or "N/A"
		PMTPID      any    `json:"pmtpid,omitempty"` // Can be int or "N/A"
		PCRPID      any    `json:"pcrpid,omitempty"` // Can be int or "N/A"
		TXT         any    `json:"txtpid,omitempty"` // Can be int or "N/A"
		TSID        any    `json:"tsid,omitempty"`   // Can be int or "N/A"
		ONID        any    `json:"onid,omitempty"`   // Can be int or "N/A"
		SID         any    `json:"sid,omitempty"`    // Can be int or "N/A"
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
	if before, _, ok := strings.Cut(base, "/"); ok {
		return before
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
