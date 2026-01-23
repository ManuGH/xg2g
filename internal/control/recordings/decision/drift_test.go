package decision

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestGate_DecisionInput_Drift enforces ADR-009 (Schema Freeze).
// Any change to the Input struct shape (fields, tags, order) will break this test.
// To fix: Update ADR-009, then update the GOLDEN_HASH here.
func TestGate_DecisionInput_Drift(t *testing.T) {
	// 1. Reflect on DecisionInput struct
	inputType := reflect.TypeOf(DecisionInput{})

	// 2. Build canonical schema string
	schema := buildSchemaString(inputType, "")

	// 3. Hash it
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(schema)))

	// 4. Compare with Golden Hash
	// CURRENT GOLDEN HASH (Phase 2 Rename + Freeze)
	const GOLDEN_HASH = "74889d57e52f587088a95f30252e71cbd20bfe500b54af9fba948177e02f84d7"

	if hash != GOLDEN_HASH {
		t.Errorf("🛑 STOP THE LINE 🛑\nDecisionInput schema drift detected!\n\nExpected Hash: %s\nActual Hash:   %s\n\nSchema Dump:\n%s\n\nTo fix:\n1. Revert changes to DecisionInput OR\n2. Update docs/adr/009-playback-decision-spec.md to reflect changes AND\n3. Update GOLDEN_HASH in this test.", GOLDEN_HASH, hash, schema)
	}
}

func buildSchemaString(t reflect.Type, prefix string) string {
	var b strings.Builder

	// If it's a struct, iterate fields
	if t.Kind() == reflect.Struct {
		b.WriteString(fmt.Sprintf("%sStruct: %s\n", prefix, t.Name()))
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			// Skip private fields if we want to be nice, but for strict freeze we include all
			tag := f.Tag.Get("json")
			b.WriteString(fmt.Sprintf("%s  Field: %s Type: %s Tag: %s\n", prefix, f.Name, f.Type.Name(), tag))

			// Recurse for nested structs (Source, Capabilities, Policy)
			if f.Type.Kind() == reflect.Struct {
				b.WriteString(buildSchemaString(f.Type, prefix+"    "))
			} else if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct {
				// Slice of structs?
				b.WriteString(buildSchemaString(f.Type.Elem(), prefix+"    "))
			} else if f.Type.Kind() == reflect.Ptr && f.Type.Elem().Kind() == reflect.Struct {
				// Pointer to struct
				b.WriteString(buildSchemaString(f.Type.Elem(), prefix+"    "))
			}
		}
	}
	return b.String()
}
