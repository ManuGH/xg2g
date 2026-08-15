// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"fmt"

	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	"github.com/ManuGH/xg2g/internal/domain/identity"
)

// Errors surfaced when a pairing cannot be attached to a canonical identity.
var (
	// ErrPairingOwnerUnknown: the pairing names an owner that does not exist as
	// an identity user.
	ErrPairingOwnerUnknown = errors.New("pairing owner does not resolve to an identity user")
	// ErrIdentityServiceUnavailable: enrollment was attempted without an
	// identity service, which would silently produce no device at all.
	ErrIdentityServiceUnavailable = errors.New("identity service is not configured")
)

// identityDeviceEnroller implements the pairing package's DeviceEnroller port.
//
// It is the seam where pairing stops being an auth system and becomes only an
// authorisation gesture: past this point the identity domain owns the device,
// its grant, its refresh family and its DPoP-bound tokens.
type identityDeviceEnroller struct {
	server *Server
}

func (e identityDeviceEnroller) service() (*identity.Service, error) {
	svc := e.server.getIdentityService()
	if svc == nil {
		return nil, ErrIdentityServiceUnavailable
	}
	return svc, nil
}

// ResolveOwner maps the pairing's owner to a canonical identity user id.
//
// The pairing record stores a *username* (ApprovePairing falls back to
// principal.ID, which auth.NewPrincipal sets to the username), while identity
// requires a foreign key into users(id). The mapping is therefore explicit and
// may fail.
//
// It deliberately never creates a user and never synthesises an id. An owner
// that does not resolve is a configuration fault, and failing here — before the
// pairing is consumed — is what keeps a misconfiguration from burning the
// user's pairing and leaving an orphaned device behind.
func (e identityDeviceEnroller) ResolveOwner(ctx context.Context, ownerID string) (string, error) {
	svc, err := e.service()
	if err != nil {
		return "", err
	}

	user, err := svc.Store().GetUserByUsername(ctx, ownerID)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrPairingOwnerUnknown, ownerID, err)
	}
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("%w: %q", ErrPairingOwnerUnknown, ownerID)
	}
	return user.ID, nil
}

// EnrollDevice registers the device identity and issues its bound credentials.
//
// `IssueDeviceGrant` looks the device up by the server-computed thumbprint and
// re-uses it when present, so a repeated enrollment of the same key yields the
// same device identity rather than a second one.
func (e identityDeviceEnroller) EnrollDevice(
	ctx context.Context,
	in v3pairing.EnrollDeviceInput,
) (*v3pairing.EnrollDeviceResult, error) {
	svc, err := e.service()
	if err != nil {
		return nil, err
	}

	result, err := svc.IssueDeviceGrant(
		ctx,
		in.UserID,
		in.DeviceName,
		in.Platform,
		in.JWK,
		in.Scopes,
		// The grant records which front-end authorised it. Both mechanisms
		// share this substrate; neither may claim to be the other.
		identity.GrantTypePairingEnrollment,
	)
	if err != nil {
		return nil, err
	}

	return &v3pairing.EnrollDeviceResult{
		DeviceID:     result.DeviceID,
		TokenType:    result.TokenType,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
		Scope:        result.Scope,
	}, nil
}
