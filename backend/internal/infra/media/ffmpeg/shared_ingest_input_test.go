// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

// failingLiveSources stands in for a shared ingest that cannot be reached, and
// records that it was asked.
type failingLiveSources struct {
	err     error
	asked   int
	lastRef string
}

func (f *failingLiveSources) AcquireLiveSource(_ context.Context, serviceRef string) (ports.LiveSource, error) {
	f.asked++
	f.lastRef = serviceRef
	return nil, f.err
}

func tunerSpec() ports.StreamSpec {
	return ports.StreamSpec{
		SessionID: "sess-cutover",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source: ports.StreamSource{
			Type:      ports.SourceTuner,
			ID:        "1:0:19:83:6:85:C00000:0:0:0:",
			TunerSlot: 0,
		},
	}
}

// The acceptance test for the input cutover. A tuner transcode that cannot get
// bytes from shared ingest has to fail. If it fell back to resolving a receiver
// URL, the parallel path this change removes would survive as an error handler
// and the architecture rule would be untrue in exactly the case that matters.
func TestStart_TunerSourceFailsWithoutSharedIngest(t *testing.T) {
	adapter := &LocalAdapter{
		Logger:  zerolog.Nop(),
		HLSRoot: t.TempDir(),
		BinPath: "/nonexistent/ffmpeg",
		// E2 is nil on purpose: reaching ResolveStreamURL would panic rather than
		// quietly resolve, so this also proves the resolver is not consulted.
		E2:          nil,
		LiveSources: nil,
	}

	handle, err := adapter.Start(context.Background(), tunerSpec())

	if err == nil {
		t.Fatalf("Start returned handle %q and no error; a tuner source without shared ingest must fail", handle)
	}
	if handle != "" {
		t.Errorf("Start returned handle %q on failure, want empty", handle)
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "shared ingest") {
		t.Errorf("error %q does not name shared ingest as the missing input", err)
	}
	if strings.Contains(msg, "http://") || strings.Contains(msg, "resolve stream url") {
		t.Errorf("error %q suggests a receiver URL was attempted", err)
	}
}

// The same rule when shared ingest exists but refuses: still an error, still no
// second way to get the bytes.
func TestStart_TunerSourceFailsWhenAcquireFails(t *testing.T) {
	sources := &failingLiveSources{err: errors.New("no tuner available upstream")}
	adapter := &LocalAdapter{
		Logger:      zerolog.Nop(),
		HLSRoot:     t.TempDir(),
		BinPath:     "/nonexistent/ffmpeg",
		LiveSources: sources,
	}

	spec := tunerSpec()
	handle, err := adapter.Start(context.Background(), spec)

	if err == nil {
		t.Fatalf("Start returned handle %q and no error; a refused acquire must fail the start", handle)
	}
	if sources.asked != 1 {
		t.Errorf("shared ingest was asked %d times, want exactly 1", sources.asked)
	}
	if sources.lastRef != spec.Source.ID {
		t.Errorf("shared ingest was asked for %q, want the session's service ref %q", sources.lastRef, spec.Source.ID)
	}
	if !strings.Contains(err.Error(), "no tuner available upstream") {
		t.Errorf("error %q does not carry the upstream reason", err)
	}
}

// stubLiveSource serves canned bytes and records its release.
type stubLiveSource struct {
	preamble []byte
	body     io.ReadCloser
	facts    ports.LiveSourceFacts
	released int
	attached int
}

func (s *stubLiveSource) Attach(context.Context, time.Duration) ([]byte, io.ReadCloser, error) {
	s.attached++
	return s.preamble, s.body, nil
}
func (s *stubLiveSource) Facts() ports.LiveSourceFacts { return s.facts }
func (s *stubLiveSource) Release()                     { s.released++ }

type stubLiveSources struct{ src *stubLiveSource }

func (p *stubLiveSources) AcquireLiveSource(context.Context, string) (ports.LiveSource, error) {
	return p.src, nil
}

// The preamble has to be the head of the byte stream FFmpeg reads. Handing over
// the topology after the payload it describes would leave a decoder configuring
// itself from packets it has already passed.
func TestSharedIngestInput_PreambleLeadsTheStream(t *testing.T) {
	preamble := []byte("PAT-PMT-PREAMBLE")
	payload := strings.Repeat("x", 4096)

	src := &stubLiveSource{
		preamble: preamble,
		body:     io.NopCloser(strings.NewReader(payload)),
	}
	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	adapter.LiveSources = &stubLiveSources{src: src}

	in, err := adapter.acquireSharedIngestInput(context.Background(), tunerSpec())
	if err != nil {
		t.Fatalf("acquireSharedIngestInput: %v", err)
	}
	defer in.Release()

	if src.attached != 1 {
		t.Errorf("attached %d times, want 1", src.attached)
	}

	got := make([]byte, len(preamble))
	if _, err := io.ReadFull(in.Stdin(), got); err != nil {
		t.Fatalf("reading the head of the spool: %v", err)
	}
	if string(got) != string(preamble) {
		t.Errorf("stream starts with %q, want the preamble %q", got, preamble)
	}
}

// Releasing must give the shared session back, or the upstream would be held open
// by a transcode that has already ended.
func TestSharedIngestInput_ReleaseGivesBackTheSession(t *testing.T) {
	src := &stubLiveSource{
		preamble: []byte("PRE"),
		body:     io.NopCloser(strings.NewReader(strings.Repeat("y", 1024))),
	}
	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	adapter.LiveSources = &stubLiveSources{src: src}

	in, err := adapter.acquireSharedIngestInput(context.Background(), tunerSpec())
	if err != nil {
		t.Fatalf("acquireSharedIngestInput: %v", err)
	}

	in.Release()
	if src.released != 1 {
		t.Fatalf("released %d times, want 1", src.released)
	}

	// The monitor releases on its own path and Start releases on the error path;
	// both can run for the same input, so a second release must be harmless.
	in.Release()
	if src.released != 1 {
		t.Errorf("released %d times after a repeated Release, want it to stay 1", src.released)
	}
}

// The other half of the cutover rule: external IPTV is a different source class
// and must be untouched by it. A SourceURL session still feeds FFmpeg the URL it
// was given, and never consults shared ingest - so deleting the receiver URL path
// did not take the IPTV path with it.
func TestPlanInput_ExternalURLSourceStillFeedsItsURL(t *testing.T) {
	const iptv = "http://provider.example.net/live/channel.m3u8"

	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	spec := ports.StreamSpec{
		SessionID: "sess-iptv",
		Mode:      ports.ModeLive,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: iptv},
	}

	plan, err := adapter.planInput(spec, iptv)
	if err != nil {
		t.Fatalf("planInput for an external URL: %v", err)
	}

	joined := strings.Join(plan.args, " ")
	if !strings.Contains(joined, "-i "+iptv) {
		t.Errorf("argv %q does not feed the external URL", joined)
	}
	if strings.Contains(joined, "pipe:0") {
		t.Errorf("argv %q routes external IPTV through the tuner pipe", joined)
	}
}

// The tuner rule stated from the other side: only SourceTuner routes through the
// spool. planInput is where the two classes part, and an external URL keeps its
// own transport.
func TestPlanInput_TunerSourceRefusesAnEmptyInput(t *testing.T) {
	adapter := &LocalAdapter{Logger: zerolog.Nop()}

	// With shared ingest attached this carries the snapshot path; without one there
	// is nothing to plan, and the planner has to say so rather than emit an -i with
	// an empty value that ffmpeg would read as the next argument.
	if _, err := adapter.planInput(tunerSpec(), ""); err == nil {
		t.Error("planInput accepted a tuner source with no input at all")
	}
}
