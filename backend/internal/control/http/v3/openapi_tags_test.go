package v3

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

var allowedOperationTags = map[string]struct{}{
	"auth":       {},
	"dvr":        {},
	"epg":        {},
	"receiver":   {},
	"recordings": {},
	"series":     {},
	"services":   {},
	"streams":    {},
	"system":     {},
	"timers":     {},
	"v3":         {},
}

func TestOpenAPIOperationsHaveAllowedTags(t *testing.T) {
	doc := loadOpenAPISpec(t)

	missingTags := make([]string, 0)
	unknownTags := make([]string, 0)

	for path, pathItem := range doc.Paths.Map() {
		for method, op := range pathItem.Operations() {
			opID := op.OperationID
			if opID == "" {
				opID = "<missing operationId>"
			}
			if len(op.Tags) == 0 {
				missingTags = append(missingTags, fmt.Sprintf("%s %s (%s)", strings.ToUpper(method), path, opID))
				continue
			}
			for _, tag := range op.Tags {
				if _, ok := allowedOperationTags[tag]; ok {
					continue
				}
				unknownTags = append(unknownTags, fmt.Sprintf("%s %s (%s): %s", strings.ToUpper(method), path, opID, tag))
			}
		}
	}

	sort.Strings(missingTags)
	sort.Strings(unknownTags)

	if len(missingTags) > 0 {
		t.Fatalf("openapi operations without tags:\n%s", strings.Join(missingTags, "\n"))
	}
	if len(unknownTags) > 0 {
		t.Fatalf("openapi operations with unknown tags:\n%s", strings.Join(unknownTags, "\n"))
	}
}

func loadOpenAPISpec(t *testing.T) *openapi3.T {
	t.Helper()

	candidates := []string{
		filepath.Clean(filepath.Join("api", "openapi.yaml")),
		filepath.Clean(filepath.Join("..", "..", "..", "..", "api", "openapi.yaml")),
	}

	if _, thisFile, _, ok := runtime.Caller(0); ok && filepath.IsAbs(thisFile) {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "api", "openapi.yaml")))
	}

	specPath := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			specPath = candidate
			break
		}
	}
	if specPath == "" {
		t.Fatalf("failed to locate openapi spec, tried: %s", strings.Join(candidates, ", "))
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load openapi spec %s: %v", specPath, err)
	}
	return doc
}
