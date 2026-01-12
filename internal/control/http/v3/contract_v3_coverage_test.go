package v3

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/problem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProblemWrite_Hardening(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/test?q=1", nil)

	extra := map[string]any{
		"status":   999, // Collision!
		"type":     "hack",
		"title":    "HAX",
		"detail":   "OVERWRITE",
		"instance": "/evil",
		"code":     "EVIL",
		"valid":    "preserved",
	}

	problem.Write(w, r, http.StatusBadRequest, "test/type", "TEST_TITLE", "TEST_CODE", "TEST_DETAIL", extra)

	resp := w.Result()
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json"))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	// Invariants: Canonical fields must win
	assert.Equal(t, "test/type", body["type"])
	assert.Equal(t, "TEST_TITLE", body["title"])
	assert.Equal(t, "TEST_CODE", body["code"])
	assert.Equal(t, 400.0, body["status"]) // json.Unmarshal uses float64
	assert.Equal(t, "TEST_DETAIL", body["detail"])
	assert.Equal(t, "/api/v3/test", body["instance"])

	// Extension: Non-colliding keys must be preserved
	assert.Equal(t, "preserved", body["valid"])
}

func TestV3Router_Hardening(t *testing.T) {
	// Setup server and handler
	cfg := config.AppConfig{}
	srv := NewServer(cfg, nil, nil)
	// We need a dummy auth middleware that just lets things through for this test
	srv.AuthMiddlewareOverride = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	h, err := NewHandler(srv, cfg)
	require.NoError(t, err)

	t.Run("404_NotFound", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/non-existent-route", nil)
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json"))

		var body map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "system/not_found", body["type"])
		assert.Equal(t, "Not Found", body["title"])
		assert.Equal(t, "NOT_FOUND", body["code"])
		assert.Equal(t, 404.0, body["status"])
		assert.Equal(t, "/api/v3/non-existent-route", body["instance"])
	})

	t.Run("405_MethodNotAllowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		// CreateSession is POST only
		r := httptest.NewRequest("GET", "/api/v3/auth/session", nil)
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json"))

		var body map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "system/method_not_allowed", body["type"])
		assert.Equal(t, "Method Not Allowed", body["title"])
		assert.Equal(t, "METHOD_NOT_ALLOWED", body["code"])
		assert.Equal(t, 405.0, body["status"])
		assert.Equal(t, "/api/v3/auth/session", body["instance"])
	})
}

// TestV3RFC7807Compliance enforces the "Reality Gate" rules using strict AST analysis.
func TestV3RFC7807Compliance(t *testing.T) {
	rootPath := "."

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != "." && (filepath.Base(path) == "problem" || filepath.Base(path) == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_gen.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("Failed to parse %s: %v", path, err)
			return nil
		}

		// Iterate over function declarations to enable context-aware checks
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			funcName := fn.Name.Name

			// Helper allowance: writeJSON in errors.go is allowed to use w.WriteHeader(code)
			// We check function name AND filename.
			isLowLevelHelper := (funcName == "writeJSON" && filepath.Base(path) == "errors.go") || funcName == "WriteHeader"

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				// 1. Forbidden httpError (Universal)
				if isCallTo(call, "http", "Error") {
					t.Errorf("%s:%s: Found forbidden httpError call in %s. Use writeProblem(w, r, ...) instead.",
						path, fset.Position(call.Pos()), funcName)
				}

				// 2. Restricted WriteHeader
				// Only allow 200, 201, 202, 204 literals/constants.
				// Exception: writeJSON helper.
				if isMethodCall(call, "WriteHeader") {
					if !isLowLevelHelper {
						if len(call.Args) > 0 {
							arg := call.Args[0]
							if !isAllowedStatus(arg) {
								t.Errorf("%s:%s: Found manual WriteHeader with non-success code in %s. Errors must use writeProblem(w, r, ...).",
									path, fset.Position(call.Pos()), funcName)
							}
						}
					}
				}

				// 3. Ensure writeProblem / RespondError pass 'r' as 2nd arg
				if isCallToLocal(call, "writeProblem") || isCallToLocal(call, "RespondError") {
					if len(call.Args) >= 2 {
						if !isIdent(call.Args[1], "r") {
							t.Errorf("%s:%s: %s call missing '*http.Request' (r) parameter as 2nd argument in %s.",
								path, fset.Position(call.Pos()), getCallName(call), funcName)
						}
					}
				}

				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
}

// Helpers for AST analysis

func isCallTo(call *ast.CallExpr, pkg, fun string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg && sel.Sel.Name == fun
}

func isCallToLocal(call *ast.CallExpr, fun string) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == fun
}

func isMethodCall(call *ast.CallExpr, method string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == method
}

func getCallName(call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		return id.Name
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return "unknown"
}

func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

func isAllowedStatus(expr ast.Expr) bool {
	// Check for literals: 200, 201, 202, 204
	if lit, ok := expr.(*ast.BasicLit); ok {
		return lit.Kind == token.INT && (lit.Value == "200" || lit.Value == "201" || lit.Value == "202" || lit.Value == "204")
	}
	// Check for constants: http.StatusOK, etc.
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "http" {
			switch sel.Sel.Name {
			case "StatusOK", "StatusCreated", "StatusAccepted", "StatusNoContent":
				return true
			}
		}
	}
	// Check for local 'status' variable which serves as a proxy (weak check but needed for some generic helpers)
	// In the real AST gate, we might strict this further, but existing code like system.go uses 'status' var.
	// But wait, system.go 'writeProblem' definition uses 'status'. We excluded definition files logic above via string check?
	// Actually we removed the explicit exclusion of system.go calls.
	// The rule says: "Disallow all others (errors must use writeProblem/RespondError)".
	// If the code calls `w.WriteHeader(status)`, it's suspicious unless it's a success path.
	// For now, let's keep it strict.
	return false
}
