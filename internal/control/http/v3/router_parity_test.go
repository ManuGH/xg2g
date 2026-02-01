// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/authz"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

// Gate 3A (unit): hand-router route existence parity.
func TestRouterParity_HandRouter(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := NewRouter(Unimplemented{}, RouterOptions{BaseURL: V3BaseURL})
	runRouteExistenceSuite(t, router, V3BaseURL, doc)
}

// Gate 3C: production wiring parity (mounts + BaseURL from production wiring).
func TestRouterParity_ProductionWiring(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	handler := buildProductionHandler(t)
	runRouteExistenceSuite(t, handler, V3BaseURL, doc)
}

// Gate 3D: AuthZ matrix parity on production wiring.
func TestRouterParity_AuthZMatrix(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	handler := buildProductionHandlerWithAuthMatrix(t)
	runAuthMatrixSuite(t, handler, V3BaseURL, doc)
}

func runRouteExistenceSuite(t *testing.T, handler http.Handler, baseURL string, doc *openapi3.T) {
	t.Helper()
	forEachOperation(t, doc, func(method, path string, op *openapi3.Operation, params []*openapi3.Parameter) {
		req := buildRequest(t, method, baseURL+path, params, false)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusNotFound || rr.Code == http.StatusMethodNotAllowed {
			t.Fatalf("route not mounted: %s %s -> %d", method, baseURL+path, rr.Code)
		}
	})
}

func runAuthMatrixSuite(t *testing.T, handler http.Handler, baseURL string, doc *openapi3.T) {
	t.Helper()
	selected := selectRepresentativeOps(t, doc)
	for _, sel := range selected {
		method := sel.method
		path := sel.path
		op := sel.op
		params := sel.params
		opID := codegen.ToCamelCase(op.OperationID)
		required, ok := authz.RequiredScopes(opID)
		if !ok {
			t.Fatalf("missing scope policy for %s", opID)
		}

		urlPath := baseURL + path

		if len(required) == 0 {
			req := buildRequest(t, method, urlPath, params, true)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
				t.Fatalf("unscoped operation blocked by auth: %s %s -> %d", method, urlPath, rr.Code)
			}
			continue
		}

		// 1) No token => 401
		reqNoToken := buildRequest(t, method, urlPath, params, true)
		rrNoToken := httptest.NewRecorder()
		handler.ServeHTTP(rrNoToken, reqNoToken)
		if rrNoToken.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without token: %s %s -> %d", method, urlPath, rrNoToken.Code)
		}

		// 2) Token without required scope => 403
		reqBadScope := buildRequest(t, method, urlPath, params, true)
		setAuth(reqBadScope, "bad-token", wrongScope(required))
		rrBadScope := httptest.NewRecorder()
		handler.ServeHTTP(rrBadScope, reqBadScope)
		if rrBadScope.Code != http.StatusForbidden {
			t.Fatalf("expected 403 with wrong scope: %s %s -> %d", method, urlPath, rrBadScope.Code)
		}

		// 3) Token with required scope => not 401/403
		reqOK := buildRequest(t, method, urlPath, params, true)
		setAuth(reqOK, "ok-token", required[0])
		rrOK := httptest.NewRecorder()
		handler.ServeHTTP(rrOK, reqOK)
		if rrOK.Code == http.StatusUnauthorized || rrOK.Code == http.StatusForbidden {
			t.Fatalf("expected non-401/403 with correct scope: %s %s -> %d", method, urlPath, rrOK.Code)
		}
	}
}

func buildProductionHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.AppConfig{TrustedProxies: "0.0.0.0/0,::/0"}
	srv := NewServer(cfg, nil, nil)
	matcher := buildOpMatcher(t, loadOpenAPIDoc(t))
	srv.AuthMiddlewareOverride = testAuthMiddleware(t, matcher, false)
	handler, err := newHandlerWithMiddlewares(srv, cfg, []MiddlewareFunc{shortCircuitMiddleware})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

func buildProductionHandlerWithAuthMatrix(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.AppConfig{TrustedProxies: "0.0.0.0/0,::/0"}
	srv := NewServer(cfg, nil, nil)
	matcher := buildOpMatcher(t, loadOpenAPIDoc(t))
	srv.AuthMiddlewareOverride = testAuthMiddleware(t, matcher, true)
	handler, err := newHandlerWithMiddlewares(srv, cfg, []MiddlewareFunc{shortCircuitMiddleware})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

func forEachOperation(t *testing.T, doc *openapi3.T, fn func(method, path string, op *openapi3.Operation, params []*openapi3.Parameter)) {
	t.Helper()
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil || op.OperationID == "" {
				continue
			}
			params := collectParams(pathItem, op)
			fn(method, path, op, params)
		}
	}
}

type selectedOp struct {
	method string
	path   string
	op     *openapi3.Operation
	params []*openapi3.Parameter
}

func selectRepresentativeOps(t *testing.T, doc *openapi3.T) []selectedOp {
	t.Helper()
	var picked []selectedOp
	var readOp, writeOp, adminOp, unscopedOp *selectedOp
	var headOp *selectedOp

	forEachOperation(t, doc, func(method, path string, op *openapi3.Operation, params []*openapi3.Parameter) {
		if op == nil || op.OperationID == "" {
			return
		}
		opID := codegen.ToCamelCase(op.OperationID)
		required, ok := authz.RequiredScopes(opID)
		if !ok {
			return
		}

		curr := selectedOp{method: method, path: path, op: op, params: params}
		if method == http.MethodHead && headOp == nil {
			headOp = &curr
		}

		if len(required) == 0 {
			if unscopedOp == nil {
				unscopedOp = &curr
			}
			return
		}

		for _, scope := range required {
			switch scope {
			case "v3:read":
				if readOp == nil {
					readOp = &curr
				}
			case "v3:write":
				if writeOp == nil {
					writeOp = &curr
				}
			case "v3:admin":
				if adminOp == nil {
					adminOp = &curr
				}
			}
		}
	})

	if readOp == nil || writeOp == nil || adminOp == nil || unscopedOp == nil {
		missing := []string{}
		if readOp == nil {
			missing = append(missing, "v3:read")
		}
		if writeOp == nil {
			missing = append(missing, "v3:write")
		}
		if adminOp == nil {
			missing = append(missing, "v3:admin")
		}
		if unscopedOp == nil {
			missing = append(missing, "unscoped")
		}
		t.Fatalf("missing representative operations for: %s", strings.Join(missing, ", "))
	}

	picked = append(picked, *readOp, *writeOp, *adminOp, *unscopedOp)
	if headOp != nil {
		picked = append(picked, *headOp)
	}
	return picked
}

func collectParams(pathItem *openapi3.PathItem, op *openapi3.Operation) []*openapi3.Parameter {
	params := make([]*openapi3.Parameter, 0)
	for _, ref := range pathItem.Parameters {
		if ref != nil && ref.Value != nil {
			params = append(params, ref.Value)
		}
	}
	for _, ref := range op.Parameters {
		if ref != nil && ref.Value != nil {
			params = append(params, ref.Value)
		}
	}
	return params
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

func buildRequest(t *testing.T, method, path string, params []*openapi3.Parameter, includeRequired bool) *http.Request {
	t.Helper()
	paramByName := map[string]*openapi3.Parameter{}
	for _, p := range params {
		if p.In == "path" {
			paramByName[p.Name] = p
		}
	}

	resolvedPath := pathParamRe.ReplaceAllStringFunc(path, func(m string) string {
		name := pathParamRe.FindStringSubmatch(m)[1]
		if p, ok := paramByName[name]; ok {
			return samplePathValue(name, p.Schema)
		}
		return "x"
	})

	u, err := url.Parse(resolvedPath)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	if includeRequired {
		q := u.Query()
		for _, p := range params {
			if p.In != "query" || !p.Required {
				continue
			}
			q.Set(p.Name, sampleValueForSchema(p.Schema, p.Name))
		}
		u.RawQuery = q.Encode()
	}

	req := httptest.NewRequest(method, u.String(), nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if includeRequired {
		for _, p := range params {
			if p.In == "header" && p.Required {
				req.Header.Set(p.Name, sampleValueForSchema(p.Schema, p.Name))
			}
			if p.In == "cookie" && p.Required {
				req.AddCookie(&http.Cookie{Name: p.Name, Value: sampleValueForSchema(p.Schema, p.Name)})
			}
		}
	}

	return req
}

func setAuth(req *http.Request, token string, scopes ...string) {
	req.Header.Set("Authorization", "Bearer "+token)
	if len(scopes) > 0 {
		req.Header.Set("X-Test-Scopes", strings.Join(scopes, " "))
	}
}

func wrongScope(required []string) string {
	if len(required) == 0 {
		return "v3:status"
	}
	switch required[0] {
	case "v3:read":
		return "v3:status"
	case "v3:write":
		return "v3:read"
	case "v3:admin":
		return "v3:write"
	default:
		return "v3:status"
	}
}

type opEntry struct {
	method   string
	re       *regexp.Regexp
	required []string
}

type opMatcher struct {
	entries []opEntry
}

func buildOpMatcher(t *testing.T, doc *openapi3.T) opMatcher {
	t.Helper()
	var entries []opEntry
	forEachOperation(t, doc, func(method, path string, op *openapi3.Operation, params []*openapi3.Parameter) {
		opID := codegen.ToCamelCase(op.OperationID)
		required, ok := authz.RequiredScopes(opID)
		if !ok {
			return
		}
		re := regexp.MustCompile("^" + pathParamRe.ReplaceAllString(path, "[^/]+") + "$")
		entries = append(entries, opEntry{
			method:   method,
			re:       re,
			required: required,
		})
	})
	return opMatcher{entries: entries}
}

func (m opMatcher) requiredFor(method, path string) ([]string, bool) {
	for _, entry := range m.entries {
		if entry.method != method {
			continue
		}
		if entry.re.MatchString(path) {
			return entry.required, true
		}
	}
	return nil, false
}

func testAuthMiddleware(t *testing.T, matcher opMatcher, allowUnscopedWithoutToken bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			required, _ := r.Context().Value(bearerAuthScopesKey).([]string)
			if expected, ok := matcher.requiredFor(r.Method, strings.TrimPrefix(r.URL.Path, V3BaseURL)); ok {
				if len(expected) > 0 && len(required) == 0 {
					t.Fatalf("missing BearerAuthScopes for scoped op: %s %s", r.Method, r.URL.Path)
				}
			}
			token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))

			if token == "" {
				if allowUnscopedWithoutToken && len(required) == 0 {
					next.ServeHTTP(w, r)
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			scopes := strings.Fields(r.Header.Get("X-Test-Scopes"))
			p := auth.NewPrincipal(token, "", scopes)
			ctx := auth.WithPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func shortCircuitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func samplePathValue(name string, schema *openapi3.SchemaRef) string {
	lower := strings.ToLower(name)
	if schema != nil && schema.Value != nil && schema.Value.Format == "uuid" {
		return "00000000-0000-0000-0000-000000000000"
	}
	switch lower {
	case "sessionid", "session_id":
		return "00000000-0000-0000-0000-000000000000"
	case "filename":
		return "file.m3u8"
	case "segment":
		return "seg.ts"
	case "recordingid":
		return "rec123"
	case "timerid":
		return "timer123"
	case "id":
		return "id123"
	default:
		return "x"
	}
}

func sampleValueForSchema(schema *openapi3.SchemaRef, name string) string {
	if schema == nil || schema.Value == nil {
		return "x"
	}
	v := schema.Value
	if v.Format == "uuid" {
		return "00000000-0000-0000-0000-000000000000"
	}
	if len(v.Enum) > 0 {
		if s, ok := v.Enum[0].(string); ok {
			return s
		}
		if b, ok := v.Enum[0].(bool); ok {
			if b {
				return "true"
			}
			return "false"
		}
	}
	if types := v.Type.Slice(); len(types) > 0 {
		switch types[0] {
		case "integer", "number":
			return "1"
		case "boolean":
			return "true"
		}
	}
	if v.Format == "date" {
		return "2024-01-01"
	}
	if v.Format == "date-time" {
		return "2024-01-01T00:00:00Z"
	}
	if strings.EqualFold(name, "filename") {
		return "file.m3u8"
	}
	return "x"
}
