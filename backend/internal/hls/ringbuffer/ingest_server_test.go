// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"bytes"
	"context"
	"fmt"
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

	// Wait briefly for async worker to write
	time.Sleep(100 * time.Millisecond)

	// Check live session on disk (should not exist)
	liveFile := filepath.Join(tmpDir, "sessions", "live_sess", "seg_000001.ts")
	if _, err := os.Stat(liveFile); !os.IsNotExist(err) {
		t.Fatalf("live session file should NOT exist on disk, but found: %v", err)
	}

	// Check rec session on disk (should exist with content "rec_data")
	recFile := filepath.Join(tmpDir, "sessions", "rec_sess", "seg_000001.ts")
	data, err := os.ReadFile(recFile)
	if err != nil {
		t.Fatalf("rec session file should exist on disk, got err: %v", err)
	}
	if string(data) != "rec_data" {
		t.Fatalf("expected rec_data on disk, got %s", string(data))
	}
}
