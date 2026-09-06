// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handwrittenRoute is one v3 route that is not generated from the OpenAPI contract.
type handwrittenRoute struct {
	method         string
	pattern        string
	handler        http.HandlerFunc
	authenticated  bool
	requiredScopes []Scope
}

// handwrittenRoutes is the single source of truth for every v3 route that is not generated
// from the OpenAPI contract.
//
// There are two router construction paths: v3.NewHandler builds a chi router directly (used
// by tests and by anything embedding the v3 handler), while the daemon builds its own router
// in internal/api and registers routes through a RouteRegistrar. Both used to carry their own
// hand-maintained copy of this list, and they drifted: 20 routes - the whole Android device
// grant surface, the household policy and approval endpoints, and the notification endpoints -
// existed only in the path tests exercise, and answered 404 in production.
//
// Adding a route here registers it on both paths. Do not register handwritten v3 routes
// anywhere else; TestHandwrittenRouteParity enforces that the two paths stay identical.
func handwrittenRoutes(svc *Server) []handwrittenRoute {
	return []handwrittenRoute{
		// Identity and passkey
		{http.MethodGet, "/auth/status", svc.AuthStatus, false, nil},
		{http.MethodPost, "/auth/passkey/login/start", svc.PasskeyLoginStart, false, nil},
		{http.MethodPost, "/auth/passkey/login/finish", svc.PasskeyLoginFinish, false, nil},
		{http.MethodPost, "/auth/passkey/register/start", svc.PasskeyRegisterStart, false, nil},
		{http.MethodPost, "/auth/passkey/register/finish", svc.PasskeyRegisterFinish, false, nil},
		{http.MethodPost, "/auth/recovery", svc.RecoveryLogin, false, nil},
		{http.MethodPost, "/auth/login/password", svc.PasswordLogin, false, nil},

		// Android / native device grant (RFC 9449 sender-constrained enrollment)
		{http.MethodPost, "/auth/device/grant/start", svc.DeviceGrantStart, false, nil},
		{http.MethodPost, "/auth/device/grant/finish", svc.DeviceGrantFinish, false, nil},
		{http.MethodPost, "/auth/device/session", svc.DeviceSessionCompat, false, nil},
		// /auth/device/refresh is not listed here: it is declared in
		// api/openapi.yaml and registered from the generated route catalog.

		// Authenticated, unlike the two above: a device proves who it is with
		// its live DPoP credential, and the handler revokes exactly that
		// device.
		{http.MethodPost, "/auth/device/revoke", svc.DeviceSelfRevoke, true, nil},

		// Invitations
		{http.MethodPost, "/auth/invitations/redeem", svc.RedeemInvitation, false, nil},
		{http.MethodPost, "/auth/invitations", svc.CreateInvitation, true, []Scope{ScopeV3Admin}},

		// Session and credential management
		{http.MethodGet, "/auth/passkeys", svc.ListPasskeys, true, []Scope{ScopeV3Read}},
		{http.MethodDelete, "/auth/passkeys/{id}", svc.DeletePasskey, true, []Scope{ScopeV3Admin}},
		{http.MethodPost, "/auth/sessions/revoke-others", svc.RevokeOtherSessions, true, []Scope{ScopeV3Write}},
		{http.MethodPost, "/auth/bootstrap/acknowledge-recovery", svc.AcknowledgeRecovery, true, []Scope{ScopeV3Admin}},
		{http.MethodPost, "/sessions/revoke-user-sessions", svc.RevokeUserSessions, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/auth/effective-permissions", svc.GetEffectivePermissions, true, []Scope{ScopeV3Read}},

		// Profiles (legacy endpoints; /household/profiles is declared in OpenAPI)
		{http.MethodGet, "/profiles", svc.ListProfiles, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/profiles", svc.CreateProfile, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/profiles/{id}", svc.GetProfile, true, []Scope{ScopeV3Read}},
		{http.MethodPut, "/profiles/{id}", svc.UpdateProfile, true, []Scope{ScopeV3Admin}},
		{http.MethodDelete, "/profiles/{id}", svc.DeleteProfile, true, []Scope{ScopeV3Admin}},

		// Household policies and approvals
		{http.MethodGet, "/household/policies/access", svc.GetAccessPolicy, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/household/policies/access", svc.CreateAccessPolicy, true, []Scope{ScopeV3Admin}},
		{http.MethodPost, "/household/policies/access/revoke", svc.RevokeAccessPolicy, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/household/approvals", svc.ListApprovalRequests, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/household/approvals", svc.CreateApprovalRequest, true, []Scope{ScopeV3Write}},
		{http.MethodPost, "/household/approvals/{id}/approve", svc.ApproveApprovalRequest, true, []Scope{ScopeV3Admin}},
		{http.MethodPost, "/household/approvals/{id}/deny", svc.DenyApprovalRequest, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/household/resource-policy", svc.GetHouseholdResourcePolicy, true, []Scope{ScopeV3Read}},
		{http.MethodPut, "/household/resource-policy", svc.PutHouseholdResourcePolicy, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/household/devices", svc.ListHouseholdDevices, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/household/devices/{id}/revoke", svc.RevokeHouseholdDevice, true, []Scope{ScopeV3Admin}},
		{http.MethodGet, "/household/members", svc.ListHouseholdMembers, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/household/members/invite", svc.CreateInvitation, true, []Scope{ScopeV3Admin}},
		{http.MethodDelete, "/household/members/{id}", svc.RemoveHouseholdMember, true, []Scope{ScopeV3Admin}},

		// Notifications
		{http.MethodGet, "/notifications", svc.ListNotifications, true, []Scope{ScopeV3Read}},
		{http.MethodGet, "/notifications/stream", svc.StreamNotifications, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/notifications/mark-read", svc.MarkNotificationRead, true, []Scope{ScopeV3Write}},
		{http.MethodPost, "/notifications/mark-all-read", svc.MarkAllNotificationsRead, true, []Scope{ScopeV3Write}},
		{http.MethodDelete, "/notifications/{id}", svc.DeleteNotification, true, []Scope{ScopeV3Write}},
		{http.MethodGet, "/notifications/vapid-key", svc.GetVAPIDPublicKey, true, []Scope{ScopeV3Read}},
		{http.MethodPost, "/notifications/push-subscriptions", svc.SavePushSubscription, true, []Scope{ScopeV3Write}},
	}
}

// HandwrittenRoutePatterns returns the method and local pattern of every handwritten v3 route.
// It lets the daemon's wiring tests assert that each of these routes actually reached the
// production router, which is the invariant that broke when the two paths kept separate lists.
func HandwrittenRoutePatterns() [][2]string {
	routes := handwrittenRoutes(&Server{})
	out := make([][2]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, [2]string{route.method, route.pattern})
	}
	return out
}

// registerHandwrittenRoutes registers every handwritten v3 route through registrar.
func registerHandwrittenRoutes(registrar RouteRegistrar, svc *Server) error {
	if registrar == nil {
		return fmt.Errorf("RouteRegistrar cannot be nil")
	}
	if svc == nil {
		return fmt.Errorf("v3 Server cannot be nil")
	}

	for _, route := range handwrittenRoutes(svc) {
		var handler http.Handler = route.handler
		if len(route.requiredScopes) > 0 {
			handler = svc.ScopeMiddleware(route.requiredScopes...)(handler)
		}
		if route.authenticated {
			handler = svc.authMiddleware(handler)
		}
		if err := registrar.Register(route.method, route.pattern, handler); err != nil {
			return fmt.Errorf("register %s %s: %w", route.method, route.pattern, err)
		}
	}
	return nil
}

// chiRouteRegistrar adapts a chi.Router to the RouteRegistrar contract so that the chi
// construction path can consume the same route list as the daemon's registrar path.
type chiRouteRegistrar struct {
	router chi.Router
}

func (c chiRouteRegistrar) Register(method, pattern string, handler http.Handler) (err error) {
	if c.router == nil {
		return fmt.Errorf("nil chi router")
	}
	// chi panics on an invalid pattern or a duplicate registration; surface that as an error
	// so handler construction fails loudly instead of at request time.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("chi route registration failed for %s %s: %v", method, pattern, recovered)
		}
	}()
	c.router.Method(method, pattern, handler)
	return nil
}
