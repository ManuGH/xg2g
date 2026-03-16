// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type playbackInfoInputProblem struct {
	status      int
	problemType string
	title       string
	code        string
	detail      string
	extra       map[string]any
}

type livePlaybackInfoInput struct {
	serviceRef   string
	capabilities *PlaybackCapabilities
}

func parseRecordingPlaybackPostInput(r *http.Request) (*PlaybackCapabilities, *playbackInfoInputProblem) {
	var caps PlaybackCapabilities
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&caps); err != nil {
		return nil, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "recordings/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidCapabilities,
			detail:      "Failed to parse capabilities body: " + err.Error(),
		}
	}
	if caps.CapabilitiesVersion < 1 {
		return nil, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "recordings/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidCapabilities,
			detail:      "capabilities_version must be >= 1",
		}
	}
	return &caps, nil
}

func parseLivePlaybackPostInput(r *http.Request) (livePlaybackInfoInput, *playbackInfoInputProblem) {
	bodyBytes, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		return livePlaybackInfoInput{}, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "live/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidInput,
			detail:      "Failed to read request body: " + readErr.Error(),
		}
	}

	rawBody := string(bodyBytes)
	log.L().Debug().Str("body", rawBody).Msg("PostLivePlaybackInfo request body")

	var req PostLivePlaybackInfoJSONRequestBody
	dec := json.NewDecoder(strings.NewReader(rawBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		log.L().Warn().Err(err).Str("body", rawBody).Msg("PostLivePlaybackInfo parse failed")
		return livePlaybackInfoInput{}, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "live/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidInput,
			detail:      "Failed to parse request body: " + err.Error(),
		}
	}
	if req.ServiceRef == "" {
		return livePlaybackInfoInput{}, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "live/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidInput,
			detail:      "serviceRef is required",
		}
	}

	serviceRef := normalize.ServiceRef(req.ServiceRef)
	if err := recordings.ValidateLiveRef(serviceRef); err != nil {
		return livePlaybackInfoInput{}, &playbackInfoInputProblem{
			status:      http.StatusBadRequest,
			problemType: "live/invalid",
			title:       "Invalid Request",
			code:        problemcode.CodeInvalidInput,
			detail:      "serviceRef must be a valid live Enigma2 reference",
		}
	}

	return livePlaybackInfoInput{
		serviceRef:   serviceRef,
		capabilities: (*PlaybackCapabilities)(&req.Capabilities),
	}, nil
}

func writePlaybackInfoInputProblem(w http.ResponseWriter, r *http.Request, problem *playbackInfoInputProblem) {
	if problem == nil {
		return
	}
	writeRegisteredProblem(w, r, problem.status, problem.problemType, problem.title, problem.code, problem.detail, problem.extra)
}
