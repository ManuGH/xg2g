package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when an object is not in the store.
var ErrNotFound = errors.New("object not found")

// StreamID uniquely identifies a media stream across the system.
type StreamID string

// ObjectKind defines the type of HLS/CMAF object being stored.
type ObjectKind uint8

const (
	ObjectInit ObjectKind = iota
	ObjectSegment
	ObjectPlaylist
)

// Object represents a single media artifact (Init Segment, Media Segment, or Playlist).
type Object struct {
	Name        string
	Kind        ObjectKind
	ContentType string
	Data        []byte
	PublishedAt time.Time
}

// ShadowStore defines the interface for the passive RAM ringbuffer.
type ShadowStore interface {
	Publish(ctx context.Context, streamID StreamID, object Object) error
	Delete(ctx context.Context, streamID StreamID, name string) error
	DeleteStream(ctx context.Context, streamID StreamID) error
	Get(ctx context.Context, streamID StreamID, name string) (Object, error)
}
