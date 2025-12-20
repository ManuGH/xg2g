// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// StreamSession represents an active streaming session on the proxy.
type StreamSession struct {
	ID          string    `json:"id"`
	ClientIP    string    `json:"client_ip"`
	UserAgent   string    `json:"user_agent"`
	ChannelName string    `json:"channel_name"`
	ServiceRef  string    `json:"service_ref"`
	StartedAt   time.Time `json:"started_at"`
	BytesSent   int64     `json:"bytes_sent"`
	State       string    `json:"state"` // "active", "idle"

	lastWrite int64              // atomic unix nano
	cancel    context.CancelFunc // To terminate session
}

func (s *StreamSession) UpdateActivity(bytes int) {
	atomic.StoreInt64(&s.lastWrite, time.Now().UnixNano())
	atomic.AddInt64(&s.BytesSent, int64(bytes))
}

func (s *StreamSession) LastActivity() time.Time {
	val := atomic.LoadInt64(&s.lastWrite)
	if val == 0 {
		return s.StartedAt
	}
	return time.Unix(0, val)
}

// Registry manages active stream sessions.
type Registry struct {
	sessions sync.Map // map[string]*StreamSession
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(req *http.Request, channelName, serviceRef string, cancel context.CancelFunc) *StreamSession {
	id := uuid.New().String()

	// Normalize IP (strip port and IPv6 mapping if needed)
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}

	// Strip ::ffff: prefix
	if len(host) > 7 && host[:7] == "::ffff:" {
		host = host[7:]
	}

	session := &StreamSession{
		ID:          id,
		ClientIP:    host,
		UserAgent:   req.UserAgent(),
		ChannelName: channelName,
		ServiceRef:  serviceRef,
		StartedAt:   time.Now(),
		State:       "active",
		cancel:      cancel,
		lastWrite:   time.Now().UnixNano(),
	}

	r.sessions.Store(id, session)
	return session
}

func (r *Registry) Unregister(id string) {
	r.sessions.Delete(id)
}

func (r *Registry) GetSession(id string) *StreamSession {
	if v, ok := r.sessions.Load(id); ok {
		return v.(*StreamSession)
	}
	return nil
}

func (r *Registry) Terminate(id string) bool {
	if v, ok := r.sessions.Load(id); ok {
		sess := v.(*StreamSession)
		if sess.cancel != nil {
			sess.cancel()
		}
		r.sessions.Delete(id)
		return true
	}
	return false
}

func (r *Registry) List() []*StreamSession {
	var list []*StreamSession
	r.sessions.Range(func(key, value any) bool {
		list = append(list, value.(*StreamSession))
		return true
	})
	return list
}

// MarshalJSON for StreamSession to avoid marshalling internal fields
func (s *StreamSession) MarshalJSON() ([]byte, error) {
	type Alias StreamSession
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}
