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
	method  string
	pattern string
	handler http.HandlerFunc
	// authenticated wraps the handler in authMiddleware. The chi construction path used to
	// express this with a route group; going through the registrar contract requires an
	// explicit per-route wrap, which behaves identically and survives both paths.
	authenticated bool
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
		// Identity and passkey: declared in api/openapi.yaml and registered from
		// the generated route catalog, so nothing is listed here.

		// Android / native device grant (RFC 9449 sender-constrained enrollment)
		// The whole /auth/device surface is declared in api/openapi.yaml and
		// registered from the generated route catalog, so nothing is listed here.

		// Invitations
		{http.MethodPost, "/auth/invitations/redeem", svc.RedeemInvitation, false},
		{http.MethodPost, "/auth/invitations", svc.CreateInvitation, true},

		// Session and credential management
		{http.MethodGet, "/auth/passkeys", svc.ListPasskeys, true},
		{http.MethodDelete, "/auth/passkeys/{id}", svc.DeletePasskey, true},
		{http.MethodPost, "/auth/sessions/revoke-others", svc.RevokeOtherSessions, true},
		{http.MethodPost, "/auth/bootstrap/acknowledge-recovery", svc.AcknowledgeRecovery, true},
		{http.MethodPost, "/sessions/revoke-user-sessions", svc.RevokeUserSessions, true},
		{http.MethodGet, "/auth/effective-permissions", svc.GetEffectivePermissions, true},

		// Profiles
		{http.MethodGet, "/profiles", svc.ListProfiles, true},
		{http.MethodPost, "/profiles", svc.CreateProfile, true},
		{http.MethodGet, "/profiles/{id}", svc.GetProfile, true},
		{http.MethodDelete, "/profiles/{id}", svc.DeleteProfile, true},

		// Household policies and approvals
		{http.MethodGet, "/household/policies/access", svc.GetAccessPolicy, true},
		{http.MethodPost, "/household/policies/access", svc.CreateAccessPolicy, true},
		{http.MethodPost, "/household/policies/access/revoke", svc.RevokeAccessPolicy, true},
		{http.MethodGet, "/household/approvals", svc.ListApprovalRequests, true},
		{http.MethodPost, "/household/approvals", svc.CreateApprovalRequest, true},
		{http.MethodPost, "/household/approvals/{id}/approve", svc.ApproveApprovalRequest, true},
		{http.MethodPost, "/household/approvals/{id}/deny", svc.DenyApprovalRequest, true},
		{http.MethodGet, "/household/resource-policy", svc.GetHouseholdResourcePolicy, true},
		{http.MethodPut, "/household/resource-policy", svc.PutHouseholdResourcePolicy, true},

		// Notifications
		{http.MethodGet, "/notifications", svc.ListNotifications, true},
		{http.MethodGet, "/notifications/stream", svc.StreamNotifications, true},
		{http.MethodPost, "/notifications/mark-read", svc.MarkNotificationRead, true},
		{http.MethodPost, "/notifications/mark-all-read", svc.MarkAllNotificationsRead, true},
		{http.MethodDelete, "/notifications/{id}", svc.DeleteNotification, true},
		{http.MethodGet, "/notifications/vapid-key", svc.GetVAPIDPublicKey, true},
		{http.MethodPost, "/notifications/push-subscriptions", svc.SavePushSubscription, true},
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
