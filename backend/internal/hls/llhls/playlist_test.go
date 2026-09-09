package llhls

import (
	"strings"
	"testing"
)

const ffmpegPlaylist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:12
#EXT-X-MAP:URI="init.mp4"
#EXTINF:2.000000,
seg_000012.m4s
#EXTINF:2.000000,
seg_000013.m4s
`

func llTestBase() basePlaylist {
	base, err := parseForTest(ffmpegPlaylist)
	if err != nil {
		panic(err)
	}
	return base
}

func parseForTest(raw string) (basePlaylist, error) {
	base := basePlaylist{raw: raw}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			base.mediaSeq = 12
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			base.targetDur = 2
		case line != "" && !strings.HasPrefix(line, "#"):
			base.segments = append(base.segments, line)
		}
	}
	return base, nil
}

func TestRenderLLPlaylistInjectsServerControlAndParts(t *testing.T) {
	cur := openSegment{
		name: "seg_000014.m4s",
		parts: []Fragment{
			{Offset: 0, Size: 51379, Independent: true},
			{Offset: 51379, Size: 46560},
		},
	}

	out := renderLLPlaylist(llTestBase(), cur, 500, "")

	for _, want := range []string{
		"#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=1.500,CAN-SKIP-UNTIL=12.000",
		"#EXT-X-PART-INF:PART-TARGET=0.500",
		`#EXT-X-PART:DURATION=0.500,URI="seg_000014.m4s",BYTERANGE="51379@0",INDEPENDENT=YES`,
		`#EXT-X-PART:DURATION=0.500,URI="seg_000014.m4s",BYTERANGE="46560@51379"`,
		`#EXT-X-PRELOAD-HINT:TYPE=PART,URI="seg_000014.m4s",BYTERANGE-START=97939`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in rendered playlist:\n%s", want, out)
		}
	}

	// Server-control must come right after TARGETDURATION, before segments.
	if strings.Index(out, "#EXT-X-SERVER-CONTROL") > strings.Index(out, "seg_000012.m4s") {
		t.Error("server-control tag must precede segment entries")
	}
	// FFmpeg's own lines must survive verbatim.
	if !strings.Contains(out, `#EXT-X-MAP:URI="init.mp4"`) || !strings.Contains(out, "seg_000013.m4s") {
		t.Error("base playlist content lost")
	}
}

func TestRenderLLPlaylistWithoutOpenSegment(t *testing.T) {
	out := renderLLPlaylist(llTestBase(), openSegment{}, 500, "")
	if strings.Contains(out, "#EXT-X-PART:") || strings.Contains(out, "PRELOAD-HINT") {
		t.Error("no parts must be advertised without an open segment")
	}
	if !strings.Contains(out, "#EXT-X-PART-INF:PART-TARGET=0.500") {
		t.Error("part-inf must always be present in LL mode")
	}
}

func TestRenderLLPlaylistDeltaSkipping(t *testing.T) {
	// 10 segments of 2s each (total 20s). CAN-SKIP-UNTIL=12s (6 * 2s).
	// We retain newest segments whose accumulated duration is >= 12s (6 segments: 16, 17, 18, 19, 20, 21).
	// The first 4 segments (12, 13, 14, 15) must be skipped.
	rawLongPlaylist := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:12
#EXT-X-MAP:URI="init.mp4"
#EXTINF:2.000000,
seg_000012.m4s
#EXTINF:2.000000,
seg_000013.m4s
#EXTINF:2.000000,
seg_000014.m4s
#EXTINF:2.000000,
seg_000015.m4s
#EXTINF:2.000000,
seg_000016.m4s
#EXTINF:2.000000,
seg_000017.m4s
#EXTINF:2.000000,
seg_000018.m4s
#EXTINF:2.000000,
seg_000019.m4s
#EXTINF:2.000000,
seg_000020.m4s
#EXTINF:2.000000,
seg_000021.m4s
`
	base := basePlaylist{
		raw:       rawLongPlaylist,
		mediaSeq:  12,
		targetDur: 2,
	}

	// 1. Full playlist when skipParam is empty
	full := renderLLPlaylist(base, openSegment{}, 500, "")
	if strings.Contains(full, "#EXT-X-SKIP:") {
		t.Fatalf("full playlist must not contain #EXT-X-SKIP:\n%s", full)
	}
	if !strings.Contains(full, "seg_000012.m4s") || !strings.Contains(full, "seg_000021.m4s") {
		t.Fatalf("full playlist missing segments:\n%s", full)
	}

	// 2. Delta playlist when skipParam is "YES"
	delta := renderLLPlaylist(base, openSegment{}, 500, "YES")
	if !strings.Contains(delta, "#EXT-X-SKIP:SKIPPED-SEGMENTS=4") {
		t.Fatalf("expected #EXT-X-SKIP:SKIPPED-SEGMENTS=4 in delta playlist:\n%s", delta)
	}

	// Media sequence must remain 12 per RFC 8216bis §4.4.5.2
	if !strings.Contains(delta, "#EXT-X-MEDIA-SEQUENCE:12") {
		t.Fatalf("EXT-X-MEDIA-SEQUENCE must remain unchanged:\n%s", delta)
	}

	// Skipped segments must not be present
	for _, skipped := range []string{"seg_000012.m4s", "seg_000013.m4s", "seg_000014.m4s", "seg_000015.m4s"} {
		if strings.Contains(delta, skipped) {
			t.Errorf("delta playlist contains skipped segment %s:\n%s", skipped, delta)
		}
	}

	// Retained segments must be present
	for _, kept := range []string{"seg_000016.m4s", "seg_000017.m4s", "seg_000018.m4s", "seg_000019.m4s", "seg_000020.m4s", "seg_000021.m4s"} {
		if !strings.Contains(delta, kept) {
			t.Errorf("delta playlist missing retained segment %s:\n%s", kept, delta)
		}
	}

	// 3. Short playlist under CAN-SKIP-UNTIL must not skip even with skipParam="YES"
	shortBase := basePlaylist{
		raw:       ffmpegPlaylist, // only 2 segments = 4s < 12s
		mediaSeq:  12,
		targetDur: 2,
	}
	shortDelta := renderLLPlaylist(shortBase, openSegment{}, 500, "YES")
	if strings.Contains(shortDelta, "#EXT-X-SKIP:") {
		t.Fatalf("short playlist under CAN-SKIP-UNTIL must not emit #EXT-X-SKIP:\n%s", shortDelta)
	}
	if !strings.Contains(shortDelta, "seg_000012.m4s") || !strings.Contains(shortDelta, "seg_000013.m4s") {
		t.Fatalf("short playlist must retain all segments:\n%s", shortDelta)
	}
}

func TestRenderLLPlaylistPreservesDateRangeAndProgramDateTime(t *testing.T) {
	rawWithMetadata := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:100
#EXT-X-MAP:URI="init.mp4"
#EXT-X-DATERANGE:ID="show-1",START-DATE="2026-09-09T10:00:00Z",PLANNED-DURATION=1800.0,X-TITLE="Tagesschau"
#EXT-X-PROGRAM-DATE-TIME:2026-09-09T10:00:00.000Z
#EXTINF:2.000000,
seg_000100.m4s
#EXT-X-PROGRAM-DATE-TIME:2026-09-09T10:00:02.000Z
#EXTINF:2.000000,
seg_000101.m4s
`
	base := basePlaylist{
		raw:       rawWithMetadata,
		mediaSeq:  100,
		targetDur: 2,
	}

	out := renderLLPlaylist(base, openSegment{}, 500, "")
	if !strings.Contains(out, `#EXT-X-DATERANGE:ID="show-1",START-DATE="2026-09-09T10:00:00Z",PLANNED-DURATION=1800.0,X-TITLE="Tagesschau"`) {
		t.Fatalf("EXT-X-DATERANGE tag lost in rendered playlist:\n%s", out)
	}
	if !strings.Contains(out, "#EXT-X-PROGRAM-DATE-TIME:2026-09-09T10:00:00.000Z") {
		t.Fatalf("EXT-X-PROGRAM-DATE-TIME tag lost in rendered playlist:\n%s", out)
	}
}

func TestSatisfiedLockedBlockingRules(t *testing.T) {
	tr := &Tracker{partTargetMs: 500}
	tr.base = llTestBase() // mediaSeq=12, 2 complete segments (12,13), current=14
	tr.current = openSegment{name: "seg_000014.m4s", parts: []Fragment{{Size: 1}}}

	cases := []struct {
		msn, part int
		want      bool
	}{
		{12, -1, true},  // old full segment: available
		{13, -1, true},  // newest complete segment: available
		{14, -1, false}, // current segment as a whole: not complete yet
		{14, 0, true},   // part 0 of current: exists
		{14, 1, false},  // part 1: not yet flushed
		{15, 0, false},  // next-but-one: must not unblock yet
	}
	for _, c := range cases {
		if got := tr.satisfiedLocked(c.msn, c.part); got != c.want {
			t.Errorf("satisfied(msn=%d, part=%d) = %v, want %v", c.msn, c.part, got, c.want)
		}
	}
}

func TestNextSegmentName(t *testing.T) {
	name, ok := nextSegmentName([]string{"seg_000012.m4s", "seg_000013.m4s"})
	if !ok || name != "seg_000014.m4s" {
		t.Fatalf("got %q ok=%v", name, ok)
	}
	if _, ok := nextSegmentName([]string{"stream0.ts"}); ok {
		t.Fatal("mpegts names must not produce a next segment")
	}
}
