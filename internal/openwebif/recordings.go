package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Movie represents a recording in the movie list
type Movie struct {
	ServiceRef  string      `json:"serviceref"`
	Title       string      `json:"eventname"`
	Description string      `json:"description"`
	Length      string      `json:"length"` // OWI typically returns string like "90 min"
	Filesize    string      `json:"filesize"`
	Filename    string      `json:"filename"`
	Begin       interface{} `json:"recordingtime"` // int or string
}

// MovieLocation represents a directory bookmark
type MovieLocation struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// MovieList represents the response from /api/movielist
type MovieList struct {
	Movies    []Movie         `json:"movies"`
	Bookmarks []MovieLocation `json:"bookmarks"`
	Directory string          `json:"directory"`
	Result    bool            `json:"result"`
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
