package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
)

// #nosec G101 //nolint:gosec // G101: this is a configuration schema key mapping, not a hardcoded credential
var openWebIFAliases = map[string]string{
	"enigma2.baseUrl":         "openWebIF.baseUrl",
	"enigma2.username":        "openWebIF.username",
	"enigma2.password":        "openWebIF.password",
	"enigma2.timeout":         "openWebIF.timeout",
	"enigma2.retries":         "openWebIF.retries",
	"enigma2.backoff":         "openWebIF.backoff",
	"enigma2.maxBackoff":      "openWebIF.maxBackoff",
	"enigma2.streamPort":      "openWebIF.streamPort",
	"enigma2.useWebIFStreams": "openWebIF.useWebIFStreams",
}

func configPathsForEntry(entry config.ConfigEntry) []string {
	if entry.Path == "" {
		return nil
	}
	return []string{entry.Path}
}

func isAliasPath(entry config.ConfigEntry, path string) bool {
	alias, ok := openWebIFAliases[entry.Path]
	return ok && alias == path
}

func examplePathsForEntry(entry config.ConfigEntry) []string {
	return configPathsForEntry(entry)
}

func updateSchemaDefaults(root string, entries []config.ConfigEntry, allowCreate bool) error {
	path := filepath.Join(root, configSchemaPath)
	// #nosec G304 -- CLI tool, path provided by user argument
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	for _, entry := range entries {
		for _, path := range configPathsForEntry(entry) {
			if err := setSchemaDefault(schema, entry, path, allowCreate); err != nil {
				return err
			}
		}
	}
	stripLegacyOpenWebIFSchema(schema)
	stripLegacyMonetizationSchema(schema)

	out, err := marshalSortedJSON(schema)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return fmt.Errorf("write schema: %w", err)
	}
	return nil
}

func stripLegacyOpenWebIFSchema(schema map[string]any) {
	if props, ok := schema["properties"].(map[string]any); ok {
		delete(props, "openWebIF")
		if enigma2, ok := props["enigma2"].(map[string]any); ok {
			if enigma2Props, ok := enigma2["properties"].(map[string]any); ok {
				if baseURL, ok := enigma2Props["baseUrl"].(map[string]any); ok {
					baseURL["description"] = "Base URL of the Enigma2 receiver"
				}
			}
		}
	}

	if examples, ok := schema["examples"].([]any); ok {
		for _, item := range examples {
			example, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if legacy, ok := example["openWebIF"]; ok {
				if _, exists := example["enigma2"]; !exists {
					example["enigma2"] = legacy
				}
				delete(example, "openWebIF")
			}
		}
	}

	required, ok := schema["required"].([]any)
	if !ok {
		return
	}
	filtered := make([]any, 0, len(required))
	hasEnigma2 := false
	for _, item := range required {
		key, ok := item.(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if key == "openWebIF" {
			continue
		}
		if key == "enigma2" {
			hasEnigma2 = true
		}
		filtered = append(filtered, key)
	}
	if !hasEnigma2 {
		filtered = append(filtered, "enigma2")
	}
	schema["required"] = filtered
}

func stripLegacyMonetizationSchema(schema map[string]any) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	monetization, ok := props["monetization"].(map[string]any)
	if !ok {
		return
	}
	monetizationProps, ok := monetization["properties"].(map[string]any)
	if !ok {
		return
	}
	delete(monetizationProps, "unlockScope")
}

func setSchemaDefault(schema map[string]any, entry config.ConfigEntry, path string, allowCreate bool) error {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	schemaType := schemaTypeForEntry(entry)
	curr := schema
	for i, part := range parts {
		props, ok := curr["properties"].(map[string]any)
		if !ok {
			if !allowCreate {
				return fmt.Errorf("schema missing properties for %q (path: %s)", part, path)
			}
			props = map[string]any{}
			curr["properties"] = props
		}
		prop, ok := props[part].(map[string]any)
		if !ok {
			if !allowCreate {
				return fmt.Errorf("schema missing property %q (path: %s)", part, path)
			}
			prop = map[string]any{}
			props[part] = prop
		}
		if i == len(parts)-1 {
			if _, ok := prop["type"]; !ok && schemaType != "" {
				prop["type"] = schemaType
			}
			if schemaType == "array" {
				if _, ok := prop["items"]; !ok {
					prop["items"] = schemaItemsForEntry(entry)
				}
			}
			if entry.Default != nil {
				prop["default"] = normalizeDefault(entry.Default)
			}
			if path == "metrics.listenAddr" && entry.Default == "" {
				allowEmptyPattern(prop)
			}
			if isAliasPath(entry, path) {
				if _, ok := prop["deprecated"]; !ok {
					prop["deprecated"] = true
				}
			}
			return nil
		}
		if _, ok := prop["type"]; !ok {
			prop["type"] = "object"
		}
		if _, ok := prop["properties"]; !ok {
			prop["properties"] = map[string]any{}
		}
		curr = prop
	}
	return nil
}

func allowEmptyPattern(prop map[string]any) {
	pattern, ok := prop["pattern"].(string)
	if !ok || pattern == "" {
		return
	}
	if strings.HasPrefix(pattern, "^$|") {
		return
	}
	prop["pattern"] = "^$|" + pattern
}

func schemaTypeForEntry(entry config.ConfigEntry) string {
	if entry.Default != nil {
		return schemaTypeFromDefault(entry.Default)
	}
	if entry.FieldPath == "" {
		return "string"
	}
	t, ok := resolveFieldType(entry.FieldPath)
	if !ok {
		return "string"
	}
	return schemaTypeFromReflect(t)
}

func schemaTypeFromDefault(def any) string {
	switch def.(type) {
	case bool:
		return "boolean"
	case int, int64:
		return "integer"
	case float64, float32:
		return "number"
	case time.Duration:
		return "string"
	case []string:
		return "array"
	default:
		return "string"
	}
}

func schemaTypeFromReflect(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.PkgPath() == "time" && t.Name() == "Duration" {
		return "string"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

func schemaItemsForEntry(entry config.ConfigEntry) map[string]any {
	if entry.Default != nil {
		if t := reflect.TypeOf(entry.Default); t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			return map[string]any{"type": schemaTypeFromReflect(t.Elem())}
		}
	}
	if entry.FieldPath == "" {
		return map[string]any{"type": "string"}
	}
	t, ok := resolveFieldType(entry.FieldPath)
	if !ok {
		return map[string]any{"type": "string"}
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return map[string]any{"type": schemaTypeFromReflect(t.Elem())}
	}
	return map[string]any{"type": "string"}
}

func resolveFieldType(path string) (reflect.Type, bool) {
	cfgType := reflect.TypeFor[config.AppConfig]()
	parts := strings.Split(path, ".")
	curr := cfgType
	for _, part := range parts {
		if curr.Kind() == reflect.Pointer {
			curr = curr.Elem()
		}
		field, ok := curr.FieldByName(part)
		if !ok {
			return nil, false
		}
		curr = field.Type
	}
	return curr, true
}
