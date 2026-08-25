// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestIngestServer_PutAndRetrieve(t *testing.T) {
	reg := NewRegistry(10)
	srv, err := NewIngestServer(0, "", reg, zerolog.Nop(), nil)
	if err != nil {
		t.Fatalf("failed to create ingest server: %v", err)
	}
	srv.Start()
	defer func() { _ = srv.Stop(context.Background()) }()

	url := fmt.Sprintf("%s/index.m3u8", srv.URL("sess1"))
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString("#EXTM3U\n#EXTINF:2.000,\nseg_0.ts\n"))
	if err != nil {
		t.Fatalf("failed to create put request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	buf, ok := reg.Get("sess1")
	if !ok {
		t.Fatalf("expected buffer for sess1 to be created")
	}

	art, ok := buf.Get("index.m3u8")
	if !ok {
		t.Fatalf("expected index.m3u8 in buffer")
	}

	if string(art.Data) != "#EXTM3U\n#EXTINF:2.000,\nseg_0.ts\n" {
		t.Fatalf("unexpected artifact content: %s", string(art.Data))
	}
}

func TestIngestServer_DVRIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hls_dvr_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	reg := NewRegistry(10)
	shouldRecord := func(sid string) bool {
		return sid == "rec_sess"
	}

	srv, err := NewIngestServer(0, tmpDir, reg, zerolog.Nop(), shouldRecord)
	if err != nil {
		t.Fatalf("failed to create ingest server: %v", err)
	}
	srv.Start()
	defer func() { _ = srv.Stop(context.Background()) }()

	// 1. Ingest for live session (shouldRecord = false) -> MUST NOT write to disk
	urlLive := fmt.Sprintf("%s/seg_000001.ts", srv.URL("live_sess"))
	reqLive, _ := http.NewRequest(http.MethodPut, urlLive, bytes.NewBufferString("live_data"))
	resp, err := http.DefaultClient.Do(reqLive)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("live put failed: %v, status: %v", err, resp)
	}

	// 2. Ingest for recording session (shouldRecord = true) -> MUST write to disk
	urlRec := fmt.Sprintf("%s/seg_000001.ts", srv.URL("rec_sess"))
	reqRec, _ := http.NewRequest(http.MethodPut, urlRec, bytes.NewBufferString("rec_data"))
	resp, err = http.DefaultClient.Do(reqRec)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("rec put failed: %v, status: %v", err, resp)
	}

	// Wait for the write the worker is supposed to make, rather than sleeping a
	// guessed interval. 100ms was enough on a developer machine and not on a
	// loaded CI runner, which made this the flakiest test in the package.
	//
	// The order matters: waiting for the recording file first also gives the
	// worker every chance to write the live file it must not write, so the
	// negative assertion below is stronger than it was under a fixed sleep.
	recFile := filepath.Join(ports.SessionHLSDir(tmpDir, "rec_sess"), "seg_000001.ts")
	data := waitForFile(t, recFile)
	if string(data) != "rec_data" {
		t.Fatalf("expected rec_data on disk, got %s", string(data))
	}

	// Check live session on disk (should not exist)
	liveFile := filepath.Join(ports.SessionHLSDir(tmpDir, "live_sess"), "seg_000001.ts")
	if _, err := os.Stat(liveFile); !os.IsNotExist(err) {
		t.Fatalf("live session file should NOT exist on disk, but found: %v", err)
	}
}

// waitForFile returns a file's contents once the writer has produced it, or
// fails the test at the deadline.
//
// The deadline is an upper bound and not a delay: the loop returns as soon as
// the file is readable, so a healthy run costs one iteration.
func waitForFile(t *testing.T, path string) []byte {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
		if err == nil {
			return data
		}
		if !os.IsNotExist(err) {
			t.Fatalf("reading %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("file never appeared within 5s: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
