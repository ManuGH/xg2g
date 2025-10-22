// SPDX-License-Identifier: MIT
package types

import (
	"encoding/json"
	"fmt"
)

// StreamState represents the current state of a media stream.
type StreamState string

// Stream state constants define all possible states of a media stream.
const (
	// StreamStateIdle indicates no stream is active.
	StreamStateIdle StreamState = "idle"

	// StreamStateConnecting indicates the stream is establishing connection.
	StreamStateConnecting StreamState = "connecting"

	// StreamStateBuffering indicates the stream is buffering data.
	StreamStateBuffering StreamState = "buffering"

	// StreamStateStreaming indicates the stream is actively transmitting.
	StreamStateStreaming StreamState = "streaming"

	// StreamStateTranscoding indicates the stream is being transcoded.
	StreamStateTranscoding StreamState = "transcoding"

	// StreamStateError indicates the stream encountered an error.
	StreamStateError StreamState = "error"

	// StreamStateStopped indicates the stream was stopped.
	StreamStateStopped StreamState = "stopped"
)

// String implements fmt.Stringer.
func (s StreamState) String() string {
	return string(s)
}

// IsValid checks whether the stream state is valid.
func (s StreamState) IsValid() bool {
	switch s {
	case StreamStateIdle, StreamStateConnecting, StreamStateBuffering,
		StreamStateStreaming, StreamStateTranscoding, StreamStateError, StreamStateStopped:
		return true
	default:
		return false
	}
}

// IsActive checks whether the stream is in an active state.
func (s StreamState) IsActive() bool {
	switch s {
	case StreamStateConnecting, StreamStateBuffering, StreamStateStreaming, StreamStateTranscoding:
		return true
	default:
		return false
	}
}

// MarshalJSON implements json.Marshaler.
func (s StreamState) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *StreamState) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	state := StreamState(str)
	if !state.IsValid() {
		return fmt.Errorf("invalid stream state: %q", str)
	}

	*s = state
	return nil
}

// ParseStreamState parses a string into a StreamState.
func ParseStreamState(s string) (StreamState, error) {
	state := StreamState(s)
	if !state.IsValid() {
		return "", fmt.Errorf("invalid stream state: %q", s)
	}
	return state, nil
}

// AllStreamStates returns all defined stream states.
func AllStreamStates() []StreamState {
	return []StreamState{
		StreamStateIdle,
		StreamStateConnecting,
		StreamStateBuffering,
		StreamStateStreaming,
		StreamStateTranscoding,
		StreamStateError,
		StreamStateStopped,
	}
}
