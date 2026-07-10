// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func TestInMemoryIngest_LiveHLSArgs_MPEGTS(t *testing.T) {
	adapter := &LocalAdapter{
		HLSRoot:        "/tmp/hls",
		inMemoryIngest: true,
		ingestPort:     8090,
		Logger:         zerolog.Nop(),
	}

	spec := ports.StreamSpec{
		SessionID: "test-sess-ts",
		Profile: ports.ProfileSpec{
			Container: "ts",
		},
	}
	layout := liveSegmentLayout{
		segmentDurationSec: 2,
		listSize:           6,
	}

	args := adapter.appendLiveHLSArgs(nil, spec, layout)
	argStr := strings.Join(args, " ")

	expectedMethod := "-method PUT"
	if !strings.Contains(argStr, expectedMethod) {
		t.Errorf("expected args to contain %q, got: %s", expectedMethod, argStr)
	}

	expectedSegmentFilename := fmt.Sprintf("-hls_segment_filename http://127.0.0.1:%d/ingest/%s/seg_%%06d.ts", adapter.ingestPort, spec.SessionID)
	if !strings.Contains(argStr, expectedSegmentFilename) {
		t.Errorf("expected args to contain %q, got: %s", expectedSegmentFilename, argStr)
	}

	outPath := adapter.prepareLiveOutputPath(spec.SessionID, 0)
	expectedOutPath := fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/index.m3u8", adapter.ingestPort, spec.SessionID)
	if outPath != expectedOutPath {
		t.Errorf("expected outPath %q, got: %q", expectedOutPath, outPath)
	}
}

func TestInMemoryIngest_LiveHLSArgs_FMP4(t *testing.T) {
	adapter := &LocalAdapter{
		HLSRoot:        "/tmp/hls",
		inMemoryIngest: true,
		ingestPort:     8091,
		Logger:         zerolog.Nop(),
	}

	spec := ports.StreamSpec{
		SessionID: "test-sess-fmp4",
		Profile: ports.ProfileSpec{
			Container: "fmp4",
		},
	}
	layout := liveSegmentLayout{
		segmentDurationSec: 2,
		listSize:           6,
	}

	args := adapter.appendLiveHLSArgs(nil, spec, layout)
	argStr := strings.Join(args, " ")

	expectedMethod := "-method PUT"
	if !strings.Contains(argStr, expectedMethod) {
		t.Errorf("expected args to contain %q, got: %s", expectedMethod, argStr)
	}

	expectedSegmentFilename := fmt.Sprintf("-hls_segment_filename http://127.0.0.1:%d/ingest/%s/seg_%%06d.m4s", adapter.ingestPort, spec.SessionID)
	if !strings.Contains(argStr, expectedSegmentFilename) {
		t.Errorf("expected args to contain %q, got: %s", expectedSegmentFilename, argStr)
	}

	expectedInitFilename := fmt.Sprintf("-hls_fmp4_init_filename http://127.0.0.1:%d/ingest/%s/init.mp4", adapter.ingestPort, spec.SessionID)
	if !strings.Contains(argStr, expectedInitFilename) {
		t.Errorf("expected args to contain %q, got: %s", expectedInitFilename, argStr)
	}
}
