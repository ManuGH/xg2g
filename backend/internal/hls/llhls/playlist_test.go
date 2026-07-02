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

	out := renderLLPlaylist(llTestBase(), cur, 500)

	for _, want := range []string{
		"#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=1.500",
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
	out := renderLLPlaylist(llTestBase(), openSegment{}, 500)
	if strings.Contains(out, "#EXT-X-PART:") || strings.Contains(out, "PRELOAD-HINT") {
		t.Error("no parts must be advertised without an open segment")
	}
	if !strings.Contains(out, "#EXT-X-PART-INF:PART-TARGET=0.500") {
		t.Error("part-inf must always be present in LL mode")
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
