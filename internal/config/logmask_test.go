// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"reflect"
	"testing"
)

func TestMaskSecrets_SimpleMap(t *testing.T) {
	input := map[string]any{
		"username": "admin",
		"password": "secret123",
		"host":     "example.com",
	}

	result := MaskSecrets(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected result to be a map")
	}

	if resultMap["username"] != "admin" {
		t.Errorf("expected username to be preserved, got %v", resultMap["username"])
	}
	if resultMap["password"] != "***" {
		t.Errorf("expected password to be masked, got %v", resultMap["password"])
	}
	if resultMap["host"] != "example.com" {
		t.Errorf("expected host to be preserved, got %v", resultMap["host"])
	}
}

func TestMaskSecrets_NestedMap(t *testing.T) {
	input := map[string]any{
		"openWebIF": map[string]any{
			"baseUrl":  "http://receiver.local",
			"username": "root",
			"password": "dreambox",
		},
		"api": map[string]any{
			"token":      "my-secret-token",
			"listenAddr": ":8080",
		},
	}

	result := MaskSecrets(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected result to be a map")
	}

	openWebIF, ok := resultMap["openWebIF"].(map[string]any)
	if !ok {
		t.Fatal("expected openWebIF to be a map")
	}

	if openWebIF["password"] != "***" {
		t.Errorf("expected openWebIF.password to be masked, got %v", openWebIF["password"])
	}
	if openWebIF["username"] != "root" {
		t.Errorf("expected openWebIF.username to be preserved, got %v", openWebIF["username"])
	}

	api, ok := resultMap["api"].(map[string]any)
	if !ok {
		t.Fatal("expected api to be a map")
	}

	if api["token"] != "***" {
		t.Errorf("expected api.token to be masked, got %v", api["token"])
	}
	if api["listenAddr"] != ":8080" {
		t.Errorf("expected api.listenAddr to be preserved, got %v", api["listenAddr"])
	}
}

func TestMaskSecrets_Struct(t *testing.T) {
	type TestConfig struct {
		Username string
		Password string
		Host     string
	}

	input := TestConfig{
		Username: "admin",
		Password: "secret123",
		Host:     "example.com",
	}

	result := MaskSecrets(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected result to be a map")
	}

	if resultMap["Username"] != "admin" {
		t.Errorf("expected Username to be preserved, got %v", resultMap["Username"])
	}
	if resultMap["Password"] != "***" {
		t.Errorf("expected Password to be masked, got %v", resultMap["Password"])
	}
	if resultMap["Host"] != "example.com" {
		t.Errorf("expected Host to be preserved, got %v", resultMap["Host"])
	}
}

func TestMaskSecrets_FileConfig(t *testing.T) {
	input := &FileConfig{
		Version: "1",
		DataDir: "/var/lib/xg2g",
		OpenWebIF: OpenWebIFConfig{
			BaseURL:  "http://receiver.local",
			Username: "root",
			Password: "dreambox",
		},
		API: APIConfig{
			Token:      "my-secret-token",
			ListenAddr: ":8080",
		},
	}

	result := MaskSecrets(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected result to be a map")
	}

	// Check OpenWebIF
	openWebIF, ok := resultMap["OpenWebIF"].(map[string]any)
	if !ok {
		t.Fatal("expected OpenWebIF to be a map")
	}

	if openWebIF["Password"] != "***" {
		t.Errorf("expected OpenWebIF.Password to be masked, got %v", openWebIF["Password"])
	}
	if openWebIF["Username"] != "root" {
		t.Errorf("expected OpenWebIF.Username to be preserved, got %v", openWebIF["Username"])
	}

	// Check API
	api, ok := resultMap["API"].(map[string]any)
	if !ok {
		t.Fatal("expected API to be a map")
	}

	if api["Token"] != "***" {
		t.Errorf("expected API.Token to be masked, got %v", api["Token"])
	}
}

func TestMaskSecrets_Slice(t *testing.T) {
	input := []map[string]any{
		{
			"name":     "config1",
			"password": "secret1",
		},
		{
			"name":     "config2",
			"password": "secret2",
		},
	}

	result := MaskSecrets(input)
	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatal("expected result to be a slice")
	}

	if len(resultSlice) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(resultSlice))
	}

	for i, item := range resultSlice {
		itemMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected item %d to be a map", i)
		}

		if itemMap["password"] != "***" {
			t.Errorf("expected password in item %d to be masked, got %v", i, itemMap["password"])
		}
	}
}

func TestMaskSecrets_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		key   string
	}{
		{"lowercase", map[string]any{"password": "secret"}, "password"},
		{"uppercase", map[string]any{"PASSWORD": "secret"}, "PASSWORD"},
		{"mixedcase", map[string]any{"PassWord": "secret"}, "PassWord"},
		{"with_underscore", map[string]any{"api_key": "secret"}, "api_key"},
		{"with_Token", map[string]any{"apiToken": "secret"}, "apiToken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSecrets(tt.input)
			resultMap, ok := result.(map[string]any)
			if !ok {
				t.Fatal("expected result to be a map")
			}

			if resultMap[tt.key] != "***" {
				t.Errorf("expected %s to be masked, got %v", tt.key, resultMap[tt.key])
			}
		})
	}
}

func TestMaskSecrets_Nil(t *testing.T) {
	result := MaskSecrets(nil)
	if result != nil {
		t.Errorf("expected nil result for nil input, got %v", result)
	}
}

func TestMaskSecrets_Primitives(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"int", 42, 42},
		{"bool", true, true},
		{"float", 3.14, 3.14},
		{"string", "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSecrets(tt.input)
			if !reflect.DeepEqual(result, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, result)
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key       string
		sensitive bool
	}{
		{"password", true},
		{"Password", true},
		{"PASSWORD", true},
		{"userPassword", true},
		{"token", true},
		{"apiToken", true},
		{"api_key", true},
		{"apiKey", true},
		{"secret", true},
		{"secretKey", true},
		{"credential", true},
		{"auth", true},
		{"authToken", true},
		{"username", false},
		{"host", false},
		{"port", false},
		{"dataDir", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := isSensitiveKey(tt.key)
			if result != tt.sensitive {
				t.Errorf("expected isSensitiveKey(%q) = %v, got %v", tt.key, tt.sensitive, result)
			}
		})
	}
}

func TestMaskURLCredentials(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "no credentials",
			input: "http://example.com",
			want:  "http://example.com",
		},
		{
			name:  "http with credentials",
			input: "http://user:pass@example.com",
			want:  "http://***@example.com",
		},
		{
			name:  "https with credentials",
			input: "https://admin:secret@receiver.local:8080/path",
			want:  "https://***@receiver.local:8080/path",
		},
		{
			name:  "complex credentials",
			input: "http://user%40example.com:p%40ssw0rd@api.example.com/v1",
			want:  "http://***@api.example.com/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskURL(tt.input)
			if got != tt.want {
				t.Errorf("MaskURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
