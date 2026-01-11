package stub

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/google/uuid"
)

type Adapter struct {
	mu     sync.Mutex
	active map[ports.RunHandle]bool
}

func NewAdapter() *Adapter {
	return &Adapter{
		active: make(map[ports.RunHandle]bool),
	}
}

func (a *Adapter) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	handle := ports.RunHandle(fmt.Sprintf("stub-%s-%s", spec.SessionID, uuid.New().String()))
	a.active[handle] = true
	return handle, nil
}

func (a *Adapter) Stop(ctx context.Context, handle ports.RunHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.active, handle)
	return nil
}

func (a *Adapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active[handle] {
		return ports.HealthStatus{Healthy: true, Message: "stub active", LastCheck: time.Now()}
	}
	return ports.HealthStatus{Healthy: false, Message: "stub missing", LastCheck: time.Now()}
}
