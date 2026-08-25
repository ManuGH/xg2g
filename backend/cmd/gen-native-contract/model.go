// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// The intermediate representation deliberately covers only the constructs the
// native contract actually uses. Anything else is an error rather than a
// best-effort translation: a model layer that silently guesses is worse than no
// model layer, because it looks authoritative while being wrong.
type kind int

const (
	kindString kind = iota
	kindInt32
	kindInt64
	kindDouble
	kindBool
	kindDateTime
	kindNamedEnum
	kindNamedObject
	kindArray
	kindMap
)

type fieldType struct {
	kind kind
	// name is the referenced component for kindNamedEnum and kindNamedObject.
	name string
	// elem is the element type for kindArray, or value type for kindMap.
	elem *fieldType
}

type field struct {
	wire     string
	doc      string
	required bool
	typ      fieldType
}

type objectModel struct {
	name   string
	doc    string
	fields []field
}

type enumModel struct {
	name   string
	doc    string
	values []string
}

// contract is the closed set of models emitted for the native clients: every
// object and every enum reachable from the declared roots, in a stable order.
type contract struct {
	objects []objectModel
	enums   []enumModel
}

type extractor struct {
	schemas openapi3.Schemas
	objects map[string]objectModel
	enums   map[string]enumModel
	// visiting guards against a schema that references itself through a chain
	// of $refs, which would otherwise recurse until the stack gives out.
	visiting map[string]bool
}

func extract(doc *openapi3.T, roots []string) (*contract, error) {
	if doc.Components == nil || len(doc.Components.Schemas) == 0 {
		return nil, fmt.Errorf("OpenAPI document declares no component schemas")
	}

	ex := &extractor{
		schemas:  doc.Components.Schemas,
		objects:  map[string]objectModel{},
		enums:    map[string]enumModel{},
		visiting: map[string]bool{},
	}

	for _, root := range roots {
		if err := ex.visit(root); err != nil {
			return nil, err
		}
	}

	out := &contract{}
	for _, model := range ex.objects {
		out.objects = append(out.objects, model)
	}
	for _, model := range ex.enums {
		out.enums = append(out.enums, model)
	}
	sort.Slice(out.objects, func(i, j int) bool { return out.objects[i].name < out.objects[j].name })
	sort.Slice(out.enums, func(i, j int) bool { return out.enums[i].name < out.enums[j].name })
	return out, nil
}

func (e *extractor) visit(name string) error {
	if _, done := e.objects[name]; done {
		return nil
	}
	if _, done := e.enums[name]; done {
		return nil
	}
	if e.visiting[name] {
		return nil
	}

	ref, ok := e.schemas[name]
	if !ok || ref.Value == nil {
		return fmt.Errorf("schema %q is not defined in components.schemas", name)
	}
	schema := ref.Value

	if err := rejectUnsupported(name, schema); err != nil {
		return err
	}

	e.visiting[name] = true
	defer delete(e.visiting, name)

	switch {
	case len(schema.Enum) > 0:
		values, err := enumValues(name, schema)
		if err != nil {
			return err
		}
		e.enums[name] = enumModel{name: name, doc: schema.Description, values: values}
		return nil

	case schema.Type.Is("object"):
		model, err := e.object(name, schema)
		if err != nil {
			return err
		}
		e.objects[name] = model
		return nil

	default:
		return fmt.Errorf("schema %q: only object and string-enum schemas can be part of the native contract", name)
	}
}

func (e *extractor) object(name string, schema *openapi3.Schema) (objectModel, error) {
	required := map[string]bool{}
	for _, key := range schema.Required {
		if _, ok := schema.Properties[key]; !ok {
			return objectModel{}, fmt.Errorf("schema %q: required property %q has no definition", name, key)
		}
		required[key] = true
	}

	wireNames := make([]string, 0, len(schema.Properties))
	for wire := range schema.Properties {
		wireNames = append(wireNames, wire)
	}
	sort.Strings(wireNames)

	model := objectModel{name: name, doc: schema.Description}
	for _, wire := range wireNames {
		property := schema.Properties[wire]
		typ, err := e.fieldType(name, wire, property)
		if err != nil {
			return objectModel{}, err
		}
		doc := ""
		if property.Value != nil {
			doc = property.Value.Description
		}
		model.fields = append(model.fields, field{
			wire:     wire,
			doc:      doc,
			required: required[wire],
			typ:      typ,
		})
	}
	return model, nil
}

func (e *extractor) fieldType(owner, wire string, ref *openapi3.SchemaRef) (fieldType, error) {
	if ref == nil || ref.Value == nil {
		return fieldType{}, fmt.Errorf("schema %q: property %q has no schema", owner, wire)
	}

	if refName, ok := componentName(ref.Ref); ok {
		if err := e.visit(refName); err != nil {
			return fieldType{}, err
		}
		if _, isEnum := e.enums[refName]; isEnum {
			return fieldType{kind: kindNamedEnum, name: refName}, nil
		}
		if _, isObject := e.objects[refName]; isObject {
			return fieldType{kind: kindNamedObject, name: refName}, nil
		}
		// Reached only for a self-referential schema still on the stack, which
		// can only be an object: an enum has no properties to recurse through.
		return fieldType{kind: kindNamedObject, name: refName}, nil
	}

	schema := ref.Value
	if err := rejectUnsupported(owner+"."+wire, schema); err != nil {
		return fieldType{}, err
	}

	switch {
	case len(schema.Enum) > 0:
		// Inline enums are named the way oapi-codegen already names them in
		// server_gen.go — owner plus property — so the same wire enum carries
		// the same type name in Go, Swift and Kotlin.
		name, err := e.inlineEnum(owner, wire, schema)
		if err != nil {
			return fieldType{}, err
		}
		return fieldType{kind: kindNamedEnum, name: name}, nil

	case schema.Type.Is("array"):
		elem, err := e.fieldType(owner, wire+"[]", schema.Items)
		if err != nil {
			return fieldType{}, err
		}
		return fieldType{kind: kindArray, elem: &elem}, nil

	case schema.Type.Is("string"):
		switch schema.Format {
		case "date-time":
			return fieldType{kind: kindDateTime}, nil
		case "", "uri", "uuid", "byte", "email", "hostname":
			return fieldType{kind: kindString}, nil
		default:
			return fieldType{}, fmt.Errorf("schema %q: property %q has unsupported string format %q", owner, wire, schema.Format)
		}

	case schema.Type.Is("integer"):
		switch schema.Format {
		case "int64":
			return fieldType{kind: kindInt64}, nil
		case "", "int32":
			return fieldType{kind: kindInt32}, nil
		default:
			return fieldType{}, fmt.Errorf("schema %q: property %q has unsupported integer format %q", owner, wire, schema.Format)
		}

	case schema.Type.Is("number"):
		return fieldType{kind: kindDouble}, nil

	case schema.Type.Is("boolean"):
		return fieldType{kind: kindBool}, nil

	case schema.Type.Is("object") && schema.AdditionalProperties.Schema != nil:
		elem, err := e.fieldType(owner, wire+"{}", schema.AdditionalProperties.Schema)
		if err != nil {
			return fieldType{}, err
		}
		return fieldType{kind: kindMap, elem: &elem}, nil

	case schema.Type.Is("object"):
		return fieldType{}, fmt.Errorf(
			"schema %q: property %q is an inline object; promote it to a named component schema so both clients get the same type name",
			owner, wire)

	default:
		return fieldType{}, fmt.Errorf("schema %q: property %q has unsupported type %v", owner, wire, schema.Type)
	}
}

// inlineEnum registers an enum declared inline on a property under the name
// oapi-codegen gives it, and returns that name.
func (e *extractor) inlineEnum(owner, wire string, schema *openapi3.Schema) (string, error) {
	name := owner + pascalCase(strings.ReplaceAll(wire, "[]", ""))

	if _, taken := e.schemas[name]; taken {
		return "", fmt.Errorf(
			"schema %q: inline enum on property %q would generate the name %q, which is already a component schema; rename one of them",
			owner, wire, name)
	}

	values, err := enumValues(name, schema)
	if err != nil {
		return "", err
	}

	if existing, ok := e.enums[name]; ok {
		if !equalStrings(existing.values, values) {
			return "", fmt.Errorf("schema %q: inline enum %q is declared twice with different values", owner, name)
		}
		return name, nil
	}

	e.enums[name] = enumModel{name: name, doc: schema.Description, values: values}
	return name, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rejectUnsupported refuses the schema constructs the renderers cannot express
// faithfully. Failing here is the point: an unsupported construct that reaches
// a renderer becomes a plausible-looking model that decodes the wrong bytes.
func rejectUnsupported(where string, schema *openapi3.Schema) error {
	switch {
	case len(schema.AllOf) > 0:
		return fmt.Errorf("schema %q: allOf is not supported by the native contract generator", where)
	case len(schema.OneOf) > 0:
		return fmt.Errorf("schema %q: oneOf is not supported by the native contract generator", where)
	case len(schema.AnyOf) > 0:
		return fmt.Errorf("schema %q: anyOf is not supported by the native contract generator", where)
	case schema.Not != nil:
		return fmt.Errorf("schema %q: not is not supported by the native contract generator", where)
	case schema.Nullable:
		return fmt.Errorf("schema %q: nullable is not supported by the native contract generator", where)
	}

	if schema.Type.Is("object") && schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has {
		return fmt.Errorf("schema %q: free-form objects (additionalProperties: true) are not supported by the native contract generator", where)
	}
	return nil
}

func enumValues(name string, schema *openapi3.Schema) ([]string, error) {
	if !schema.Type.Is("string") {
		return nil, fmt.Errorf("schema %q: only string enums are supported, got %v", name, schema.Type)
	}
	values := make([]string, 0, len(schema.Enum))
	for _, raw := range schema.Enum {
		text, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("schema %q: enum value %v is not a string", name, raw)
		}
		values = append(values, text)
	}
	return values, nil
}

func componentName(ref string) (string, bool) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	return strings.TrimPrefix(ref, prefix), true
}
