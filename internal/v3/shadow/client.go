package shadow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	v3api "github.com/ManuGH/xg2g/internal/v3/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
)

var (
	shadowIntentsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_shadow_intents_total",
			Help: "Total number of shadow intents fired to v3 API",
		},
		[]string{"result"},
	)
)

// Config holds configuration for the Shadow Client
type Config struct {
	Enabled   bool
	TargetURL string // e.g., "http://localhost:8080/api/v3/intents"
}

// Client is a dedicated HTTP client for firing shadow intents.
// It is designed to be fail-safe, non-blocking, and isolated from the main path.
type Client struct {
	httpClient *http.Client
	cfg        Config
	logger     zerolog.Logger
	queue      chan shadowJob
}

type shadowJob struct {
	ctx        context.Context
	serviceRef string
	profile    string
	clientIP   string
}

// New creates a new Shadow Client with a strict 50ms timeout.
func New(cfg Config) *Client {
	c := &Client{
		cfg: cfg,
		httpClient: &http.Client{
			// Strict hard timeout to prevent goroutine pile-up
			Timeout: 50 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   50 * time.Millisecond,
				ResponseHeaderTimeout: 50 * time.Millisecond,
				ExpectContinueTimeout: 50 * time.Millisecond,
				DisableKeepAlives:     false, // KeepAlive essential for latency
			},
		},
		logger: log.WithComponent("v3-shadow"),
		queue:  make(chan shadowJob, 100), // Buffer of 100 pending intents
	}
	c.startWorker()
	return c
}

func (c *Client) startWorker() {
	go func() {
		for job := range c.queue {
			// Create a fresh context for execution (detached from Fire's context)
			// We use a short timeout for the actual request
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			err := c.execute(ctx, job.serviceRef, job.profile, job.clientIP)
			cancel() // Always cancel

			if err != nil {
				shadowIntentsTotal.WithLabelValues("error").Inc()
				// Log at debug level to avoid noise in production
				c.logger.Debug().Err(err).Msg("shadow intent failed")
			} else {
				shadowIntentsTotal.WithLabelValues("sent").Inc()
			}
		}
	}()
}

// Fire (Fire-and-Forget) triggers a shadow intent asynchronously.
// It returns immediately. If the queue is full, it drops the intent.
func (c *Client) Fire(ctx context.Context, serviceRef, profile, clientIP string) {
	if !c.cfg.Enabled {
		shadowIntentsTotal.WithLabelValues("disabled").Inc()
		return
	}
	if c.cfg.TargetURL == "" {
		shadowIntentsTotal.WithLabelValues("skipped_config").Inc()
		return
	}

	select {
	case c.queue <- shadowJob{ctx: ctx, serviceRef: serviceRef, profile: profile, clientIP: clientIP}:
		// Enqueued
	default:
		// Queue full, drop
		shadowIntentsTotal.WithLabelValues("dropped").Inc()
	}
}

func (c *Client) execute(ctx context.Context, serviceRef, profile, clientIP string) error {
	// Idempotency Key Generation
	// Key = SHA256(ServiceRef + Profile + ClientIP + 30s_TimeBucket)
	// This prevents spamming the v3 API with the same "start" intent if the client retries rapidly.
	bucket := time.Now().Unix() / 30
	keyRaw := fmt.Sprintf("%s|%s|%s|%d", serviceRef, profile, clientIP, bucket)
	hash := sha256.Sum256([]byte(keyRaw))
	idempotencyKey := hex.EncodeToString(hash[:])

	// Use v3api.IntentRequest to match API contract
	intent := v3api.IntentRequest{
		ServiceRef:     serviceRef,
		ProfileID:      profile,
		IdempotencyKey: idempotencyKey,
		Params: map[string]string{
			"shadow":    "true",
			"client_ip": clientIP,
			"source":    "v2_proxy",
		},
	}

	payload, err := json.Marshal(intent)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.TargetURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", idempotencyKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("shadow api returned %d", resp.StatusCode)
	}

	return nil
}
