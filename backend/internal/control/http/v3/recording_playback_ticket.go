package v3

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/household"
)

// recordingIDFromMediaPath deliberately excludes stream-info, resume and API
// actions. A recording ticket is a GET/HEAD credential for its media only.
func recordingIDFromMediaPath(mediaPath string) string {
	const prefix = "/api/v3/recordings/"
	if !strings.HasPrefix(mediaPath, prefix) {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(mediaPath, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return ""
	}
	switch parts[1] {
	case "stream.mp4", "playlist.m3u8", "timeshift.m3u8":
		return parts[0]
	}
	if v3recordings.IsAllowedVideoSegment(parts[1]) {
		return parts[0]
	}
	return ""
}

func (s *Server) ticketRecordingPlaybackInfo(r *http.Request, recordingID string, info *v3playbackinfo.PlaybackInfo) error {
	if info == nil || info.Url == nil || *info.Url == "" || info.Mode == v3playbackinfo.PlaybackInfoModeDenied {
		return nil
	}
	if !s.playbackTicketRequestAllowed(r) {
		return errors.New("recording ticket requires HTTPS or a local connection")
	}
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		return errors.New("recording ticket requires an authenticated principal")
	}
	profile := s.currentHouseholdProfile(r.Context())
	resource := playbackTicket{recordingID: recordingID, principal: principal.ID, profile: &profile}
	ticket, err := s.playbackTicketStoreOrDefault().issueResource(resource, time.Now().UTC())
	if err != nil {
		return err
	}
	addTicket := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || recordingIDFromMediaPath(parsed.Path) != recordingID {
			return raw
		}
		q := parsed.Query()
		q.Set("ticket", ticket)
		parsed.RawQuery = q.Encode()
		return parsed.String()
	}
	output := addTicket(*info.Url)
	info.Url = &output
	if info.Decision != nil {
		info.Decision.SelectedOutputUrl = addTicket(info.Decision.SelectedOutputUrl)
		for i := range info.Decision.Outputs {
			info.Decision.Outputs[i].Url = addTicket(info.Decision.Outputs[i].Url)
		}
	}
	return nil
}

type recordingTicketProfileKey struct{}

func (s *Server) recordingTicketContext(r *http.Request) context.Context {
	ctx := r.Context()
	ticket, ok := s.playbackTicketStoreOrDefault().resolve(extractPlaybackTicket(r), time.Now().UTC())
	if ok && ticket.recordingID != "" && ticket.profile != nil {
		profile := household.CloneProfile(*ticket.profile)
		ctx = household.WithProfile(ctx, &profile)
		// Authentication sets this marker only after media/resource validation.
		// Household middleware must retain the profile authorized at issuance.
		return context.WithValue(ctx, recordingTicketProfileKey{}, profile)
	}
	return ctx
}

var recordingPlaylistURI = regexp.MustCompile(`URI="([^"]+)"`)

// rewriteRecordingPlaylistTicket carries the authenticated recording ticket to
// local media URIs only. Never forward a credential to another host or recording.
func rewriteRecordingPlaylistTicket(data []byte, recordingID, ticket string) []byte {
	if ticket == "" {
		return data
	}
	base := "/api/v3/recordings/" + recordingID + "/"
	rewriteURI := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			return raw
		}
		resolvedPath := parsed.Path
		if !strings.HasPrefix(resolvedPath, "/") {
			resolvedPath = base + resolvedPath
		}
		if path.Clean(resolvedPath) != resolvedPath || recordingIDFromMediaPath(resolvedPath) != recordingID {
			return raw
		}
		q := parsed.Query()
		q.Set("ticket", ticket)
		parsed.RawQuery = q.Encode()
		return parsed.String()
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			lines[i] = recordingPlaylistURI.ReplaceAllStringFunc(line, func(attribute string) string {
				return `URI="` + rewriteURI(attribute[5:len(attribute)-1]) + `"`
			})
		} else if strings.TrimSpace(line) != "" {
			lines[i] = rewriteURI(strings.TrimSpace(line))
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func (s *Server) recordingMediaTicket(r *http.Request, recordingID string) string {
	value := extractPlaybackTicket(r)
	ticket, ok := s.playbackTicketStoreOrDefault().resolve(value, time.Now().UTC())
	if !ok || ticket.recordingID != recordingID {
		return ""
	}
	return value
}
