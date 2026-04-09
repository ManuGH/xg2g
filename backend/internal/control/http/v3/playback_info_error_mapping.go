// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strconv"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type playbackInfoHTTPProblemSpec struct {
	status     int
	problem    terminalProblemSpec
	extra      map[string]any
	retryAfter *int
	rawProblem *v3recordings.PlaybackInfoProblem
}

func problemSpecForPlaybackInfoError(schemaType string, err *v3recordings.PlaybackInfoError) playbackInfoHTTPProblemSpec {
	internal := playbackInfoHTTPProblemSpec{
		status:  http.StatusInternalServerError,
		problem: problemSpecForCode(problemcode.CodeInternalError, "Resolution Failed", "Failed to resolve playback info"),
	}
	internal.problem.problemType = "playback/resolution_failed"

	if err == nil {
		return internal
	}

	switch err.Kind {
	case v3recordings.PlaybackInfoErrorProblem:
		if err.Problem != nil {
			return playbackInfoHTTPProblemSpec{
				status:     err.Problem.Status,
				rawProblem: err.Problem,
			}
		}
	case v3recordings.PlaybackInfoErrorUnavailable:
		spec := problemSpecForCode(problemcode.CodeUnavailable, "Service Unavailable", err.Error())
		spec.problemType = "system/unavailable"
		return playbackInfoHTTPProblemSpec{status: http.StatusServiceUnavailable, problem: spec}
	case v3recordings.PlaybackInfoErrorInvalidInput:
		spec := problemSpecForCode(problemcode.CodeInvalidInput, "Invalid Request", err.Error())
		if schemaType == "live" {
			spec.problemType = "live/invalid"
		} else {
			spec.problemType = "recordings/invalid"
		}
		return playbackInfoHTTPProblemSpec{status: http.StatusBadRequest, problem: spec}
	case v3recordings.PlaybackInfoErrorForbidden:
		spec := problemSpecForCode(problemcode.CodeForbidden, "Access Denied", err.Error())
		spec.problemType = "recordings/forbidden"
		return playbackInfoHTTPProblemSpec{status: http.StatusForbidden, problem: spec}
	case v3recordings.PlaybackInfoErrorNotFound:
		spec := problemSpecForCode(problemcode.CodeNotFound, "Not Found", err.Error())
		spec.problemType = "recordings/not-found"
		return playbackInfoHTTPProblemSpec{status: http.StatusNotFound, problem: spec}
	case v3recordings.PlaybackInfoErrorPreparing:
		retryAfter := err.RetryAfterSeconds
		if err.Cause != nil && retryAfter <= 0 {
			retryAfter = 5
		}
		if err.Cause == nil {
			spec := problemSpecForCode(problemcode.CodeRecordingPreparing, "Media is being analyzed", "Retry shortly.")
			spec.problemType = "recordings/preparing"
			return playbackInfoHTTPProblemSpec{
				status:     http.StatusServiceUnavailable,
				problem:    spec,
				retryAfter: &retryAfter,
				extra: map[string]any{
					"retryAfterSeconds": retryAfter,
					"probeState":        err.ProbeState,
				},
			}
		}
		spec := problemSpecForCode(problemcode.CodePreparing, "Preparing", err.Error())
		spec.problemType = "recordings/preparing"
		return playbackInfoHTTPProblemSpec{
			status:     http.StatusServiceUnavailable,
			problem:    spec,
			retryAfter: &retryAfter,
		}
	case v3recordings.PlaybackInfoErrorUnverified:
		retryAfter := err.RetryAfterSeconds
		if retryAfter <= 0 {
			retryAfter = 5
		}
		problemType := "live/unverified"
		title := "Live media truth unavailable"
		code := problemcode.CodeUnavailable
		switch err.TruthReason {
		case "scanner_unavailable":
			problemType = "live/scan_unavailable"
			title = "Live scan unavailable"
			code = problemcode.CodeScanUnavailable
		case "missing_scan_truth":
			problemType = "live/missing_scan_truth"
			title = "Live media truth missing"
		case "inactive_event_feed":
			problemType = "live/inactive_event_feed"
			title = "Live event feed inactive"
		case "partial_scan_truth", "incomplete_scan_truth":
			problemType = "live/partial_truth"
			title = "Live media truth incomplete"
		case "failed_scan_truth":
			problemType = "live/failed_scan_truth"
			title = "Live media truth failed"
		}
		spec := problemSpecForCode(code, title, err.Error())
		spec.problemType = problemType
		extra := map[string]any{
			"retryAfterSeconds": retryAfter,
		}
		if err.TruthState != "" {
			extra["truthState"] = err.TruthState
		}
		if err.TruthReason != "" {
			extra["truthReason"] = err.TruthReason
		}
		if err.TruthOrigin != "" {
			extra["truthOrigin"] = err.TruthOrigin
		}
		if len(err.ProblemFlags) > 0 {
			extra["problemFlags"] = append([]string(nil), err.ProblemFlags...)
		}
		return playbackInfoHTTPProblemSpec{
			status:     http.StatusServiceUnavailable,
			problem:    spec,
			retryAfter: &retryAfter,
			extra:      extra,
		}
	case v3recordings.PlaybackInfoErrorUnsupported:
		spec := problemSpecForCode(problemcode.CodeRemoteProbeUnsupported, "Remote Probe Unsupported", err.Error())
		spec.problemType = "recordings/remote-probe-unsupported"
		return playbackInfoHTTPProblemSpec{status: http.StatusUnprocessableEntity, problem: spec}
	case v3recordings.PlaybackInfoErrorUpstreamUnavailable:
		if err.Cause == nil {
			spec := problemSpecForCode(problemcode.CodeUpstreamUnavailable, "Upstream media source is unavailable", "Retry later.")
			spec.problemType = "recordings/upstream_unavailable"
			return playbackInfoHTTPProblemSpec{status: http.StatusServiceUnavailable, problem: spec}
		}
		spec := problemSpecForCode(problemcode.CodeUpstreamError, "Upstream Error", err.Error())
		spec.problemType = "recordings/upstream"
		return playbackInfoHTTPProblemSpec{status: http.StatusBadGateway, problem: spec}
	}

	return internal
}

func writePlaybackInfoServiceError(w http.ResponseWriter, r *http.Request, id string, schemaType string, err *v3recordings.PlaybackInfoError) {
	spec := problemSpecForPlaybackInfoError(schemaType, err)
	if spec.retryAfter != nil {
		w.Header().Set("Retry-After", strconv.Itoa(*spec.retryAfter))
	}
	if spec.rawProblem != nil {
		writeProblem(w, r, spec.rawProblem.Status, spec.rawProblem.Type, spec.rawProblem.Title, spec.rawProblem.Code, spec.rawProblem.Detail, nil)
		return
	}

	if spec.problem.code == "" {
		fallback := problemSpecForCode(problemcode.CodeInternalError, "Resolution Failed", "Failed to resolve playback info")
		fallback.problemType = "playback/resolution_failed"
		log.L().Error().Err(err).Str("id", id).Msg("playback resolution failed")
		writeProblem(w, r, http.StatusInternalServerError, fallback.problemType, fallback.title, fallback.code, fallback.detail, nil)
		return
	}
	writeProblem(w, r, spec.status, spec.problem.problemType, spec.problem.title, spec.problem.code, spec.problem.detail, spec.extra)
}
