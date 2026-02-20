package v3

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRouteRegistrar_AddPanicsWhenScopePolicyMissing(t *testing.T) {
	register := routeRegistrar{
		baseURL: V3BaseURL,
		router:  chi.NewRouter(),
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic when scope policy is missing")
		}
		msg, ok := recovered.(string)
		if !ok {
			t.Fatalf("expected panic string, got %T", recovered)
		}
		if !strings.Contains(msg, "missing scope policy for operation UnknownPolicyOperation") {
			t.Fatalf("unexpected panic message: %s", msg)
		}
	}()

	register.add(http.MethodGet, "/__test__", "UnknownPolicyOperation", func(http.ResponseWriter, *http.Request) {})
}

