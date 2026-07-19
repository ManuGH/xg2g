// Command gen-operation-catalog generates the runtime route and authorization
// catalog from the canonical OpenAPI document.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

const policiesExtension = "x-xg2g-operation-policies"

type operationPolicy struct {
	Class        string `json:"class"`
	Auth         string `json:"auth"`
	BrowserTrust string `json:"browserTrust"`
	RateLimit    string `json:"rateLimit"`
	Audit        bool   `json:"audit"`
	RedactErrors bool   `json:"redactErrors"`
}

type operationEntry struct {
	ID     string
	Method string
	Path   string
	Scopes []string
	Policy operationPolicy
}

func main() {
	specPath := flag.String("spec", "api/openapi.yaml", "canonical OpenAPI document")
	authzOut := flag.String("authz-out", "internal/control/authz/operation_catalog_gen.go", "generated authorization catalog")
	routesOut := flag.String("routes-out", "internal/control/http/v3/operation_routes_gen.go", "generated route catalog")
	flag.Parse()

	entries, err := loadCatalog(*specPath)
	if err != nil {
		fatal(err)
	}
	if err := writeFormatted(*authzOut, renderAuthz(entries)); err != nil {
		fatal(err)
	}
	if err := writeFormatted(*routesOut, renderRoutes(entries)); err != nil {
		fatal(err)
	}
}

func loadCatalog(specPath string) ([]operationEntry, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI: %w", err)
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("OpenAPI document is missing paths")
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("validate OpenAPI: %w", err)
	}

	policies, err := decodePolicies(doc.Extensions[policiesExtension])
	if err != nil {
		return nil, err
	}

	entries := make([]operationEntry, 0, len(policies))
	seenIDs := make(map[string]string)
	seenRoutes := make(map[string]string)
	paths := make([]string, 0, len(doc.Paths.Map()))
	for path := range doc.Paths.Map() {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pathItem := doc.Paths.Find(path)
		if pathItem == nil {
			continue
		}
		operations := pathItem.Operations()
		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, strings.ToUpper(method))
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := operations[method]
			if op == nil || strings.TrimSpace(op.OperationID) == "" {
				return nil, fmt.Errorf("%s %s: operationId is required", method, path)
			}
			id := codegen.ToCamelCase(op.OperationID)
			if previous, ok := seenIDs[id]; ok {
				return nil, fmt.Errorf("duplicate canonical operation id %s: %s and %s %s", id, previous, method, path)
			}
			routeKey := method + " " + path
			if previous, ok := seenRoutes[routeKey]; ok {
				return nil, fmt.Errorf("duplicate route %s for %s and %s", routeKey, previous, id)
			}
			policy, ok := policies[id]
			if !ok {
				return nil, fmt.Errorf("%s %s (%s): missing %s entry", method, path, id, policiesExtension)
			}
			scopes, err := resolveScopes(doc.Security, op.Security)
			if err != nil {
				return nil, fmt.Errorf("%s %s (%s): %w", method, path, id, err)
			}
			if err := validatePolicy(id, scopes, policy); err != nil {
				return nil, err
			}

			seenIDs[id] = routeKey
			seenRoutes[routeKey] = id
			entries = append(entries, operationEntry{ID: id, Method: method, Path: path, Scopes: scopes, Policy: policy})
		}
	}

	for id := range policies {
		if _, ok := seenIDs[id]; !ok {
			return nil, fmt.Errorf("%s entry references unknown operation %s", policiesExtension, id)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

func decodePolicies(raw any) (map[string]operationPolicy, error) {
	if raw == nil {
		return nil, fmt.Errorf("OpenAPI root extension %s is required", policiesExtension)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", policiesExtension, err)
	}
	var policies map[string]operationPolicy
	if err := json.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("decode %s: %w", policiesExtension, err)
	}
	if len(policies) == 0 {
		return nil, fmt.Errorf("OpenAPI root extension %s must not be empty", policiesExtension)
	}
	return policies, nil
}

func resolveScopes(root openapi3.SecurityRequirements, operation *openapi3.SecurityRequirements) ([]string, error) {
	requirements := root
	if operation != nil {
		requirements = *operation
	}
	if len(requirements) == 0 {
		return []string{}, nil
	}
	if len(requirements) != 1 {
		return nil, fmt.Errorf("exactly one security requirement is supported, got %d", len(requirements))
	}
	requirement := requirements[0]
	if len(requirement) != 1 {
		return nil, fmt.Errorf("exactly one security scheme is supported, got %d", len(requirement))
	}
	scopes, ok := requirement["BearerAuth"]
	if !ok {
		return nil, fmt.Errorf("unsupported security scheme; expected BearerAuth")
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("BearerAuth must declare at least one scope")
	}
	return append([]string{}, scopes...), nil
}

func validatePolicy(id string, scopes []string, policy operationPolicy) error {
	if !contains([]string{"read", "write", "admin", "device", "pairing", "session", "health", "system"}, policy.Class) {
		return fmt.Errorf("%s: invalid exposure class %q", id, policy.Class)
	}
	if !contains([]string{"bearer_scope", "none", "pairing_secret", "device_grant", "bootstrap_token"}, policy.Auth) {
		return fmt.Errorf("%s: invalid auth kind %q", id, policy.Auth)
	}
	if !contains([]string{"same_origin_or_allowed_origin", "not_browser", "none"}, policy.BrowserTrust) {
		return fmt.Errorf("%s: invalid browser trust %q", id, policy.BrowserTrust)
	}
	if !contains([]string{"none", "global", "auth", "pairing_start", "pairing_poll", "pairing_secret", "device_grant", "web_bootstrap"}, policy.RateLimit) {
		return fmt.Errorf("%s: invalid rate limit class %q", id, policy.RateLimit)
	}
	if policy.Auth == "bearer_scope" && len(scopes) == 0 {
		return fmt.Errorf("%s: bearer_scope requires OpenAPI BearerAuth scopes", id)
	}
	if policy.Auth != "bearer_scope" && len(scopes) != 0 {
		return fmt.Errorf("%s: %s auth must use OpenAPI security: []", id, policy.Auth)
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func renderAuthz(entries []operationEntry) []byte {
	var out bytes.Buffer
	out.WriteString("// Code generated by gen-operation-catalog from api/openapi.yaml; DO NOT EDIT.\n\n")
	out.WriteString("package authz\n\n")
	out.WriteString("var operationScopes = map[string][]string{\n")
	for _, entry := range entries {
		fmt.Fprintf(&out, "\t%q: {", entry.ID)
		for i, scope := range entry.Scopes {
			if i > 0 {
				out.WriteString(", ")
			}
			fmt.Fprintf(&out, "%q", scope)
		}
		out.WriteString("},\n")
	}
	out.WriteString("}\n\n")
	out.WriteString("var unscopedOperations = map[string]struct{}{\n")
	for _, entry := range entries {
		if len(entry.Scopes) == 0 {
			fmt.Fprintf(&out, "\t%q: {},\n", entry.ID)
		}
	}
	out.WriteString("}\n\n")
	out.WriteString("var operationExposurePolicies = map[string]ExposurePolicy{\n")
	for _, entry := range entries {
		p := entry.Policy
		fmt.Fprintf(&out, "\t%q: {Class: ExposureClass(%q), AuthKind: ExposureAuthKind(%q), BrowserTrust: ExposureBrowserTrust(%q), RateLimitClass: ExposureRateLimitClass(%q), AuditRequired: %t, RedactErrors: %t},\n", entry.ID, p.Class, p.Auth, p.BrowserTrust, p.RateLimit, p.Audit, p.RedactErrors)
	}
	out.WriteString("}\n")
	return out.Bytes()
}

func renderRoutes(entries []operationEntry) []byte {
	var out bytes.Buffer
	out.WriteString("// Code generated by gen-operation-catalog from api/openapi.yaml; DO NOT EDIT.\n\n")
	out.WriteString("package v3\n\n")
	out.WriteString("var operationRoutes = map[string]operationRoute{\n")
	for _, entry := range entries {
		fmt.Fprintf(&out, "\t%q: {Method: %q, Path: %q},\n", entry.ID, entry.Method, entry.Path)
	}
	out.WriteString("}\n\n")
	out.WriteString("func registerGeneratedRoutes(register routeRegistrar, handler *ServerInterfaceWrapper) {\n")
	for _, entry := range entries {
		fmt.Fprintf(&out, "\tregister.add(%q, handler.%s)\n", entry.ID, entry.ID)
	}
	out.WriteString("}\n")
	return out.Bytes()
}

func writeFormatted(path string, source []byte) error {
	formatted, err := format.Source(source)
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, formatted, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen-operation-catalog:", err)
	os.Exit(1)
}
