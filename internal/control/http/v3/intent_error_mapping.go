// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

type IntentErrorKind uint8

const (
	IntentErrInvalidInput IntentErrorKind = iota
	IntentErrV3Unavailable
	IntentErrAdmissionRejected
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

var intentErrorKinds = []IntentErrorKind{
	IntentErrInvalidInput,
	IntentErrV3Unavailable,
	IntentErrAdmissionRejected,
	IntentErrNoTunerSlots,
	IntentErrStoreUnavailable,
	IntentErrPublishUnavailable,
	IntentErrLeaseBusy,
	IntentErrInternal,
}

var intentErrorMap = map[IntentErrorKind]intentErrorSpec{
	IntentErrInvalidInput:       {status: http.StatusBadRequest, apiErr: ErrInvalidInput},
	IntentErrV3Unavailable:      {status: http.StatusServiceUnavailable, apiErr: ErrV3Unavailable},
	IntentErrAdmissionRejected:  {status: http.StatusServiceUnavailable, apiErr: ErrAdmissionRejected},
	IntentErrNoTunerSlots:       {status: http.StatusServiceUnavailable, apiErr: ErrServiceUnavailable},
	IntentErrStoreUnavailable:   {status: http.StatusServiceUnavailable, apiErr: ErrServiceUnavailable},
	IntentErrPublishUnavailable: {status: http.StatusServiceUnavailable, apiErr: ErrServiceUnavailable},
	IntentErrLeaseBusy:          {status: http.StatusConflict, apiErr: ErrLeaseBusy},
	IntentErrInternal:           {status: http.StatusInternalServerError, apiErr: ErrInternalServer},
}

func respondIntentFailure(w http.ResponseWriter, r *http.Request, kind IntentErrorKind, details ...any) {
	spec, ok := intentErrorMap[kind]
	if !ok {
		spec = intentErrorMap[IntentErrInternal]
	}
	RespondError(w, r, spec.status, spec.apiErr, details...)
}
