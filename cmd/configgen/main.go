// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	configDocPath              = "docs/guides/CONFIGURATION.md"
	configSchemaPath           = "docs/guides/config.schema.json"
	configGeneratedExamplePath = "config.generated.example.yaml"
	configInventory            = "docs/guides/CONFIG_SURFACES.md"
)

const (
	docBeginMarker = "<!-- BEGIN GENERATED CONFIG OPTIONS -->"
	docEndMarker   = "<!-- END GENERATED CONFIG OPTIONS -->"
)

func main() {
	allowCreate := flag.Bool("allow-create", false, "allow creating new schema nodes")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}

	registry, err := config.GetRegistry()
	if err != nil {
		fail(fmt.Errorf("get registry: %w", err))
	}
	entries := registryEntries(registry)

	if err := updateConfigDoc(root, entries); err != nil {
		fail(err)
	}
	if err := updateSchemaDefaults(root, entries, *allowCreate); err != nil {
		fail(err)
	}
	if err := writeGeneratedExample(root, entries); err != nil {
		fail(err)
	}
	if err := writeConfigInventory(root); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "configgen: %v\n", err)
	os.Exit(1)
}

func registryEntries(reg *config.Registry) []config.ConfigEntry {
	entries := make([]config.ConfigEntry, 0, len(reg.ByPath))
	for _, entry := range reg.ByPath {
		if entry.Path == "" {
			continue
		}
		if entry.Status == config.StatusInternal {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func updateConfigDoc(root string, entries []config.ConfigEntry) error {
	path := filepath.Join(root, configDocPath)
	// #nosec G304 -- CLI tool, path provided by user argument
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config doc: %w", err)
	}

	generated := buildConfigDoc(entries)
	out, err := replaceGeneratedSection(string(raw), generated)
	if err != nil {
		return fmt.Errorf("update config doc: %w", err)
	}

	if err := os.WriteFile(path, []byte(out), 0600); err != nil {
		return fmt.Errorf("write config doc: %w", err)
	}
	return nil
}

func buildConfigDoc(entries []config.ConfigEntry) string {
	grouped := make(map[string][]config.ConfigEntry)
	for _, entry := range entries {
		group := entry.Path
		if idx := strings.Index(group, "."); idx != -1 {
			group = group[:idx]
		} else {
			group = "root"
		}
		grouped[group] = append(grouped[group], entry)
	}

	groups := make([]string, 0, len(grouped))
	for group := range grouped {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	var b strings.Builder
	b.WriteString(docBeginMarker)
	b.WriteString("\n## Registry Options (Generated)\n\n")
	b.WriteString("This section is generated from `internal/config/registry.go`. Do not edit by hand.\n\n")

	for _, group := range groups {
		entries := grouped[group]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		b.WriteString(fmt.Sprintf("### %s\n\n", group))
		if group == "enigma2" {
			b.WriteString("Aliases: `openWebIF.*` (compat; prefer `enigma2.*`).\n\n")
		}
		b.WriteString("| Path | Env | Default | Status | Profile |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, entry := range entries {
			env := "-"
			if entry.Env != "" {
				env = fmt.Sprintf("`%s`", entry.Env)
			}
			def := "-"
			if entry.Default != nil {
				def = fmt.Sprintf("`%s`", formatDefault(entry.Default))
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
				entry.Path, env, def, entry.Status, entry.Profile))
		}
		b.WriteString("\n")
	}
	b.WriteString(docEndMarker)
	return b.String()
}

func replaceGeneratedSection(content string, generated string) (string, error) {
	start := strings.Index(content, docBeginMarker)
	end := strings.Index(content, docEndMarker)
	if start == -1 || end == -1 || end < start {
		return content + "\n\n" + generated + "\n", nil
	}
	end += len(docEndMarker)
	return content[:start] + generated + content[end:], nil
}

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

var generatedExampleFallbacks = map[string]any{
	"openWebIF.baseUrl": "http://127.0.0.1",
	"bouquets":          []string{},
}

func configPathsForEntry(entry config.ConfigEntry) []string {
	if entry.Path == "" {
		return nil
	}
	paths := []string{entry.Path}
	if alias, ok := openWebIFAliases[entry.Path]; ok && alias != entry.Path {
		paths = append(paths, alias)
	}
	return paths
}

func isAliasPath(entry config.ConfigEntry, path string) bool {
	alias, ok := openWebIFAliases[entry.Path]
	return ok && alias == path
}

func examplePathsForEntry(entry config.ConfigEntry) []string {
	if entry.Path == "" {
		return nil
	}
	if alias, ok := openWebIFAliases[entry.Path]; ok {
		return []string{alias}
	}
	return []string{entry.Path}
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

	out, err := marshalSortedJSON(schema)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return fmt.Errorf("write schema: %w", err)
	}
	return nil
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

func writeGeneratedExample(root string, entries []config.ConfigEntry) error {
	var rootNode yaml.Node
	rootNode.Kind = yaml.MappingNode
	rootNode.HeadComment = "yaml-language-server: $schema=./docs/guides/config.schema.json\nGenerated from internal/config/registry.go. Do not edit by hand."

	for _, entry := range entries {
		for _, path := range examplePathsForEntry(entry) {
			node, ok := exampleValueNode(entry, path)
			if !ok {
				continue
			}
			setYamlValue(&rootNode, strings.Split(path, "."), node)
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&rootNode); err != nil {
		return fmt.Errorf("encode generated example: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode generated example: %w", err)
	}
	path := filepath.Join(root, configGeneratedExamplePath)
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("write generated example: %w", err)
	}
	return nil
}

func exampleValueNode(entry config.ConfigEntry, path string) (*yaml.Node, bool) {
	if entry.Default != nil {
		return yamlNodeForValue(entry.Default), true
	}
	if fallback, ok := generatedExampleFallbacks[path]; ok {
		return yamlNodeForValue(fallback), true
	}
	return zeroValueNode(schemaTypeForEntry(entry)), true
}

func setYamlValue(node *yaml.Node, path []string, value *yaml.Node) {
	if node.Kind != yaml.MappingNode || len(path) == 0 {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Value != path[0] {
			continue
		}
		if len(path) == 1 {
			node.Content[i+1] = value
			return
		}
		if valNode.Kind != yaml.MappingNode {
			valNode.Kind = yaml.MappingNode
			valNode.Content = nil
			valNode.Tag = ""
		}
		setYamlValue(valNode, path[1:], value)
		return
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}
	var valNode *yaml.Node
	if len(path) == 1 {
		valNode = value
	} else {
		valNode = &yaml.Node{Kind: yaml.MappingNode}
		setYamlValue(valNode, path[1:], value)
	}
	node.Content = append(node.Content, keyNode, valNode)
}

func yamlScalar(def any) (string, string) {
	switch v := def.(type) {
	case string:
		return v, "!!str"
	case bool:
		return fmt.Sprintf("%t", v), "!!bool"
	case int:
		return fmt.Sprintf("%d", v), "!!int"
	case int64:
		return fmt.Sprintf("%d", v), "!!int"
	case float32:
		return fmt.Sprintf("%g", v), "!!float"
	case float64:
		return fmt.Sprintf("%g", v), "!!float"
	case time.Duration:
		return formatDuration(v), "!!str"
	default:
		return fmt.Sprintf("%v", v), "!!str"
	}
}

func yamlNodeForValue(def any) *yaml.Node {
	if def == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	}
	rv := reflect.ValueOf(def)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for i := 0; i < rv.Len(); i++ {
			seq.Content = append(seq.Content, yamlNodeForValue(rv.Index(i).Interface()))
		}
		return seq
	case reflect.Map:
		return &yaml.Node{Kind: yaml.MappingNode}
	}
	value, tag := yamlScalar(def)
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

func zeroValueNode(kind string) *yaml.Node {
	switch kind {
	case "boolean":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
	case "integer":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"}
	case "number":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "0"}
	case "array":
		return &yaml.Node{Kind: yaml.SequenceNode}
	case "object":
		return &yaml.Node{Kind: yaml.MappingNode}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	}
}

func normalizeDefault(def any) any {
	switch v := def.(type) {
	case time.Duration:
		return formatDuration(v)
	default:
		return def
	}
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
	if t.Kind() == reflect.Ptr {
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
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return map[string]any{"type": schemaTypeFromReflect(t.Elem())}
	}
	return map[string]any{"type": "string"}
}

func resolveFieldType(path string) (reflect.Type, bool) {
	cfgType := reflect.TypeOf(config.AppConfig{})
	parts := strings.Split(path, ".")
	curr := cfgType
	for _, part := range parts {
		if curr.Kind() == reflect.Ptr {
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

func formatDefault(def any) string {
	switch v := def.(type) {
	case string:
		if v == "" {
			return "\"\""
		}
		return v
	case time.Duration:
		return formatDuration(v)
	default:
		return fmt.Sprintf("%v", def)
	}
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	s := d.String()
	if strings.ContainsAny(s, "hm") {
		s = strings.TrimSuffix(s, "0s")
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func marshalSortedJSON(v any) ([]byte, error) {
	normalized := sortJSONValue(v)
	return json.MarshalIndent(normalized, "", "  ")
}

func sortJSONValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make(map[string]any, len(val))
		for _, k := range keys {
			ordered[k] = sortJSONValue(val[k])
		}
		return ordered
	case []any:
		out := make([]any, len(val))
		for i := range val {
			out[i] = sortJSONValue(val[i])
		}
		return out
	default:
		return v
	}
}

func writeConfigInventory(root string) error {
	files, err := trackedFiles(root)
	if err != nil {
		return fmt.Errorf("tracked files: %w", err)
	}

	sort.Strings(files)
	var b strings.Builder
	b.WriteString("# Configuration Surfaces Inventory (Generated)\n\n")
	b.WriteString("This file is generated by `cmd/configgen` and lists files referencing `XG2G_*` keys.\n\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("- `%s`\n", f))
	}

	path := filepath.Join(root, configInventory)
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	return nil
}

func trackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	paths := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(paths))
	for _, p := range paths {
		if len(p) == 0 {
			continue
		}
		rel := string(p)
		if !shouldScanFile(rel) {
			continue
		}
		abs := filepath.Join(root, rel)
		if fileContains(abs, "XG2G_") {
			files = append(files, rel)
		}
	}
	return files, nil
}

func shouldScanFile(path string) bool {
	base := filepath.Base(path)
	if base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".md", ".yaml", ".yml", ".json", ".env", ".txt", ".sh", ".service", ".ts", ".tsx", ".js", ".toml":
		return true
	default:
		return false
	}
}

func fileContains(path, needle string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Size() > 1024*1024 {
		return false
	}
	// #nosec G304 -- intended CLI behavior
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(needle))
}
