// SPDX-License-Identifier: MIT

// schema-docs generates Markdown documentation from JSON Schema files.
//
// Usage:
//
//	go run ./tools/schema-docs [input.json] [output.md]
//
// Defaults:
//   - input: docs/config.schema.json
//   - output: docs/config.md
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Schema map[string]any

type propInfo struct {
	Name        string
	Type        string
	Required    bool
	Description string
	Enum        []string
	Example     string
	Default     string
	Format      string
}

func main() {
	in := "docs/config.schema.json"
	out := "docs/config.md"
	if len(os.Args) > 1 {
		in = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}

	data, err := os.ReadFile(in)
	check(err)

	var root Schema
	check(json.Unmarshal(data, &root))

	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "# xg2g Konfiguration")
	fmt.Fprintln(buf, "> Quelle: `docs/config.schema.json` (Draft 2020-12)")

	// Root required
	reqSet := map[string]bool{}
	if req, ok := root["required"].([]any); ok {
		for _, r := range req {
			reqSet[fmt.Sprint(r)] = true
		}
	}

	// Root properties
	props := getMap(root, "properties")
	keys := sortedKeys(props)

	fmt.Fprintln(buf, "## Übersicht")
	fmt.Fprintln(buf, "| Feld | Typ | Pflicht | Beschreibung |")
	fmt.Fprintln(buf, "|---|---|:---:|---|")
	for _, k := range keys {
		p := extractProp(k, getMap(props, k), reqSet[k])
		fmt.Fprintf(buf, "| `%s` | %s | %s | %s |\n",
			p.Name, mdCode(p.Type), boolIcon(p.Required), mdSan(p.Description))
	}
	fmt.Fprintln(buf)

	// Detailabschnitte
	for _, k := range keys {
		pnode := getMap(props, k)
		fmt.Fprintf(buf, "## `%s`\n\n", k)
		renderNode(buf, k, pnode, reqSet[k], 3)
		fmt.Fprintln(buf)
	}

	// Schreiben
	check(os.MkdirAll("docs", 0o755))
	check(os.WriteFile(out, buf.Bytes(), 0o644))
	fmt.Printf("generated %s from %s\n", out, in)
}

func renderNode(buf *bytes.Buffer, name string, node Schema, required bool, h int) {
	pi := extractProp(name, node, required)

	fmt.Fprintf(buf, "**Typ:** %s  \n", mdCode(pi.Type))
	fmt.Fprintf(buf, "**Pflicht:** %s  \n", boolIcon(pi.Required))
	if pi.Format != "" {
		fmt.Fprintf(buf, "**Format:** `%s`  \n", pi.Format)
	}
	if pi.Default != "" {
		fmt.Fprintf(buf, "**Default:** `%s`  \n", mdSan(pi.Default))
	}
	if pi.Enum != nil && len(pi.Enum) > 0 {
		fmt.Fprintf(buf, "**Erlaubte Werte:** %s  \n", strings.Join(wrapBackticks(pi.Enum), ", "))
	}
	if pi.Description != "" {
		fmt.Fprintf(buf, "\n%s\n", mdSan(pi.Description))
	}
	if pi.Example != "" {
		fmt.Fprintf(buf, "\n**Beispiel:**\n\n```yaml\n%s\n```\n", pi.Example)
	}

	t := typeOf(node)
	switch t {
	case "object":
		reqSet := map[string]bool{}
		if req, ok := node["required"].([]any); ok {
			for _, r := range req {
				reqSet[fmt.Sprint(r)] = true
			}
		}
		props := getMap(node, "properties")
		if len(props) == 0 {
			return
		}
		fmt.Fprintln(buf, "\n**Felder:**")
		fmt.Fprintln(buf, "| Feld | Typ | Pflicht | Beschreibung |")
		fmt.Fprintln(buf, "|---|---|:---:|---|")
		keys := sortedKeys(props)
		for _, k := range keys {
			child := extractProp(k, getMap(props, k), reqSet[k])
			fmt.Fprintf(buf, "| `%s` | %s | %s | %s |\n",
				child.Name, mdCode(child.Type), boolIcon(child.Required), mdSan(child.Description))
		}
		for _, k := range keys {
			fmt.Fprintf(buf, "\n### `%s.%s`\n\n", name, k)
			renderNode(buf, k, getMap(props, k), reqSet[k], h+1)
		}
	case "array":
		items := getMap(node, "items")
		if len(items) == 0 {
			return
		}
		fmt.Fprintln(buf, "\n**Elementtyp:**")
		renderNode(buf, name+"[]", items, false, h+1)
	}
}

func extractProp(name string, node Schema, required bool) propInfo {
	pi := propInfo{Name: name, Required: required}
	pi.Type = typeOf(node)
	if d, ok := node["description"].(string); ok {
		pi.Description = d
	}
	if f, ok := node["format"].(string); ok {
		pi.Format = f
	}
	if def, ok := node["default"]; ok {
		pi.Default = fmt.Sprint(def)
	}
	if ex, ok := node["examples"].([]any); ok && len(ex) > 0 {
		pi.Example = fmt.Sprint(ex[0])
	}
	if en, ok := node["enum"].([]any); ok {
		for _, v := range en {
			pi.Enum = append(pi.Enum, fmt.Sprint(v))
		}
	}
	// Arrays mit Item-Typ
	if pi.Type == "array" {
		items := getMap(node, "items")
		pi.Type = "array<" + typeOf(items) + ">"
	}
	return pi
}

func typeOf(node Schema) string {
	if t, ok := node["type"]; ok {
		switch v := t.(type) {
		case string:
			return v
		case []any:
			var parts []string
			for _, e := range v {
				parts = append(parts, fmt.Sprint(e))
			}
			sort.Strings(parts)
			return strings.Join(parts, "|")
		}
	}
	// object ohne type gilt als object
	if _, ok := node["properties"]; ok {
		return "object"
	}
	return "any"
}

func getMap(m Schema, key string) Schema {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return Schema(mm)
		}
	}
	return Schema{}
}

func sortedKeys(m Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mdSan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func mdCode(s string) string {
	return "`" + s + "`"
}

func wrapBackticks(v []string) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = "`" + s + "`"
	}
	return out
}

func boolIcon(b bool) string {
	if b {
		return "✓"
	}
	return ""
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
