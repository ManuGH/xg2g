package enigma2

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Response is the generic wrapper for Enigma2 API responses.
type Response struct {
	Result    bool   `json:"result"`
	Message   string `json:"message"`
	ErrorCode int    `json:"error_code,omitempty"`
}

// CurrentInfo represents the response from /api/getcurrent.
type CurrentInfo struct {
	Result bool `json:"result"`
	Info   struct {
		ServiceReference string    `json:"ref"`
		Name             string    `json:"name"`
		Provider         string    `json:"provider"`
		VideoHeight      IntString `json:"video_height,omitempty"`
		VideoWidth       IntString `json:"video_width,omitempty"`
	} `json:"info"`
}

// Signal represents the response from /api/signal.
type Signal struct {
	Result bool      `json:"result"`
	Snr    IntString `json:"snr"`
	Agc    IntString `json:"agc"`
	Ber    IntString `json:"ber"`
	Locked bool      `json:"lock"`
}

// IntString handles JSON fields that might be numbers or strings.
type IntString int

func (i *IntString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*i = 0
			return nil
		}
		val, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid int string: %s", s)
		}
		*i = IntString(val)
		return nil
	}
	var val int
	if err := json.Unmarshal(b, &val); err != nil {
		return err
	}
	*i = IntString(val)
	return nil
}
