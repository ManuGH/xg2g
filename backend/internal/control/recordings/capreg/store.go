package capreg

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

type Store interface {
	RememberHost(ctx context.Context, snapshot HostSnapshot) error
	RememberDevice(ctx context.Context, snapshot DeviceSnapshot) error
	RememberSource(ctx context.Context, snapshot SourceSnapshot) error
	LookupCapabilities(ctx context.Context, identity DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error)
	LookupDecisionObservation(ctx context.Context, requestID string) (PlaybackObservation, bool, error)
	RecordObservation(ctx context.Context, observation PlaybackObservation) error
}

type DeviceIdentity struct {
	ClientFamily     string
	ClientCapsSource string
	DeviceType       string
	DeviceContext    *capabilities.DeviceContext
}

type DeviceSnapshot struct {
	Identity     DeviceIdentity
	Capabilities capabilities.PlaybackCapabilities
	Network      *capabilities.NetworkContext
	UpdatedAt    time.Time
}

type HostIdentity struct {
	Hostname     string
	OSName       string
	OSVersion    string
	Architecture string
}

func NewStore(backend, storagePath string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "capability_registry.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unknown capability registry backend: %s (supported: sqlite, memory)", backend)
	}
}
