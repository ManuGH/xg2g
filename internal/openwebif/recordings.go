// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
)

// StringOrNumberString handles JSON fields that can be "123" or 123.
type StringOrNumberString string

func (s *StringOrNumberString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*s = ""
		return nil
	}

	// If it's a JSON string: "12345"
	if b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = StringOrNumberString(v)
		return nil
	}

	// Otherwise treat as number: 12345 (or 12345.0, etc.)
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return fmt.Errorf("filesize: invalid json value: %s", string(b))
	}

	// Prefer integer string if possible
	if i, err := n.Int64(); err == nil {
		*s = StringOrNumberString(strconv.FormatInt(i, 10))
		return nil
	}

	// Fallback: keep raw number text
	*s = StringOrNumberString(n.String())
	return nil
}

// IntOrStringInt64 handles JSON fields that can be "123" or 123.
type IntOrStringInt64 int64

func (v *IntOrStringInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) || bytes.Equal(b, []byte(`""`)) {
		*v = 0
		return nil
	}
	// If it's a JSON string: "12345"
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*v = 0
			return nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("recordingtime: invalid string %q", s)
		}
		*v = IntOrStringInt64(i)
		return nil
	}
	// Otherwise treat as number
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return fmt.Errorf("recordingtime: invalid json value: %s", string(b))
	}
	i, err := n.Int64()
	if err != nil {
		return fmt.Errorf("recordingtime: not int64: %s", n.String())
	}
	*v = IntOrStringInt64(i)
	return nil
}

// MovieLocation represents a directory bookmark
type MovieLocation struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// BookmarkList handles varied bookmark formats ([], [strings], [objects], "", {})
type BookmarkList []MovieLocation

func (bl *BookmarkList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) || bytes.Equal(b, []byte(`""`)) {
		*bl = []MovieLocation{}
		return nil
	}

	// Case 1: Array of Objects (Standard)
	var objs []MovieLocation
	if err := json.Unmarshal(b, &objs); err == nil {
		*bl = BookmarkList(objs)
		return nil
	}

	// Case 2: Array of Strings (Legacy)
	var strs []string
	if err := json.Unmarshal(b, &strs); err == nil {
		list := make([]MovieLocation, len(strs))
		for i, s := range strs {
			list[i] = MovieLocation{
				Path: s,
				Name: filepath.Base(s),
			}
		}
		*bl = BookmarkList(list)
		return nil
	}

	// Case 3: Single Object (Edge Case)
	var obj MovieLocation
	if err := json.Unmarshal(b, &obj); err == nil && (obj.Path != "" || obj.Name != "") {
		*bl = BookmarkList{obj}
		return nil
	}

	return fmt.Errorf("bookmarks: invalid json format")
}

// Movie represents a recording in the movie list
type Movie struct {
	ServiceRef          string               `json:"serviceref"`
	Title               string               `json:"eventname"`
	Description         string               `json:"description"`
	ExtendedDescription string               `json:"extended_description"` // Full plot summary
	Length              string               `json:"length"`               // OWI typically returns string like "90 min"
	Filesize            StringOrNumberString `json:"filesize"`
	Filename            string               `json:"filename"`
	Begin               IntOrStringInt64     `json:"recordingtime"`
}

// MovieList represents the response from /api/movielist
type MovieList struct {
	Movies    []Movie      `json:"movies"`
	Bookmarks BookmarkList `json:"bookmarks"`
	Directory string       `json:"directory"`
	Result    bool         `json:"result"`
}

// GetRecordings retrieves the list of recordings from the receiver.
// dirname can be empty to use default.
func (c *Client) GetRecordings(ctx context.Context, dirname string) (*MovieList, error) {
	params := url.Values{}
	if dirname != "" {
		params.Set("dirname", dirname)
	}

	path := "/api/movielist"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	body, err := c.get(ctx, path, "movielist", nil)
	if err != nil {
		return nil, err
	}

	var list MovieList
	if err := json.Unmarshal(body, &list); err != nil {
		c.loggerFor(ctx).Error().Err(err).Str("event", "openwebif.decode").Str("operation", "movielist").Msg("failed to decode movielist")
		return nil, fmt.Errorf("failed to decode movielist: %w", err)
	}

	if !list.Result {
		// Log warning but proceed, empty dirs might return result=false
		c.loggerFor(ctx).Warn().Str("dirname", dirname).Msg("movielist result=false")
	}

	return &list, nil
}

// MovieDeleteResponse represents the response from /api/moviedelete
type MovieDeleteResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

// DeleteMovie deletes a recording by its Service Reference.
func (c *Client) DeleteMovie(ctx context.Context, sRef string) error {
	params := url.Values{}
	params.Set("sRef", sRef)

	path := "/api/moviedelete?" + params.Encode()
	body, err := c.get(ctx, path, "moviedelete", nil)
	if err != nil {
		return err
	}

	var resp MovieDeleteResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// Some versions might just return simple JSON or even bool?
		// Fallback check? usually it matches.
		c.loggerFor(ctx).Error().Err(err).Str("body", string(body)).Msg("failed to decode moviedelete response")
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.Result {
		return fmt.Errorf("receiver returned failure: %s", resp.Message)
	}

	return nil
}
