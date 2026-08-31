// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The differential, over the shared corpus, through the real wire.
//
// The corpus is already checked against both observers in their own languages -
// esaudio's corpus test and media-core's. What neither of those can say is that
// the answers survive the journey: that the feed boundaries the corpus is built
// around are the boundaries the Rust process actually sees, that the observation
// comes back describing the stream it was about, and that a field does not change
// meaning between an encoder and a decoder written months apart.
//
// So this asks the same questions across a socket and holds the answers against
// the same expectations.

const shadowCorpusPath = "../../../../../testdata/audio-corpus/corpus.txt"

type corpusStep struct {
	feed []byte
	want esaudio.Observation
}

type corpusEntry struct {
	name  string
	steps []corpusStep
}

func loadShadowCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	f, err := os.Open(shadowCorpusPath)
	if err != nil {
		t.Fatalf("open the shared corpus: %v", err)
	}
	defer func() { _ = f.Close() }()

	var (
		entries []corpusEntry
		current *corpusEntry
		pending []byte
		havFeed bool
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		head, rest, _ := strings.Cut(text, " ")
		switch head {
		case "version":
			if rest != "1" {
				t.Fatalf("the corpus is format version %q; this test reads 1", rest)
			}
		case "case":
			entries = append(entries, corpusEntry{name: rest})
			current = &entries[len(entries)-1]
		case "desc":
		case "feed":
			raw, err := hex.DecodeString(rest)
			if err != nil {
				t.Fatalf("line %d: %v", line, err)
			}
			pending, havFeed = raw, true
		case "want":
			if current == nil || !havFeed {
				t.Fatalf("line %d: a want with no feed in front of it", line)
			}
			current.steps = append(current.steps, corpusStep{feed: pending, want: parseCorpusWant(t, line, rest)})
			pending, havFeed = nil, false
		case "end":
			current = nil
		default:
			t.Fatalf("line %d: %q is not part of the corpus format", line, head)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read the corpus: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the corpus is empty")
	}
	return entries
}

func parseCorpusWant(t *testing.T, line int, rest string) esaudio.Observation {
	t.Helper()
	var o esaudio.Observation
	for _, field := range strings.Fields(rest) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("line %d: %q is not key=value", line, field)
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		switch key {
		case "channels":
			o.Channels = int(n)
		case "lfe":
			o.LFE = n == 1
		case "acmod":
			o.Acmod = uint8(n)
		case "hasAcmod":
			o.HasAcmod = n == 1
		case "dependent":
			o.DependentSubstream = n == 1
		case "frames":
			o.Frames = n
		default:
			t.Fatalf("line %d: %q is not a field of an observation", line, key)
		}
	}
	return o
}

func requireRealCore(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("XG2G_MEDIA_CORE_BIN")
	if bin == "" {
		t.Skip("XG2G_MEDIA_CORE_BIN not set; build media-core and point this at it")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("media core binary not usable: %v", err)
	}
	requireOwnableCore(t)
	return bin
}

// One feed per request, so every boundary in the corpus is a boundary the Rust
// process was actually handed - and so every step's expectation is checked at the
// step it belongs to, rather than only at the end where a stream that resynced
// differently could still arrive at the same answer.
func TestShadowCorpus_TheRealCoreAgreesFeedByFeed(t *testing.T) {
	bin := requireRealCore(t)
	corpus := loadShadowCorpus(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	shadow, err := StartAudioShadow(ctx, bin)
	if err != nil {
		t.Fatalf("StartAudioShadow: %v", err)
	}
	defer func() {
		if err := shadow.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	for i, entry := range corpus {
		// A stream of its own per case, so nothing carries over between them.
		pid := uint16(i + 1)
		reference := esaudio.NewObserver()

		for step, s := range entry.steps {
			reference.Feed(s.feed)

			got, err := shadow.ObserveAudio(ctx, []mediafacts.AudioShadowBatch{{
				PID: pid, Epoch: 1, Feeds: [][]byte{s.feed},
			}})
			if err != nil {
				t.Fatalf("%s step %d: %v", entry.name, step, err)
			}
			if len(got) != 1 {
				t.Fatalf("%s step %d: %d observations for one batch", entry.name, step, len(got))
			}
			if got[0].PID != pid || got[0].Epoch != 1 {
				t.Fatalf("%s step %d: answered about pid %d epoch %d", entry.name, step, got[0].PID, got[0].Epoch)
			}
			if fields := esaudio.Compare(s.want, got[0].Observation); len(fields) != 0 {
				t.Errorf("%s step %d: the Rust core disagrees with the corpus about %v\n  corpus %+v\n  rust   %+v",
					entry.name, step, fields, s.want, got[0].Observation)
			}
			if fields := esaudio.Compare(reference.Current(), got[0].Observation); len(fields) != 0 {
				t.Errorf("%s step %d: the two observers disagree about %v\n  go   %+v\n  rust %+v",
					entry.name, step, fields, reference.Current(), got[0].Observation)
			}
		}
	}
}

// The same corpus, asked the way a chunk actually asks: many streams and more
// than one epoch inside a single call, with several feeds per batch.
//
// A PID is reused across the epoch boundary on purpose. That is the case the
// whole differential exists for - the same number naming two different
// elementary streams - and it is the one where a peer that keyed its observers
// on the PID alone would look right until it suddenly did not.
func TestShadowCorpus_ManyStreamsAndEpochsInOneCall(t *testing.T) {
	bin := requireRealCore(t)
	corpus := loadShadowCorpus(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	shadow, err := StartAudioShadow(ctx, bin)
	if err != nil {
		t.Fatalf("StartAudioShadow: %v", err)
	}
	defer func() {
		if err := shadow.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	// Two epochs over the same handful of PIDs, each carrying a different case's
	// feeds, all in one request.
	const streams = 4
	if len(corpus) < 2*streams {
		t.Skipf("the corpus has %d cases; this needs %d", len(corpus), 2*streams)
	}

	var (
		batches   []mediafacts.AudioShadowBatch
		want      []esaudio.Observation
		labels    []string
		nextEntry int
	)
	for _, epoch := range []uint64{1, 2} {
		for s := 0; s < streams; s++ {
			entry := corpus[nextEntry]
			nextEntry++

			reference := esaudio.NewObserver()
			feeds := make([][]byte, 0, len(entry.steps))
			for _, step := range entry.steps {
				feeds = append(feeds, step.feed)
				reference.Feed(step.feed)
			}
			batches = append(batches, mediafacts.AudioShadowBatch{
				PID: uint16(s + 1), Epoch: epoch, Feeds: feeds,
			})
			want = append(want, reference.Current())
			labels = append(labels, fmt.Sprintf("%s (pid %d epoch %d)", entry.name, s+1, epoch))
		}
	}

	got, err := shadow.ObserveAudio(ctx, batches)
	if err != nil {
		t.Fatalf("ObserveAudio: %v", err)
	}
	if len(got) != len(batches) {
		t.Fatalf("%d observations for %d batches", len(got), len(batches))
	}
	for i := range got {
		if got[i].PID != batches[i].PID || got[i].Epoch != batches[i].Epoch {
			t.Fatalf("answer %d is about pid %d epoch %d, asked about pid %d epoch %d",
				i, got[i].PID, got[i].Epoch, batches[i].PID, batches[i].Epoch)
		}
		if fields := esaudio.Compare(want[i], got[i].Observation); len(fields) != 0 {
			t.Errorf("%s: the two observers disagree about %v\n  go   %+v\n  rust %+v",
				labels[i], fields, want[i], got[i].Observation)
		}
	}
}
