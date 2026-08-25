// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

// The plan headers must describe the stream the client is actually getting.
//
// A capability gap routes a request to a variant worker, but that attach can fail -
// no transcoder on the host, a worker that cannot start - and the request then falls
// back to the master stream, unchanged and untranscoded. The headers used to keep
// announcing the plan that was attempted rather than the one that happened, so a
// client was told "transcode" and "deinterlace_50p" while being handed the original
// interlaced stream in its original codec.
//
// The transcoder is made unavailable here rather than mocked: an empty PATH is
// exactly the production failure this guards against, and it needs no ffmpeg to run.
func TestHandler_FallbackToMaster_DoesNotAnnounceATranscode(t *testing.T) {
	capture := tsfixture.Load(t, "verify_final_v3.ts")

	cfg := DefaultConnectorConfig("", 8001)
	cfg.NormConfig.StartupReservoirMs = 50.0
	cfg.NormConfig.PacerIntervalMs = 5.0
	cfg.NormConfig.InitialBitrateKbps = 20000.0
	cfg.NormConfig.StagingBufferCapacity = 32 * 1024 * 1024
	cfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			defer func() { _ = pw.Close() }()
			for {
				for i := 0; i < len(capture); i += feedChunkPackets * ring.TSPacketSize {
					end := i + feedChunkPackets*ring.TSPacketSize
					if end > len(capture) {
						end = len(capture)
					}
					if _, err := pw.Write(capture[i:end]); err != nil {
						return
					}
					time.Sleep(feedChunkInterval)
				}
			}
		}()
		return pr, nil
	}

	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(cfg))
	defer func() { _ = mgr.Close() }()

	server := httptest.NewServer(NewHandlerWithReceiver(mgr, "127.0.0.1", 8001))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const serviceRef = "1:0:19:1:3FB:1:C00000:0:0:0:"
	key := session.NewSessionKey("127.0.0.1", 8001, serviceRef)
	key.TargetProgram = 1

	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire ingest lease: %v", err)
	}
	defer lease.Release()

	pipe, ok := lease.Session().Payload().(*SessionPipeline)
	if !ok {
		t.Fatal("session payload is not a SessionPipeline")
	}

	waitUntil(t, 30*time.Second, "upstream never became decodable", func() bool {
		facts := pipe.MasterRing().ReadinessFacts()
		_, hasKeyframe := pipe.MasterRing().LatestKeyframeOffset()
		return hasKeyframe && len(facts.AudioTracks) > 0
	})

	// No transcoder anywhere on PATH, so every variant attach fails and every
	// capability gap resolves to the master stream.
	t.Setenv("PATH", "")

	// Both gaps at once: a client that claims to need HEVC video and AAC audio.
	reqURL := fmt.Sprintf("%s/api/v3/stream/live/%s?audio=aac", server.URL, serviceRef)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Client-Video-Codecs", "hevc")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("fallback answered %d: %s", resp.StatusCode, body)
	}

	// The request is genuinely being served, from the master ring.
	chunk := make([]byte, 32*1024)
	if n, rerr := io.ReadFull(resp.Body, chunk); rerr != nil || n == 0 {
		t.Fatalf("fallback delivered no stream: n=%d err=%v", n, rerr)
	}

	for _, tc := range []struct{ header, want, why string }{
		{"X-Xg2g-Video-Mode", "direct", "video is being passed through, not transcoded"},
		{"X-Xg2g-Audio-Mode", "direct", "audio is being passed through, not transcoded"},
		{"X-Xg2g-Scan-Policy", "passthrough", "no transcode is running, so no scan policy is being applied"},
	} {
		if got := resp.Header.Get(tc.header); got != tc.want {
			t.Errorf("%s = %q, want %q: %s", tc.header, got, tc.want, tc.why)
		}
	}

	// And what it does announce is the stream it is actually sending.
	if src, eff := resp.Header.Get("X-Xg2g-Audio-Source"), resp.Header.Get("X-Xg2g-Audio-Effective"); src != eff {
		t.Errorf("audio source %q and effective %q disagree while passing through untouched", src, eff)
	}
	if src, eff := resp.Header.Get("X-Xg2g-Video-Source"), resp.Header.Get("X-Xg2g-Video-Effective"); src != eff {
		t.Errorf("video source %q and effective %q disagree while passing through untouched", src, eff)
	}
}
