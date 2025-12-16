// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func isYAMLUnknownFieldError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "field") && strings.Contains(msg, "not found")
}

// normalizeLegacyYAML applies a small set of backward-compatible key mappings for config.yaml.
// It returns YAML bytes that can be decoded with KnownFields(true).
func normalizeLegacyYAML(data []byte) ([]byte, []string, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, false, fmt.Errorf("parse YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return data, nil, false, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return data, nil, false, nil
	}

	var warnings []string
	changed := changedKey(root, &warnings, "openwebif", "openWebIF")

	// Root-level key aliases.

	// Legacy: receiver: <baseUrl> or receiver: { ... } → openWebIF: ...
	if receiverVal, receiverKeyIdx := mappingGetCI(root, "receiver"); receiverVal != nil {
		if _, exists := mappingGet(root, "openWebIF"); exists {
			mappingDeleteAt(root, receiverKeyIdx)
			warnings = append(warnings, "ignored legacy key 'receiver' (openWebIF already configured)")
			changed = true
		} else {
			openWebIF := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			switch receiverVal.Kind {
			case yaml.MappingNode:
				openWebIF = receiverVal
			case yaml.ScalarNode:
				openWebIF.Content = append(openWebIF.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "baseUrl"},
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: receiverVal.Value},
				)
			default:
				// Unsupported legacy type; keep original strict error.
				return data, nil, false, nil
			}

			mappingDeleteAt(root, receiverKeyIdx)
			mappingSet(root, "openWebIF", openWebIF)
			warnings = append(warnings, "migrated legacy key 'receiver' → 'openWebIF'")
			changed = true
		}
	}

	// Legacy: bouquet: "Premium" or bouquet: [ ... ] → bouquets: [...]
	if bouquetVal, bouquetKeyIdx := mappingGetCI(root, "bouquet"); bouquetVal != nil {
		if _, exists := mappingGet(root, "bouquets"); exists {
			mappingDeleteAt(root, bouquetKeyIdx)
			warnings = append(warnings, "ignored legacy key 'bouquet' (bouquets already configured)")
			changed = true
		} else {
			var seq *yaml.Node
			switch bouquetVal.Kind {
			case yaml.SequenceNode:
				seq = bouquetVal
			case yaml.ScalarNode:
				seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: bouquetVal.Value})
			default:
				return data, nil, false, nil
			}

			mappingDeleteAt(root, bouquetKeyIdx)
			mappingSet(root, "bouquets", seq)
			warnings = append(warnings, "migrated legacy key 'bouquet' → 'bouquets'")
			changed = true
		}
	}

	// Legacy: listenAddr: ":8080" at root → api.listenAddr
	if listenVal, listenKeyIdx := mappingGetCI(root, "listenAddr"); listenVal != nil {
		if apiNode := ensureMapping(root, "api"); apiNode != nil {
			if _, ok := mappingGet(apiNode, "listenAddr"); !ok {
				mappingSet(apiNode, "listenAddr", listenVal)
				warnings = append(warnings, "migrated legacy key 'listenAddr' → 'api.listenAddr'")
				// changed = true

			}
			mappingDeleteAt(root, listenKeyIdx)
			changed = true
		}
	}

	// Legacy: xmltv/xmltvPath at root → epg.xmltvPath
	if xmltvVal, xmltvKeyIdx := mappingGetCI(root, "xmltv"); xmltvVal != nil {
		if epgNode := ensureMapping(root, "epg"); epgNode != nil {
			if _, ok := mappingGet(epgNode, "xmltvPath"); !ok {
				mappingSet(epgNode, "xmltvPath", xmltvVal)
				warnings = append(warnings, "migrated legacy key 'xmltv' → 'epg.xmltvPath'")
				// changed = true

			}
			mappingDeleteAt(root, xmltvKeyIdx)
			changed = true
		}
	}
	if xmltvVal, xmltvKeyIdx := mappingGetCI(root, "xmltvPath"); xmltvVal != nil {
		if epgNode := ensureMapping(root, "epg"); epgNode != nil {
			if _, ok := mappingGet(epgNode, "xmltvPath"); !ok {
				mappingSet(epgNode, "xmltvPath", xmltvVal)
				warnings = append(warnings, "migrated legacy key 'xmltvPath' → 'epg.xmltvPath'")
				// changed = true

			}
			mappingDeleteAt(root, xmltvKeyIdx)
			changed = true
		}
	}

	// Nested key aliases and type conversions.
	openWebIFVal, _ := mappingGet(root, "openWebIF")
	if openWebIFVal != nil && openWebIFVal.Kind == yaml.MappingNode {
		if changedKey(openWebIFVal, &warnings, "base", "baseUrl") {
			changed = true
		}
		if changedKey(openWebIFVal, &warnings, "baseURL", "baseUrl") {
			changed = true
		}
		if changedKey(openWebIFVal, &warnings, "user", "username") {
			changed = true
		}
		if changedKey(openWebIFVal, &warnings, "pass", "password") {
			changed = true
		}
		if changedKey(openWebIFVal, &warnings, "stream_port", "streamPort") {
			changed = true
		}
		if changedKey(openWebIFVal, &warnings, "useWebIF", "useWebIFStreams") {
			changed = true
		}

		if convertDurationMsKey(openWebIFVal, &warnings, "timeoutMs", "timeout") {
			changed = true
		}
		if convertDurationMsKey(openWebIFVal, &warnings, "timeout_ms", "timeout") {
			changed = true
		}
		if convertDurationMsKey(openWebIFVal, &warnings, "backoffMs", "backoff") {
			changed = true
		}
		if convertDurationMsKey(openWebIFVal, &warnings, "backoff_ms", "backoff") {
			changed = true
		}
		if convertDurationMsKey(openWebIFVal, &warnings, "maxBackoffMs", "maxBackoff") {
			changed = true
		}
		if convertDurationMsKey(openWebIFVal, &warnings, "max_backoff_ms", "maxBackoff") {
			changed = true
		}
	}

	apiVal, _ := mappingGet(root, "api")
	if apiVal != nil && apiVal.Kind == yaml.MappingNode {
		if changedKey(apiVal, &warnings, "addr", "listenAddr") {
			changed = true
		}
		if changedKey(apiVal, &warnings, "apiAddr", "listenAddr") {
			changed = true
		}
	}

	metricsVal, _ := mappingGet(root, "metrics")
	if metricsVal != nil && metricsVal.Kind == yaml.MappingNode {
		if changedKey(metricsVal, &warnings, "addr", "listenAddr") {
			changed = true
		}
		if changedKey(metricsVal, &warnings, "metricsAddr", "listenAddr") {
			changed = true
		}
	}

	epgVal, _ := mappingGet(root, "epg")
	if epgVal != nil && epgVal.Kind == yaml.MappingNode {
		if changedKey(epgVal, &warnings, "xmltv", "xmltvPath") {
			changed = true
		}
	}

	if !changed {
		return data, nil, false, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, warnings, true, fmt.Errorf("encode normalized YAML: %w", err)
	}
	_ = enc.Close()

	return buf.Bytes(), warnings, true, nil
}

func mappingGet(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i < len(m.Content)-1; i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

func mappingGetCI(m *yaml.Node, key string) (*yaml.Node, int) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, -1
	}
	for i := 0; i < len(m.Content)-1; i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.ScalarNode && strings.EqualFold(k.Value, key) {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

func mappingDeleteAt(m *yaml.Node, keyIdx int) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	if keyIdx < 0 || keyIdx+1 >= len(m.Content) {
		return
	}
	m.Content = append(m.Content[:keyIdx], m.Content[keyIdx+2:]...)
}

func mappingSet(m *yaml.Node, key string, value *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	if _, idx := mappingGetCI(m, key); idx != -1 {
		m.Content[idx].Value = key
		m.Content[idx+1] = value
		return
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func ensureMapping(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	if v, ok := mappingGet(root, key); ok {
		if v.Kind == yaml.MappingNode {
			return v
		}
		return nil
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mappingSet(root, key, m)
	return m
}

func changedKey(m *yaml.Node, warnings *[]string, from, to string) bool {
	if m == nil || m.Kind != yaml.MappingNode || from == to {
		return false
	}
	val, idx := mappingGetCI(m, from)
	if val == nil {
		return false
	}
	if _, toIdx := mappingGetCI(m, to); toIdx != -1 && toIdx != idx {
		mappingDeleteAt(m, idx)
		*warnings = append(*warnings, fmt.Sprintf("ignored legacy key %q (%q already configured)", from, to))
		return true
	}
	m.Content[idx].Value = to
	*warnings = append(*warnings, fmt.Sprintf("renamed legacy key %q → %q", from, to))
	return true
}

func convertDurationMsKey(m *yaml.Node, warnings *[]string, from, to string) bool {
	val, idx := mappingGetCI(m, from)
	if val == nil {
		return false
	}

	if _, toIdx := mappingGetCI(m, to); toIdx != -1 && toIdx != idx {
		mappingDeleteAt(m, idx)
		*warnings = append(*warnings, fmt.Sprintf("ignored legacy key %q (%q already configured)", from, to))
		return true
	}

	if val.Kind != yaml.ScalarNode {
		return false
	}

	ms, err := strconv.Atoi(strings.TrimSpace(val.Value))
	if err != nil {
		return false
	}

	m.Content[idx].Value = to
	m.Content[idx+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprintf("%dms", ms)}
	*warnings = append(*warnings, fmt.Sprintf("converted legacy key %q → %q (%dms)", from, to, ms))
	return true
}
