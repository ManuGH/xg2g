// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"encoding/json"
	"testing"
)

func TestStringOrNumberString_UnmarshalJSON(t *testing.T) {
	var s StringOrNumberString

	if err := json.Unmarshal([]byte(`123456`), &s); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if string(s) != "123456" {
		t.Fatalf("want 123456 got %q", string(s))
	}

	if err := json.Unmarshal([]byte(`"123456"`), &s); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if string(s) != "123456" {
		t.Fatalf("want 123456 got %q", string(s))
	}

	if err := json.Unmarshal([]byte(`null`), &s); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if string(s) != "" {
		t.Fatalf("want empty got %q", string(s))
	}
}

func TestIntOrStringInt64_UnmarshalJSON(t *testing.T) {
	var v IntOrStringInt64

	// Number
	if err := json.Unmarshal([]byte(`123456`), &v); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if int64(v) != 123456 {
		t.Fatalf("want 123456 got %d", int64(v))
	}

	// String number
	if err := json.Unmarshal([]byte(`"123456"`), &v); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if int64(v) != 123456 {
		t.Fatalf("want 123456 got %d", int64(v))
	}

	// Empty string
	if err := json.Unmarshal([]byte(`""`), &v); err != nil {
		t.Fatalf("unmarshal empty string: %v", err)
	}
	if int64(v) != 0 {
		t.Fatalf("want 0 got %d", int64(v))
	}

	// Null
	if err := json.Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if int64(v) != 0 {
		t.Fatalf("want 0 got %d", int64(v))
	}
}

func TestBookmarkList_UnmarshalJSON(t *testing.T) {
	var bl BookmarkList

	// 1. Array of Objects
	jsonObjs := `[{"path":"/hdd/movie","name":"HDD"},{"path":"/hdd/timeshift","name":"Timeshift"}]`
	if err := json.Unmarshal([]byte(jsonObjs), &bl); err != nil {
		t.Fatalf("unmarshal objects: %v", err)
	}
	if len(bl) != 2 || bl[0].Name != "HDD" {
		t.Fatalf("objects parsed incorrectly: %+v", bl)
	}

	// 2. Array of Strings
	jsonStrs := `["/hdd/movie", "/hdd/timeshift"]`
	if err := json.Unmarshal([]byte(jsonStrs), &bl); err != nil {
		t.Fatalf("unmarshal strings: %v", err)
	}
	if len(bl) != 2 || bl[0].Path != "/hdd/movie" || bl[0].Name != "movie" {
		t.Fatalf("strings parsed incorrectly: %+v", bl)
	}

	// 3. Single Object (Edge Case)
	jsonSingle := `{"path":"/hdd/movie","name":"HDD"}`
	if err := json.Unmarshal([]byte(jsonSingle), &bl); err != nil {
		t.Fatalf("unmarshal single object: %v", err)
	}
	if len(bl) != 1 || bl[0].Name != "HDD" {
		t.Fatalf("single object parsed incorrectly: %+v", bl)
	}

	// 4. Empty String
	jsonEmpty := `""`
	if err := json.Unmarshal([]byte(jsonEmpty), &bl); err != nil {
		t.Fatalf("unmarshal empty string: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("expected empty list, got %d", len(bl))
	}

	// 5. Null
	if err := json.Unmarshal([]byte(`null`), &bl); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("expected empty list, got %d", len(bl))
	}
}
