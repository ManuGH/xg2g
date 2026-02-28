// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/metrics"
)

type IntentErrorKind uint8

const (
	IntentErrInvalidInput IntentErrorKind = iota
	IntentErrV3Unavailable
	IntentErrSessionsFull
	IntentErrTranscodesFull
	IntentErrNoTuners
	IntentErrEngineDisabled
	IntentErrAdmissionUnknown
	IntentErrNoTunerSlots
	IntentErrStoreUnavailable
	IntentErrPublishUnavailable
	IntentErrLeaseBusy
	IntentErrInternal
)

type intentErrorSpec struct {
	status int
	apiErr *APIError
}

var intentErrorMap = map[IntentErrorKind]intentErrorSpec{
	IntentErrInvalidInput:       {status: http.StatusBadRequest, apiErr: ErrInvalidInput},
	IntentErrV3Unavailable:      {status: http.StatusServiceUnavailable, apiErr: ErrV3Unavailable},
	IntentErrSessionsFull:       {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionSessionsFull},
	IntentErrTranscodesFull:     {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionTranscodesFull},
	IntentErrNoTuners:           {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionNoTuners},
	IntentErrEngineDisabled:     {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionEngineDisabled},
	IntentErrAdmissionUnknown:   {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionStateUnknown},
	IntentErrNoTunerSlots:       {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionNoTuners},
	IntentErrStoreUnavailable:   {status: http.StatusServiceUnavailable, apiErr: ErrServiceUnavailable},
	IntentErrPublishUnavailable: {status: http.StatusServiceUnavailable, apiErr: ErrServiceUnavailable},
	IntentErrLeaseBusy:          {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionNoTuners},
	IntentErrInternal:           {status: http.StatusInternalServerError, apiErr: ErrInternalServer},
}

func respondIntentFailure(w http.ResponseWriter, r *http.Request, kind IntentErrorKind, details ...any) {
	spec, ok := intentErrorMap[kind]
	if !ok {
		spec = intentErrorMap[IntentErrInternal]
	}
	metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStageIntentLabel, spec.apiErr.Code)
	RespondError(w, r, spec.status, spec.apiErr, details...)
}
