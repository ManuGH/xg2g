package recordings

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestGateA_NoForbiddenImports(t *testing.T) {
	cfg := &packages.Config{Mode: packages.NeedImports}
	pkgs, err := packages.Load(cfg, "github.com/ManuGH/xg2g/internal/control/recordings")
	if err != nil {
		t.Fatalf("failed to load package: %v", err)
	}

	forbiddenPatterns := []string{
		"net/http",
		"github.com/go-chi/chi",
		"github.com/ManuGH/xg2g/internal/control/http",
	}

	for _, pkg := range pkgs {
		for imp := range pkg.Imports {
			for _, pattern := range forbiddenPatterns {
				if strings.Contains(imp, pattern) {
					t.Errorf("forbidden import found in domain package: %s (matches pattern %s)", imp, pattern)
				}
			}
		}
	}
}
