package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/authz"
)

func TestGeneratedOperationRoutesMatchAuthorizationCatalog(t *testing.T) {
	operationIDs := authz.OperationIDs()
	if len(operationRoutes) != len(operationIDs) {
		t.Fatalf("route count = %d, authorization operation count = %d", len(operationRoutes), len(operationIDs))
	}

	seenRoutes := make(map[string]string, len(operationRoutes))
	for _, operationID := range operationIDs {
		route, ok := operationRoutes[operationID]
		if !ok {
			t.Errorf("missing generated route for %s", operationID)
			continue
		}
		if route.Method == "" || route.Path == "" {
			t.Errorf("%s has incomplete route: %+v", operationID, route)
			continue
		}
		key := route.Method + " " + route.Path
		if previous, exists := seenRoutes[key]; exists {
			t.Errorf("duplicate generated route %s for %s and %s", key, previous, operationID)
		}
		seenRoutes[key] = operationID
	}
}
