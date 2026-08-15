// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/domain/devicebinding"
)

func statePrincipal() *auth.Principal {
	return auth.NewPrincipal("token", "alice", []string{"v3:read"})
}

// MARK: - Contract binding

type deviceStateOpenAPIDoc struct {
	Components struct {
		Headers map[string]struct {
			Schema struct {
				Ref    string `yaml:"$ref"`
				Type   string `yaml:"type"`
				Format string `yaml:"format"`
			} `yaml:"schema"`
		} `yaml:"headers"`
		Schemas map[string]struct {
			Enum []string `yaml:"enum"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadDeviceStateContract(t *testing.T) deviceStateOpenAPIDoc {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		candidate := filepath.Join(dir, "api", "openapi.yaml")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			data, readErr := os.ReadFile(candidate)
			require.NoError(t, readErr)
			var doc deviceStateOpenAPIDoc
			require.NoError(t, yaml.Unmarshal(data, &doc))
			return doc
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	require.FailNow(t, "api/openapi.yaml not found")
	return deviceStateOpenAPIDoc{}
}

// The header must be a contract type, not a string convention that happens to
// be documented. If the Go values and the contract enum can drift apart, the
// semantics are back in strings.
func TestDeviceStateValuesMatchTheContractEnum(t *testing.T) {
	doc := loadDeviceStateContract(t)

	schema, ok := doc.Components.Schemas["DeviceBindingState"]
	require.True(t, ok, "schema DeviceBindingState missing from the contract")

	expected := slices.Clone(DeviceBindingStateValues)
	actual := slices.Clone(schema.Enum)
	slices.Sort(expected)
	slices.Sort(actual)

	require.Equal(t, expected, actual, "Go device state values and the OpenAPI enum disagree")
}

func TestDeviceStateHeadersAreDeclaredAsComponents(t *testing.T) {
	doc := loadDeviceStateContract(t)

	state, ok := doc.Components.Headers["Xg2gDeviceState"]
	require.True(t, ok, "header component Xg2gDeviceState missing")
	require.Equal(t, "#/components/schemas/DeviceBindingState", state.Schema.Ref,
		"the state header must reference the enum rather than restate it")

}

// MARK: - Derivation

func TestBoundDeviceReportsBound(t *testing.T) {
	headers, ok := deviceStateHeadersFor(auth.AuthenticatedDevice(statePrincipal(), devicebinding.Decision{
		Outcome: devicebinding.OutcomeAllow,
		State:   devicebinding.StateBound,
	}))

	require.True(t, ok, "a paired device must report its state")
	require.Equal(t, DeviceStateBound, headers.State)
}

// A device that has not re-paired reports its state; the request that carries
// it is refused elsewhere, so this is only observable on a still-valid session.
func TestLegacyStateIsReported(t *testing.T) {
	headers, ok := deviceStateHeadersFor(auth.AuthenticatedDevice(statePrincipal(), devicebinding.Decision{
		Outcome: devicebinding.OutcomeAllow,
		State:   devicebinding.StateLegacyUnbound,
	}))

	require.True(t, ok)
	require.Equal(t, DeviceStateLegacyUnbound, headers.State)
}

// An admin token or web session is not a paired device; claiming `bound` would
// assert a property that was never established.
func TestNonDeviceSessionsReportNothing(t *testing.T) {
	_, ok := deviceStateHeadersFor(auth.Authenticated(statePrincipal()))
	require.False(t, ok, "a non-device session must not report a device state")
}

func TestFailedAuthReportsNothing(t *testing.T) {
	for name, result := range map[string]auth.Result{
		"invalid":         auth.InvalidCredentials(),
		"none":            auth.NoCredentials(),
		"repair required": auth.RepairRequired(devicebinding.Decision{State: devicebinding.StateLegacyUnbound}),
	} {
		_, ok := deviceStateHeadersFor(result)
		require.Falsef(t, ok, "%s: a failed request must not carry success state metadata", name)
	}
}

// MARK: - Transport behaviour

// State metadata, not an error: idempotent, no accumulation, no status change.
func TestHeadersAreIdempotent(t *testing.T) {
	result := auth.AuthenticatedDevice(statePrincipal(), devicebinding.Decision{
		Outcome: devicebinding.OutcomeAllow,
		State:   devicebinding.StateBound,
	})
	recorder := httptest.NewRecorder()

	setDeviceStateHeader(recorder, result)
	setDeviceStateHeader(recorder, result)

	require.Len(t, recorder.Header().Values(DeviceStateHeader), 1, "headers must not accumulate")
	require.Equal(t, http.StatusOK, recorder.Code, "state metadata must not change the status")
}

// The reported state must come from the decision taken during authentication,
// never from a second evaluation in the transport.
func TestStateFollowsTheDomainDecision(t *testing.T) {
	decision := devicebinding.Evaluate(devicebinding.StateBound)

	headers, ok := deviceStateHeadersFor(auth.AuthenticatedDevice(statePrincipal(), decision))

	require.True(t, ok)
	require.Equal(t, DeviceStateBound, headers.State)
}
