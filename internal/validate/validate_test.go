// SPDX-License-Identifier: MIT
package validate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidator_URL(t *testing.T) {
	tests := []struct {
		name           string
		value          string
		allowedSchemes []string
		wantErr        bool
	}{
		{"valid http", "http://example.com", []string{"http", "https"}, false},
		{"valid https", "https://example.com", []string{"http", "https"}, false},
		{"empty url", "", []string{"http"}, true},
		{"no host", "http://", []string{"http"}, true},
		{"invalid scheme", "ftp://example.com", []string{"http", "https"}, true},
		{"no scheme", "example.com", []string{"http"}, true},
		{"with port", "http://example.com:8080", []string{"http"}, false},
		{"with path", "http://example.com/path", []string{"http"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.URL("testURL", tt.value, tt.allowedSchemes)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_Port(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"valid port 80", 80, false},
		{"valid port 8080", 8080, false},
		{"valid port 65535", 65535, false},
		{"valid port 1", 1, false},
		{"invalid port 0", 0, true},
		{"invalid port -1", -1, true},
		{"invalid port 65536", 65536, true},
		{"invalid port 100000", 100000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Port("testPort", tt.port)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_Range(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		min     int
		max     int
		wantErr bool
	}{
		{"in range", 5, 1, 10, false},
		{"at min", 1, 1, 10, false},
		{"at max", 10, 1, 10, false},
		{"below min", 0, 1, 10, true},
		{"above max", 11, 1, 10, true},
		{"negative range", -5, -10, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Range("testValue", tt.value, tt.min, tt.max)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")

	tests := []struct {
		name      string
		path      string
		mustExist bool
		wantErr   bool
	}{
		{"existing dir", tmpDir, true, false},
		{"existing dir no mustExist", tmpDir, false, false},
		{"nonexistent mustExist", nonExistentDir, true, true},
		{"nonexistent create", filepath.Join(tmpDir, "autocreate"), false, false},
		{"empty path", "", false, true},
		{"path traversal", "../etc", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Directory("testDir", tt.path, tt.mustExist)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_NotEmpty(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"non-empty", "hello", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"tab only", "\t", true},
		{"newline only", "\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.NotEmpty("testField", tt.value)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_OneOf(t *testing.T) {
	allowed := []string{"red", "green", "blue"}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid red", "red", false},
		{"valid green", "green", false},
		{"valid blue", "blue", false},
		{"invalid yellow", "yellow", true},
		{"invalid empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.OneOf("testField", tt.value, allowed)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_Positive(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"positive 1", 1, false},
		{"positive 100", 100, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Positive("testField", tt.value)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_NonNegative(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"positive 1", 1, false},
		{"zero", 0, false},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.NonNegative("testField", tt.value)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_Custom(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		validator func(interface{}) error
		wantErr   bool
	}{
		{
			name:  "passes custom validation",
			value: "hello",
			validator: func(v interface{}) error {
				s, ok := v.(string)
				if !ok {
					return errors.New("expected string value")
				}
				if len(s) >= 3 {
					return nil
				}
				return errors.New("too short")
			},
			wantErr: false,
		},
		{
			name:  "fails custom validation",
			value: "hi",
			validator: func(v interface{}) error {
				s, ok := v.(string)
				if !ok {
					return errors.New("expected string value")
				}
				if len(s) >= 3 {
					return nil
				}
				return errors.New("too short")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Custom("testField", tt.value, tt.validator)

			if tt.wantErr && v.IsValid() {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && !v.IsValid() {
				t.Errorf("unexpected error: %v", v.Err())
			}
		})
	}
}

func TestValidator_MultipleErrors(t *testing.T) {
	v := New()

	v.Port("port", 0)                  // Invalid
	v.URL("url", "", []string{"http"}) // Invalid
	v.NotEmpty("name", "")             // Invalid

	if v.IsValid() {
		t.Fatal("expected errors, got none")
	}

	errors := v.Errors()
	if len(errors) != 3 {
		t.Errorf("expected 3 errors, got %d", len(errors))
	}

	err := v.Err()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	errorMsg := err.Error()
	if !strings.Contains(errorMsg, "port") {
		t.Error("error message should mention 'port'")
	}
	if !strings.Contains(errorMsg, "url") {
		t.Error("error message should mention 'url'")
	}
	if !strings.Contains(errorMsg, "name") {
		t.Error("error message should mention 'name'")
	}
}

func TestValidator_Chaining(t *testing.T) {
	v := New()

	// Chain multiple validations
	v.URL("baseURL", "http://example.com", []string{"http", "https"})
	v.Port("port", 8080)
	v.Range("days", 7, 1, 14)
	v.NotEmpty("bouquet", "Favourites")

	if !v.IsValid() {
		t.Errorf("unexpected errors: %v", v.Err())
	}
}

func TestValidator_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "auto", "create", "nested")

	v := New()
	v.Directory("testDir", newDir, false)

	if !v.IsValid() {
		t.Errorf("unexpected error: %v", v.Err())
	}

	// Check that directory was actually created
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestLogLevel_IsValid(t *testing.T) {
	tests := []struct {
		level LogLevel
		want  bool
	}{
		{LogLevelDebug, true},
		{LogLevelInfo, true},
		{LogLevelWarn, true},
		{LogLevelError, true},
		{LogLevel("invalid"), false},
		{LogLevel(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			if got := tt.level.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    LogLevel
		wantErr bool
	}{
		{"debug", LogLevelDebug, false},
		{"info", LogLevelInfo, false},
		{"warn", LogLevelWarn, false},
		{"error", LogLevelError, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLogLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLogLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseLogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Security regression tests for path traversal protection
func TestValidatePath_Security(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "valid relative path",
			path:      "xmltv.xml",
			shouldErr: false,
		},
		{
			name:      "valid subdirectory",
			path:      "output/xmltv.xml",
			shouldErr: false,
		},
		{
			name:      "empty path allowed",
			path:      "",
			shouldErr: false,
		},
		{
			name:      "absolute path",
			path:      "/etc/passwd",
			shouldErr: true,
			errMsg:    "must be relative",
		},
		{
			name:      "traversal with dotdot",
			path:      "../../../etc/passwd",
			shouldErr: true,
			errMsg:    "traversal",
		},
		{
			name:      "traversal encoded",
			path:      "..%2F..%2Fetc%2Fpasswd",
			shouldErr: true,
			errMsg:    "traversal",
		},
		{
			name:      "windows-style traversal",
			path:      "..\\..\\windows\\system32",
			shouldErr: true,
			errMsg:    "traversal",
		},
		{
			name:      "hidden traversal in subdirectory",
			path:      "subdir/../../../etc/passwd",
			shouldErr: true,
			errMsg:    "traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.Path("test_field", tt.path)

			if tt.shouldErr {
				if v.IsValid() {
					t.Errorf("expected validation to fail, but it passed")
				} else {
					err := v.Err()
					if err == nil {
						t.Fatal("expected validation error, got nil")
					}
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error to contain %q, got %q", tt.errMsg, err)
					}
				}
			} else {
				if !v.IsValid() {
					t.Errorf("expected validation to pass, got error: %v", v.Err())
				}
			}
		})
	}
}

func TestValidatePath_Integration(t *testing.T) {
	// Test the validator with multiple fields
	v := New()

	v.Path("xmltvPath", "data/xmltv.xml")
	v.Path("playlistPath", "playlists/my.m3u")
	v.Path("maliciousPath", "../../../etc/passwd")

	if v.IsValid() {
		t.Fatal("expected validation to fail")
	}

	errors := v.Errors()
	if len(errors) != 1 {
		t.Errorf("expected exactly 1 error, got %d", len(errors))
	}
	if len(errors) > 0 {
		if errors[0].Field != "maliciousPath" {
			t.Errorf("expected error for 'maliciousPath', got %q", errors[0].Field)
		}
		if !strings.Contains(errors[0].Message, "traversal") {
			t.Errorf("expected error message to contain 'traversal', got %q", errors[0].Message)
		}
	}
}

func TestPathWithinRoot_Security(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file inside tmpDir
	validFile := filepath.Join(tmpDir, "valid.xml")
	if err := os.WriteFile(validFile, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a file outside tmpDir
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside tmpDir pointing outside
	symlinkPath := filepath.Join(tmpDir, "escape")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		path      string
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "valid file within root",
			path:      "valid.xml",
			shouldErr: false,
		},
		{
			name:      "absolute path rejected",
			path:      "/etc/passwd",
			shouldErr: true,
			errMsg:    "must be relative",
		},
		{
			name:      "traversal attack",
			path:      "../../../etc/passwd",
			shouldErr: true,
			errMsg:    "traversal",
		},
		{
			name:      "symlink escape attempt",
			path:      "escape",
			shouldErr: true,
			errMsg:    "escapes root",
		},
		{
			name:      "nonexistent file is okay",
			path:      "future.xml",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.PathWithinRoot("testPath", tt.path, tmpDir)

			if tt.shouldErr {
				if v.IsValid() {
					t.Errorf("expected validation to fail, but it passed")
				} else {
					err := v.Err()
					if err == nil {
						t.Fatal("expected validation error, got nil")
					}
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error to contain %q, got %q", tt.errMsg, err)
					}
				}
			} else {
				if !v.IsValid() {
					t.Errorf("expected validation to pass, got error: %v", v.Err())
				}
			}
		})
	}
}

func TestValidator_StreamURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid http stream with path",
			url:     "http://example.com:8001/stream/123.ts",
			wantErr: false,
		},
		{
			name:    "valid https stream with query params",
			url:     "https://stream.example.com/live?channel=123&auth=token",
			wantErr: false,
		},
		{
			name:    "valid stream with port and path",
			url:     "http://192.168.1.100:8080/channels/HD/1",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "invalid URL syntax",
			url:     "ht!tp://invalid",
			wantErr: true,
			errMsg:  "invalid URL syntax",
		},
		{
			name:    "unsupported scheme ftp",
			url:     "ftp://example.com/stream.ts",
			wantErr: true,
			errMsg:  "unsupported scheme",
		},
		{
			name:    "unsupported scheme rtsp",
			url:     "rtsp://example.com/stream",
			wantErr: true,
			errMsg:  "unsupported scheme",
		},
		{
			name:    "missing host",
			url:     "http:///stream/123",
			wantErr: true,
			errMsg:  "must have a host",
		},
		{
			name:    "missing path",
			url:     "http://example.com",
			wantErr: true,
			errMsg:  "must have a path",
		},
		{
			name:    "root path only",
			url:     "http://example.com/",
			wantErr: true,
			errMsg:  "must have a path",
		},
		{
			name:    "no scheme",
			url:     "example.com/stream",
			wantErr: true,
			errMsg:  "unsupported scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New()
			v.StreamURL("streamURL", tt.url)

			if tt.wantErr {
				if v.IsValid() {
					t.Errorf("expected validation to fail for %q, but it passed", tt.url)
				} else {
					err := v.Err()
					if err == nil {
						t.Fatal("expected validation error, got nil")
					}
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error to contain %q, got %q", tt.errMsg, err.Error())
					}
				}
			} else {
				if !v.IsValid() {
					t.Errorf("expected validation to pass for %q, got error: %v", tt.url, v.Err())
				}
			}
		})
	}
}

func TestValidator_StreamURL_Integration(t *testing.T) {
	// Test StreamURL validation with multiple URLs in a single validator
	v := New()

	v.StreamURL("stream1", "http://example.com:8001/live/channel1.ts")
	v.StreamURL("stream2", "https://secure.stream.tv/hls/playlist.m3u8")
	v.StreamURL("stream3", "ftp://invalid.com/stream") // Invalid scheme
	v.StreamURL("stream4", "http://nopath.com")        // Missing path

	if v.IsValid() {
		t.Fatal("expected validation to fail")
	}

	errors := v.Errors()
	if len(errors) != 2 {
		t.Errorf("expected exactly 2 errors, got %d: %v", len(errors), errors)
	}

	// Verify error fields
	errorFields := make(map[string]bool)
	for _, e := range errors {
		errorFields[e.Field] = true
	}
	if !errorFields["stream3"] {
		t.Error("expected error for 'stream3' (ftp scheme)")
	}
	if !errorFields["stream4"] {
		t.Error("expected error for 'stream4' (missing path)")
	}
}
