package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"gopkg.in/yaml.v3"
)

var generatedExampleFallbacks = map[string]any{
	"enigma2.baseUrl": "http://127.0.0.1",
	"api.tokenScopes": []string{"v3:read"},
	"bouquets":        []string{},
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
