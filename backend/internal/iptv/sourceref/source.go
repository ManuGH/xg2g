package sourceref

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

type sourceData struct {
	id           ID
	serviceType  string // "4097", "5001", "5002"
	serviceName  string
	rawURL       func() string // Unredacted stream URL protected from reflection
	rawRef       func() string // Complete original Enigma2 service reference, protected from reflection
	canonicalURL func() string // Canonical URL protected from reflection
}

// Source represents an opaque, server-side IPTV stream source.
// All string, formatting, logging, and serialization methods are strictly redacted
// to guarantee that the underlying stream URL and credentials can never be leaked.
// All internal state is stored behind an unexported pointer to ensure that reflection
// across unexported fields in holding structs prints pointer addresses rather than sensitive fields.
type Source struct {
	d *sourceData
}

// ID returns the opaque, HMAC-derived identifier.
func (s Source) ID() ID {
	if s.d == nil {
		return ""
	}
	return s.d.id
}

// ServiceType returns the Enigma2 service type ("4097", "5001", "5002").
func (s Source) ServiceType() string {
	if s.d == nil {
		return ""
	}
	return s.d.serviceType
}

// ServiceName returns the optional service/channel name extracted from the reference.
func (s Source) ServiceName() string {
	if s.d == nil {
		return ""
	}
	return s.d.serviceName
}

// IsZero reports whether the source is uninitialized.
func (s Source) IsZero() bool {
	return s.d == nil || s.d.id == ""
}

// RevealURL returns the unredacted target stream URL.
// This method is for in-process use only (e.g. private ingest/relay).
// Callers must NEVER log, serialize, or expose this URL to clients.
func (s Source) RevealURL() string {
	if s.d == nil || s.d.rawURL == nil {
		return ""
	}
	return s.d.rawURL()
}

// RawRef returns the complete original Enigma2 service reference
// (e.g. "4097:0:1:...:<encoded url>:<name>"), as needed for OpenWebIF calls
// and internal lookups that require the wire format.
// This method is for in-process use only. Callers must NEVER log, serialize,
// or expose this value to clients: it embeds the provider URL.
func (s Source) RawRef() string {
	if s.d == nil || s.d.rawRef == nil {
		return ""
	}
	return s.d.rawRef()
}

// canonicalURL returns the internal canonical URL used for ID generation.
func (s Source) canonicalURL() string {
	if s.d == nil || s.d.canonicalURL == nil {
		return ""
	}
	return s.d.canonicalURL()
}

// String implements fmt.Stringer and returns the opaque ID.
func (s Source) String() string {
	if s.IsZero() {
		return "sourceref.Source(empty)"
	}
	return s.d.id.String()
}

// GoString implements fmt.GoStringer and returns a redacted representation.
func (s Source) GoString() string {
	if s.IsZero() {
		return "sourceref.Source{}"
	}
	return fmt.Sprintf("sourceref.Source{ID: %q, ServiceType: %q}", s.d.id.String(), s.d.serviceType)
}

// Format implements fmt.Formatter to prevent %v, %+v, %#v from leaking private fields.
func (s Source) Format(f fmt.State, verb rune) {
	if s.IsZero() {
		_, _ = fmt.Fprint(f, "sourceref.Source{}")
		return
	}
	switch verb {
	case 's':
		_, _ = fmt.Fprint(f, s.d.id.String())
	case 'q':
		_, _ = fmt.Fprintf(f, "%q", s.d.id.String())
	case 'v':
		if f.Flag('#') {
			_, _ = fmt.Fprintf(f, "sourceref.Source{ID: %q, ServiceType: %q}", s.d.id.String(), s.d.serviceType)
		} else if f.Flag('+') {
			_, _ = fmt.Fprintf(f, "sourceref.Source{ID:%s ServiceType:%s}", s.d.id.String(), s.d.serviceType)
		} else {
			_, _ = fmt.Fprint(f, s.d.id.String())
		}
	default:
		_, _ = fmt.Fprint(f, s.d.id.String())
	}
}

// MarshalJSON implements json.Marshaler and marshals only non-sensitive metadata.
func (s Source) MarshalJSON() ([]byte, error) {
	if s.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(struct {
		ID          string `json:"id"`
		ServiceType string `json:"serviceType,omitempty"`
		ServiceName string `json:"serviceName,omitempty"`
	}{
		ID:          s.d.id.String(),
		ServiceType: s.d.serviceType,
		ServiceName: s.d.serviceName,
	})
}

// MarshalText implements encoding.TextMarshaler and emits the opaque ID.
func (s Source) MarshalText() ([]byte, error) {
	if s.IsZero() {
		return []byte{}, nil
	}
	return []byte(s.d.id.String()), nil
}

// LogValue implements slog.LogValuer to ensure structured logging never emits the URL.
func (s Source) LogValue() slog.Value {
	if s.IsZero() {
		return slog.GroupValue()
	}
	return slog.GroupValue(
		slog.String("id", s.d.id.String()),
		slog.String("service_type", s.d.serviceType),
	)
}
