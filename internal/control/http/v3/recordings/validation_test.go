package recordings

import "testing"

func TestIsAllowedVideoSegment_Allowlist(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"init.mp4",
		"seg_000000.ts",
		"seg_000000.TS",
		"seg_000000.m4s",
		"seg_000000.cmfv",
		"sessions/abc/seg_000001.m4s",
	}
	for _, in := range allowed {
		if !IsAllowedVideoSegment(in) {
			t.Errorf("expected allowed: %q", in)
		}
	}

	denied := []string{
		"",
		"index.m3u8",
		"init.MP4",
		"seg_000000",
		"seg_000000.mp4",
		"foo.ts",
		"seg_000000.cmfa",
	}
	for _, in := range denied {
		if IsAllowedVideoSegment(in) {
			t.Errorf("expected denied: %q", in)
		}
	}
}
