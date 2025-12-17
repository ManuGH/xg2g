package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConfinePath(t *testing.T) {
	// Setup Temp Dir
	rootDir := t.TempDir()

	// Create legitimate file
	goodFile := filepath.Join(rootDir, "good.ts")
	if err := os.WriteFile(goodFile, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create secret file OUTSIDE root
	secretDir := t.TempDir()
	secretFile := filepath.Join(secretDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink INSIDE root pointing OUTSIDE
	symlink := filepath.Join(rootDir, "bad_link")
	if err := os.Symlink(secretFile, symlink); err != nil {
		t.Fatal(err)
	}

	// Create symlink INSIDE root pointing INSIDE
	goodSymlink := filepath.Join(rootDir, "good_link.ts")
	if err := os.Symlink(goodFile, goodSymlink); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		root      string
		target    string
		wantError bool
	}{
		{"Valid file", rootDir, "good.ts", false},
		{"Valid symlink (safe)", rootDir, "good_link.ts", false},
		{"Traversal DotDot", rootDir, "../etc/passwd", true},
		{"Traversal Backslash", rootDir, "..\\windows\\system32", true},
		{"Symlink Escape (Direct)", rootDir, "bad_link", true},
		{"Cleaned DotDot", rootDir, "foo/../../etc/passwd", true},
		// ".." as filename inside path:
		// "good..name" -> should pass now with new logic?
		// "good..name" Clean -> "good..name". Prefix check fails. == ".." fails.
		// So it should pass if we fixed the logic to be segment based.
		// We'll trust the logic update allowed it.
		// Let's create a file with .. in name to verify.
	}

	// Create file with dotdot in name
	dotFile := filepath.Join(rootDir, "foo..bar")
	os.WriteFile(dotFile, []byte("ok"), 0644)
	tests = append(tests, struct {
		name, root, target string
		wantError          bool
	}{"Filename with dots", rootDir, "foo..bar", false})

	for _, tt := range tests {
		t.Run("Rel_"+tt.name, func(t *testing.T) {
			got, err := confineRelPath(tt.root, tt.target)
			if tt.wantError {
				if err == nil {
					t.Errorf("confineRelPath(%q, %q) expected error, got %q", tt.root, tt.target, got)
				}
			} else {
				if err != nil {
					t.Errorf("confineRelPath(%q, %q) unexpected error: %v", tt.root, tt.target, err)
				}
				if _, err := os.Stat(got); err != nil {
					t.Errorf("result path %q does not exist", got)
				}
			}
		})
	}

	// Validate confineAbsPath
	// We construct absolute target paths
	absGood := filepath.Join(rootDir, "good.ts")
	absBad := filepath.Join(secretDir, "secret.txt") // Outside
	absSymlink := filepath.Join(rootDir, "bad_link") // Inside but points out

	// Create a symlink to the root itself
	// rootLink -> rootDir
	// Accessing rootLink/good.ts should resolve to rootDir/good.ts and be allowed.
	rootLink := filepath.Join(t.TempDir(), "root_link")
	if err := os.Symlink(rootDir, rootLink); err != nil {
		t.Fatal(err)
	}
	absRootLinkValid := filepath.Join(rootLink, "good.ts")

	absTests := []struct {
		name      string
		root      string
		target    string
		wantError bool
	}{
		{"Abs Valid", rootDir, absGood, false},
		{"Abs Outside", rootDir, absBad, true},
		{"Abs Symlink Escape", rootDir, absSymlink, true},
		{"Abs Symlinked Root Valid", rootLink, absRootLinkValid, false},
	}

	for _, tt := range absTests {
		t.Run("Abs_"+tt.name, func(t *testing.T) {
			got, err := confineAbsPath(tt.root, tt.target)
			if tt.wantError {
				if err == nil {
					t.Errorf("confineAbsPath(%q, %q) expected error, got %q", tt.root, tt.target, got)
				}
			} else {
				if err != nil {
					t.Errorf("confineAbsPath(%q, %q) unexpected error: %v", tt.root, tt.target, err)
				}
				if _, err := os.Stat(got); err != nil {
					t.Errorf("result path %q does not exist", got)
				}
			}
		})
	}
}

func TestSegmentAllowList(t *testing.T) {
	tests := []struct {
		segment string
		valid   bool
	}{
		{"index.m3u8", true},
		{"segment-0001.ts", true},
		{"init.mp4", true},
		{"chunk_1.m4s", true},
		{"audio.aac", true},
		{"subs.vtt", true},
		{"master.m3u8", true},
		{"../bad.ts", false},
		{"/root.ts", false},
		{"segment.ts?foo=bar", false}, // query not part of path param usually, but good to check regex fails
		{"funny-name_1.2.ts", true},
		{"evil.exe", false},
		{"hack.sh", false},
	}

	for _, tt := range tests {
		if got := segmentAllowList.MatchString(tt.segment); got != tt.valid {
			t.Errorf("MatchString(%q) = %v; want %v", tt.segment, got, tt.valid)
		}
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		cookies    map[string]string
		query      string
		allowQuery bool
		want       string
	}{
		{
			name:    "Bearer Header",
			headers: map[string]string{"Authorization": "Bearer abc"},
			want:    "abc",
		},
		{
			name:    "X-API-Token Header",
			headers: map[string]string{"X-API-Token": "xyz"},
			want:    "xyz",
		},
		{
			name:    "xg2g_session Cookie",
			cookies: map[string]string{"xg2g_session": "cookie1"},
			want:    "cookie1",
		},
		{
			name:    "Legacy Cookie",
			cookies: map[string]string{"X-API-Token": "legacy"},
			want:    "legacy",
		},
		{
			name:       "Query Param Allowed",
			query:      "token=query1",
			allowQuery: true,
			want:       "query1",
		},
		{
			name:       "Query Param Denied",
			query:      "token=query1",
			allowQuery: false,
			want:       "",
		},
		{
			name:    "Priority Header over Cookie",
			headers: map[string]string{"Authorization": "Bearer header"},
			cookies: map[string]string{"xg2g_session": "cookie"},
			want:    "header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			for k, v := range tt.cookies {
				req.AddCookie(&http.Cookie{Name: k, Value: v})
			}

			got := extractToken(req, tt.allowQuery)
			if got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
