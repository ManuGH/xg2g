// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

// probeVideoCodecs reports every distinct video codec ffprobe finds, sorted.
//
// Every one of them, not just the first: a body carrying both sides of a topology
// change would name two, and that is the failure worth catching. ffprobe lists a
// stream once per program and once at the top level, so the names are deduplicated
// rather than counted.
func probeVideoCodecs(t *testing.T, data []byte) []string {
	t.Helper()

	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v",
		"-show_entries", "stream=codec_name", "-of", "default=nokey=1:noprint_wrappers=1", "pipe:0")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("ffprobe codec detection failed: %v (stderr: %s)", err, stderr.String())
	}

	seen := map[string]bool{}
	for _, name := range strings.Fields(stdout.String()) {
		seen[name] = true
	}
	codecs := make([]string, 0, len(seen))
	for name := range seen {
		codecs = append(codecs, name)
	}
	sort.Strings(codecs)
	return codecs
}

// assertTSAligned proves the body is transport stream and nothing else. A handler
// that appended an error message, or that was cut off mid-packet, fails here.
func assertTSAligned(t *testing.T, body []byte) {
	t.Helper()

	if len(body) == 0 {
		t.Fatal("client received no bytes at all")
	}
	if rem := len(body) % ring.TSPacketSize; rem != 0 {
		t.Fatalf("body is %d bytes, %d past a packet boundary: the response did not end on a whole packet",
			len(body), rem)
	}
	for off := 0; off < len(body); off += ring.TSPacketSize {
		if body[off] != ring.SyncByte {
			t.Fatalf("no sync byte at offset %d of %d: the body carries something that is not TS", off, len(body))
		}
	}
}

// The upstream is fed at roughly 4 Mbps, below the bitrate of either capture, so
// the normalizer's pacer always has less than it could send. Feeding faster than
// the media rate is what fills its staging buffer, and a full staging buffer ends
// the ingest - which would end the client's stream for a reason that has nothing to
// do with the topology cut this test is about.
const (
	feedChunkPackets  = 64
	feedChunkInterval = 24 * time.Millisecond
)

// The HTTP boundary of the topology generation cut.
//
// Everything below the handler is already proven: a worker serves exactly one
// upstream generation, and its VariantRing closes when that generation ends. What
// this test pins down is the last hop - that the cut reaches the client as an
// ordinary end of stream and not as a failure.
//
// The contract being asserted is deliberately narrow, and it is a backend contract:
//
//	the response stays 200, ends by itself, carries no error body, and carries
//	the bytes of exactly one generation.
//
// Whether an iOS, Android or web player then reconnects is a client question and is
// not proven here. A clean end of stream is what the backend owes it.
func TestHandler_UpstreamGenerationCut_EndsClientStreamCleanly(t *testing.T) {
	skipIfNoFFmpeg(t)

	// Generation N is H.264 + AAC; generation N+1 is HEVC on the same PIDs, with its
	// PMT version bumped so the ring sees a topology change rather than a splice.
	first := tsfixture.Load(t, "verify_final_v3.ts")
	second := tsfixture.BumpPMTVersion(t, tsfixture.Load(t, "test_hevc_stream.ts"), 1)

	// Closed by the test once the client is definitely streaming, so the cut happens
	// at a known point rather than whenever a capture happens to run out.
	switchGeneration := make(chan struct{})

	cfg := DefaultConnectorConfig("", 8001)
	cfg.NormConfig.StartupReservoirMs = 50.0
	cfg.NormConfig.PacerIntervalMs = 5.0
	cfg.NormConfig.InitialBitrateKbps = 20000.0
	// The normalizer fails closed when its staging buffer fills, and the two captures
	// were recorded at very different bitrates (9.0 and 2.1 Mbps). The feed below
	// stays under both, so the buffer drains rather than grows; the headroom is what
	// keeps a scheduling hiccup on a loaded machine from ending the ingest instead of
	// the test.
	cfg.NormConfig.StagingBufferCapacity = 32 * 1024 * 1024
	cfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			defer func() { _ = pw.Close() }()
			// The upstream never stops, before or after the change. A broadcast does
			// not pause for a PMT bump, and a client that is somehow still being
			// served after the cut has to have something to be served, or this test
			// would pass on the upstream drying up.
			for {
				src := first
				select {
				case <-switchGeneration:
					src = second
				default:
				}
				for i := 0; i < len(src); i += feedChunkPackets * ring.TSPacketSize {
					end := i + feedChunkPackets*ring.TSPacketSize
					if end > len(src) {
						end = len(src)
					}
					if _, err := pw.Write(src[i:end]); err != nil {
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const serviceRef = "1:0:19:1:3FB:1:C00000:0:0:0:"
	key := session.NewSessionKey("127.0.0.1", 8001, serviceRef)
	key.TargetProgram = 1

	// The test holds its own lease on the same session the client will be served
	// from. It keeps the ingest alive independently of the client, which is what
	// makes the assertions after the client's stream ends mean anything.
	lease, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire ingest lease: %v", err)
	}
	defer lease.Release()

	pipe, ok := lease.Session().Payload().(*SessionPipeline)
	if !ok {
		t.Fatal("session payload is not a SessionPipeline")
	}

	// The transcode decision is made from the PMT, so the request must not be sent
	// before generation N is fully known.
	waitUntil(t, 30*time.Second, "upstream never became decodable on generation N", func() bool {
		facts := pipe.MasterRing().ReadinessFacts()
		_, hasKeyframe := pipe.MasterRing().LatestKeyframeOffset()
		return hasKeyframe && len(facts.AudioTracks) > 0
	})
	generationN := pipe.MasterRing().Generation()

	// audio=aac forces the client onto a variant worker. Without a capability gap
	// it would be served straight from the master ring, which is a different path
	// with a different lifetime and is not what this test is about.
	reqURL := fmt.Sprintf("%s/api/v3/stream/live/%s?audio=aac", server.URL, serviceRef)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("stream answered %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Xg2g-Audio-Mode"); got != "transcode" {
		t.Fatalf("audio mode is %q, so the client was never routed to a variant worker", got)
	}

	type streamResult struct {
		body []byte
		err  error
	}
	const streamingProof = 256 * 1024
	streaming := make(chan struct{})
	finished := make(chan streamResult, 1)

	go func() {
		var buf bytes.Buffer
		chunk := make([]byte, 32*1024)
		signalled := false
		for {
			n, rerr := resp.Body.Read(chunk)
			if n > 0 {
				buf.Write(chunk[:n])
				if !signalled && buf.Len() >= streamingProof {
					signalled = true
					close(streaming)
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					rerr = nil
				}
				finished <- streamResult{body: buf.Bytes(), err: rerr}
				return
			}
		}
	}()

	select {
	case <-streaming:
	case res := <-finished:
		t.Fatalf("the stream ended before the topology ever changed (%d bytes, err %v)", len(res.body), res.err)
	case <-time.After(60 * time.Second):
		t.Fatal("client never received a variant stream that could be cut")
	}

	close(switchGeneration)

	var result streamResult
	select {
	case result = <-finished:
	case <-time.After(60 * time.Second):
		t.Fatal("the response never ended after the upstream topology changed")
	}

	// A clean end of body. io.Copy semantics: EOF is not an error, anything else is
	// a truncated or reset response, which is exactly what a client would surface as
	// a playback failure rather than as an end of stream.
	if result.err != nil {
		t.Fatalf("client stream ended with %v, want a clean end of body", result.err)
	}

	assertTSAligned(t, result.body)

	if got := pipe.MasterRing().Generation(); got == generationN {
		t.Fatalf("upstream is still at generation %d; the response ended for some other reason", generationN)
	}

	// The ingest itself is untouched: it is still running and still accepting the
	// new generation. This is also what rules out the fallback path - a client that
	// had been served straight from the master ring would still be receiving bytes
	// here instead of having ended.
	headAfterCut := pipe.MasterRing().Head()
	waitUntil(t, 10*time.Second, "the whole ingest died with the variant worker", func() bool {
		return pipe.MasterRing().Head() > headAfterCut
	})

	// One generation only. The bytes the client received were produced by a single
	// FFmpeg process bound to generation N; the cut is what stopped generation N+1
	// from ever reaching it. A decode that still succeeds end to end is the evidence
	// that nothing from the other side of the cut was mixed in.
	codecs := probeVideoCodecs(t, result.body)
	if len(codecs) != 1 || codecs[0] != "h264" {
		t.Fatalf("client stream carries video codecs %v, want only the h264 of the generation it attached to", codecs)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(result.body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("strict decode of the delivered stream failed: %v (stderr: %s)", err, stderr.String())
	}
	if frames := countDecodedVideoFrames(t, result.body); frames <= 0 {
		t.Fatal("client stream decoded 0 video frames")
	}

	t.Logf("✅ generation cut delivered as a clean end of stream: %d bytes, %d frames, HTTP %d",
		len(result.body), countDecodedVideoFrames(t, result.body), resp.StatusCode)
}

// waitUntil polls a condition, failing with the caller's own wording rather than a
// generic timeout.
func waitUntil(t *testing.T, timeout time.Duration, failure string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(failure)
}
