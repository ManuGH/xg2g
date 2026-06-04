// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPickPreviewSegment(t *testing.T) {
	segs := []string{"seg_000000.ts", "seg_000001.ts", "seg_000002.ts", "seg_000003.ts"}
	cases := []struct {
		name   string
		offset float64
		seg    int
		want   string
	}{
		{"window start", 0, 6, "seg_000000.ts"},
		{"mid first segment", 3, 6, "seg_000000.ts"},
		{"second segment boundary", 6, 6, "seg_000001.ts"},
		{"third segment", 13, 6, "seg_000002.ts"},
		{"beyond end clamps to last", 9999, 6, "seg_000003.ts"},
		{"negative collapses to first", -50, 6, "seg_000000.ts"},
		{"zero seg duration falls back to 6", 6, 0, "seg_000001.ts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pickPreviewSegment(segs, tc.offset, tc.seg)
			if !ok {
				t.Fatalf("expected ok for offset %v", tc.offset)
			}
			if got != tc.want {
				t.Fatalf("offset %v seg %d: got %q want %q", tc.offset, tc.seg, got, tc.want)
			}
		})
	}

	// Negative control: an empty segment list never resolves a segment.
	if _, ok := pickPreviewSegment(nil, 0, 6); ok {
		t.Fatal("expected !ok for empty segment list")
	}
}

func TestParsePreviewOffset(t *testing.T) {
	cases := map[string]float64{
		"":       0,
		"0":      0,
		"42.5":   42.5,
		"-5":     0,   // negative -> 0
		"abc":    0,   // malformed -> 0
		"  90  ": 90,  // trimmed
		"NaN":    0,   // non-finite -> 0
		"Inf":    0,   // non-finite -> 0
		"1e9":    1e9, // large but finite is fine; clamping happens in pickPreviewSegment
	}
	for in, want := range cases {
		if got := parsePreviewOffset(in); got != want {
			t.Fatalf("parsePreviewOffset(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestListLiveSegments(t *testing.T) {
	dir := t.TempDir()
	// Files that should be ignored: playlist, init segment, partials, dirs.
	for _, name := range []string{
		"seg_000002.ts", "seg_000000.ts", "seg_000001.ts", // out of order on disk
		"index.m3u8", "init.mp4", "seg_000000.tmp", "notseg_000000.ts",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "seg_dir.ts"), 0o750); err != nil {
		t.Fatal(err)
	}

	segs, err := listLiveSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"seg_000000.ts", "seg_000001.ts", "seg_000002.ts"}
	if len(segs) != len(want) {
		t.Fatalf("got %v, want %v", segs, want)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Fatalf("sorted order wrong: got %v want %v", segs, want)
		}
	}

	// Negative control: a non-existent dir errors (caller treats as 404).
	if _, err := listLiveSegments(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestPreviewCacheGetOrBuildDedupsAndEvicts(t *testing.T) {
	c := newPreviewCache(2)

	var builds int32
	build := func(val string) func() ([]byte, error) {
		return func() ([]byte, error) {
			atomic.AddInt32(&builds, 1)
			return []byte(val), nil
		}
	}

	// First build populates; second hits cache (no extra build).
	if _, err := c.getOrBuild("a", build("A")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.getOrBuild("a", build("A")); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("expected 1 build for repeated key, got %d", got)
	}

	// Concurrent identical builds are deduped by singleflight.
	atomic.StoreInt32(&builds, 0)
	var wg sync.WaitGroup
	slow := func() ([]byte, error) {
		atomic.AddInt32(&builds, 1)
		return []byte("Z"), nil
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.getOrBuild("z", slow)
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&builds); got > 4 {
		t.Fatalf("singleflight should collapse concurrent builds, got %d", got)
	}

	// Eviction: cap is 2; "a" and "z" present, adding "b" then "c" evicts oldest.
	atomic.StoreInt32(&builds, 0)
	_, _ = c.getOrBuild("b", build("B"))
	_, _ = c.getOrBuild("c", build("C")) // now over cap -> oldest evicted
	// The oldest key ("a") should have been evicted: rebuilding it builds again.
	if _, err := c.getOrBuild("a", build("A2")); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&builds); got < 3 {
		t.Fatalf("expected eviction to force a rebuild of 'a' (>=3 builds), got %d", got)
	}

	// Negative control: a build error is propagated and not cached.
	wantErr := errors.New("boom")
	if _, err := c.getOrBuild("err", func() ([]byte, error) { return nil, wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("expected build error to propagate, got %v", err)
	}
	if _, ok := c.get("err"); ok {
		t.Fatal("failed build must not be cached")
	}
}
