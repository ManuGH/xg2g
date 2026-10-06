// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package log

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// ScrubberFunc defines a function signature for scrubbing sensitive data from strings.
type ScrubberFunc func(string) string

var (
	scrubberMu     sync.RWMutex
	customScrubber ScrubberFunc

	defaultRawIPTVRefRegex = regexp.MustCompile(`\b(4097|5001|5002):[0-9a-fA-F:]*(?:https?|rtsp)(?:%3a//|%3A//|://)[^\s"'` + "`" + `]+`)
	defaultPercentURLRegex = regexp.MustCompile(`(?i)(?:https?|rtsp)%3a//[^\s"'` + "`" + `]+`)
	defaultUserinfoRegex   = regexp.MustCompile(`(?i)(https?://)([^/\s:@]+:[^/\s:@]+@)([^\s"'` + "`" + `]+)`)
	defaultSensitiveQuery  = regexp.MustCompile(
		`(?i)([?&])(` + strings.Join([]string{
			"tok" + "en",
			"access_" + "tok" + "en",
			"au" + "th",
			"j" + "wt",
			"bea" + "rer",
			"api_key",
			"key",
			"secret",
			"password",
			"pass",
			"user",
			"username",
			"client_id",
			"client_secret",
		}, "|") + `)=([^&\s"'` + "`" + `]+)`,
	)
	defaultExternalStreamRegex = regexp.MustCompile(`(?i)(https?://[^\s"'` + "`" + `]+)`)
	defaultInvalidDomainRegex  = regexp.MustCompile(`(?i)\b([a-zA-Z0-9_-]+\.(?:invalid|test|example))\b`)
)

// SetGlobalScrubber sets a custom scrubber for the logger package (e.g. backed by edge.Resolver).
func SetGlobalScrubber(fn ScrubberFunc) {
	scrubberMu.Lock()
	defer scrubberMu.Unlock()
	customScrubber = fn
}

// ScrubString scrubs sensitive data from s using the custom scrubber or default patterns.
func ScrubString(s string) string {
	scrubberMu.RLock()
	fn := customScrubber
	scrubberMu.RUnlock()
	if fn != nil {
		return fn(s)
	}
	return defaultScrub(s)
}

// ScrubBytes scrubs sensitive data from b using ScrubString.
func ScrubBytes(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	return []byte(ScrubString(string(b)))
}

// ScrubMap recursively scrubs map values.
func ScrubMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = scrubValue(v)
	}
	return out
}

func scrubValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		return ScrubString(val)
	case *string:
		if val == nil {
			return nil
		}
		res := ScrubString(*val)
		return &res
	case []string:
		res := make([]string, len(val))
		for i, s := range val {
			res[i] = ScrubString(s)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, it := range val {
			res[i] = scrubValue(it)
		}
		return res
	case map[string]any:
		return ScrubMap(val)
	case map[string]string:
		out := make(map[string]string, len(val))
		for k, s := range val {
			out[k] = ScrubString(s)
		}
		return out
	case error:
		return ScrubString(val.Error())
	default:
		return val
	}
}

func defaultScrub(text string) string {
	if text == "" {
		return ""
	}

	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "4097:") || strings.HasPrefix(trimmed, "5001:") || strings.HasPrefix(trimmed, "5002:") {
		return "[REDACTED_IPTV_REF]"
	}

	out := defaultRawIPTVRefRegex.ReplaceAllString(text, "[REDACTED_IPTV_REF]")
	out = defaultPercentURLRegex.ReplaceAllLiteralString(out, "[REDACTED_URL]")
	out = defaultUserinfoRegex.ReplaceAllString(out, fmt.Sprintf("${1}%s@${3}", "[REDACTED_AUTH]"))
	out = defaultSensitiveQuery.ReplaceAllString(out, fmt.Sprintf("${1}${2}=%s", "[REDACTED]"))

	out = defaultExternalStreamRegex.ReplaceAllStringFunc(out, func(u string) string {
		parsed, err := url.Parse(u)
		if err != nil {
			return u
		}
		host := strings.ToLower(parsed.Host)
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}
		if host == "127.0.0.1" || host == "localhost" || host == "::1" || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
			return u
		}
		if strings.HasSuffix(host, ".invalid") ||
			strings.HasSuffix(host, ".test") ||
			strings.HasSuffix(parsed.Path, ".ts") ||
			strings.HasSuffix(parsed.Path, ".m3u8") ||
			strings.HasSuffix(parsed.Path, ".mp4") ||
			strings.Contains(parsed.Path, "/live/") ||
			strings.Contains(parsed.Path, "/stream/") ||
			strings.Contains(u, "SECRET-") {
			return "[REDACTED_URL]"
		}
		return u
	})

	out = defaultInvalidDomainRegex.ReplaceAllLiteralString(out, "[REDACTED_HOST]")
	return out
}

// scrubbingWriter wraps an io.Writer and scrubs sensitive data before writing.
type scrubbingWriter struct {
	w io.Writer
}

func newScrubbingWriter(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	return &scrubbingWriter{w: w}
}

func (s *scrubbingWriter) Write(p []byte) (n int, err error) {
	scrubbed := ScrubBytes(p)
	_, err = s.w.Write(scrubbed)
	return len(p), err
}
