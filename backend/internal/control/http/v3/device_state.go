// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/domain/devicebinding"
)

// DeviceStateHeader reports device-binding state on **successful** responses.
//
// Defined once as a reusable OpenAPI header component and produced in exactly
// one place — the auth middleware — so no endpoint invents its own field.
//
// It is *state metadata*, not a warning. The request succeeded; the header is
// idempotent, carries no retry semantics, and a client that ignores it behaves
// exactly as before. Its job is to let a client show "this device is paired"
// versus "this device needs pairing" without inferring either from a status
// code.
//
// There is no deadline companion header: the cutover is hard, so an unbound
// credential is refused rather than warned about.
const DeviceStateHeader = "XG2G-Device-State"

// Values of DeviceStateHeader. Mirrors the `DeviceBindingState` enum in the
// OpenAPI contract; the pair is guarded by a test.
const (
	DeviceStateBound         = "bound"
	DeviceStateLegacyUnbound = "legacy_unbound"
)

// DeviceBindingStateValues is the closed set, in contract order.
var DeviceBindingStateValues = []string{DeviceStateBound, DeviceStateLegacyUnbound}

// deviceStateHeaders is the transport representation of a binding decision.
//
// A value type rather than loose strings so the mapping has one shape and one
// place. It renders; it never parses and never decides.
type deviceStateHeaders struct {
	State string
}

// deviceStateHeadersFor derives the transport representation from the decision
// already taken during authentication.
//
// Returns false when no device-binding decision took part — an admin token or a
// web session is not a paired device, and reporting `bound` for it would assert
// a property that was never established. Absence therefore means "not a
// paired-device session", never "unbound".
func deviceStateHeadersFor(result auth.Result) (deviceStateHeaders, bool) {
	if !result.OK() || result.Binding == nil {
		return deviceStateHeaders{}, false
	}

	switch result.Binding.State {
	case devicebinding.StateBound:
		return deviceStateHeaders{State: DeviceStateBound}, true
	case devicebinding.StateLegacyUnbound:
		return deviceStateHeaders{State: DeviceStateLegacyUnbound}, true
	default:
		return deviceStateHeaders{}, false
	}
}

// writeTo applies the header. Must run before the handler writes, since headers
// are immutable once the status line is out.
func (h deviceStateHeaders) writeTo(header http.Header) {
	header.Set(DeviceStateHeader, h.State)
}

func setDeviceStateHeader(w http.ResponseWriter, result auth.Result) {
	if headers, ok := deviceStateHeadersFor(result); ok {
		headers.writeTo(w.Header())
	}
}
