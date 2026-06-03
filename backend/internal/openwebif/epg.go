package openwebif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"html"
	"net/url"
	"strings"
	"unicode/utf8"
)

// GetEPG retrieves EPG data for a specific service reference over specified days
func (c *Client) GetEPG(ctx context.Context, sRef string, days int) ([]EPGEvent, error) {
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Note: OpenWebIF /web/epgservice doesn't properly support endTime parameter
	// Using time=-1 returns all available EPG data (typically 7-14 days)
	// We rely on the receiver's EPG database having sufficient data

	// Try primary endpoint: /api/epgservice
	primaryURL := fmt.Sprintf("/api/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, err := c.fetchEPGFromURL(ctx, primaryURL)
	if err == nil && len(events) > 0 {
		return events, nil
	}

	// Log primary failure and try fallback
	c.log.Debug().
		Err(err).
		Str("sref", maskValue(sRef)).
		Str("endpoint", "api").
		Msg("primary EPG endpoint failed, trying fallback")

	// Fallback: /web/epgservice
	fallbackURL := fmt.Sprintf("/web/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, fallbackErr := c.fetchEPGFromURL(ctx, fallbackURL)
	if fallbackErr != nil {
		combinedErr := errors.Join(err, fallbackErr)
		return nil, fmt.Errorf("both EPG endpoints failed: %w", combinedErr)
	}

	return events, nil
}

// GetBouquetEPG fetches EPG events for an entire bouquet
func (c *Client) GetBouquetEPG(ctx context.Context, bouquetRef string, days int) ([]EPGEvent, error) {
	if bouquetRef == "" {
		return nil, fmt.Errorf("bouquet reference cannot be empty")
	}
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Use bouquet EPG endpoint
	epgURL := fmt.Sprintf("/api/epgbouquet?bRef=%s", url.QueryEscape(bouquetRef))

	events, err := c.fetchEPGFromURL(ctx, epgURL)
	if err != nil {
		return nil, fmt.Errorf("bouquet EPG request failed: %w", err)
	}

	return events, nil
}

// needsLatin1Conversion checks if data needs to be converted from Latin-1/ISO-8859-1 to UTF-8
func needsLatin1Conversion(data []byte, contentType string) bool {
	// If Content-Type explicitly mentions UTF-8, don't convert
	if strings.Contains(strings.ToLower(contentType), "utf-8") {
		return false
	}

	// If Content-Type explicitly mentions ISO-8859-1 or Latin-1, convert
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "iso-8859-1") || strings.Contains(ct, "latin1") {
		return true
	}

	// Heuristic: Check for invalid UTF-8 sequences that look like Latin-1
	// Look for byte patterns like 0xF6 (ö in Latin-1) that would be invalid in UTF-8
	for _, b := range data {
		// Bytes 0x80-0xFF in Latin-1 are single-byte characters
		// But in UTF-8, they must be part of multi-byte sequences
		if b >= 0x80 {
			// Check if this is a valid UTF-8 continuation
			if !utf8.Valid(data) {
				return true
			}
			break
		}
	}

	return false
}

// convertLatin1ToUTF8 converts Latin-1/ISO-8859-1 encoded bytes to UTF-8
func convertLatin1ToUTF8(latin1 []byte) []byte {
	// Allocate buffer with enough space (worst case: every byte becomes 2 bytes in UTF-8)
	buf := make([]byte, 0, len(latin1)*2)

	for _, b := range latin1 {
		if b < 0x80 {
			// ASCII range, copy directly
			buf = append(buf, b)
		} else {
			// Latin-1 byte 0x80-0xFF maps to Unicode U+0080-U+00FF
			// In UTF-8, these are encoded as two bytes: 110xxxxx 10xxxxxx
			buf = append(buf, 0xC0|(b>>6), 0x80|(b&0x3F))
		}
	}

	return buf
}

// GetServiceEPG fetches EPG events for a specific service reference
func (c *Client) GetServiceEPG(ctx context.Context, serviceRef string) ([]EPGEvent, error) {
	// API expects sRef parameter
	// e.g. /api/epgservice?sRef=1:0:19:132F:3EF:1:C00000:0:0:0:
	params := url.Values{}
	params.Set("sRef", serviceRef)
	urlPath := fmt.Sprintf("/api/epgservice?%s", params.Encode())

	return c.fetchEPGFromURL(ctx, urlPath)
}

func (c *Client) fetchEPGFromURL(ctx context.Context, urlPath string) ([]EPGEvent, error) {
	decorate := func(zc *zerolog.Context) {
		zc.Str("path", urlPath)
	}

	body, err := c.get(ctx, urlPath, "epg", decorate)
	if err != nil {
		return nil, fmt.Errorf("EPG request failed: %w", err)
	}

	// Check if response starts with JSON or XML
	trimmed := bytes.TrimLeft(body, " \t\n\r")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// If it starts with '<', it's XML (web endpoint)
	if trimmed[0] == '<' {
		return c.parseEPGXML(body)
	}

	// Otherwise try JSON (api endpoint)
	var epgResp EPGResponse
	if err := json.Unmarshal(body, &epgResp); err != nil {
		return nil, fmt.Errorf("parsing EPG response: %w", err)
	}

	// Note: Some OpenWebIF endpoints don't return a "result" field at all
	// so we check for events directly rather than validating Result

	// Filter out invalid events and unescape HTML entities
	validEvents := make([]EPGEvent, 0, len(epgResp.Events))
	for _, event := range epgResp.Events {
		if event.Title != "" && event.Begin > 0 {
			// Resolve fallbacks
			if event.Description == "" {
				event.Description = event.DescriptionFallback
			}
			if event.LongDesc == "" {
				event.LongDesc = event.LongDescFallback
			}

			// Sanitize strings: OpenWebIF often sends HTML entities (e.g. &#x27;) in JSON
			// We must unescape them here so the XMLTV generator doesn't double-escape them.
			event.Title = html.UnescapeString(event.Title)
			event.Description = html.UnescapeString(event.Description)
			event.LongDesc = html.UnescapeString(event.LongDesc)
			validEvents = append(validEvents, event)
		}
	}

	return validEvents, nil
}
