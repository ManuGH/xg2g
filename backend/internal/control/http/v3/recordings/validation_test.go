package recordings

import "testing"

func TestIsAllowedVideoSegment_Allowlist(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"init.mp4",
		"init_0.mp4",
		"init_1.mp4",
		"init_12.mp4",
		"sessions/abc/init_0.mp4",
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
		"init_.mp4",
		"init_x.mp4",
		"init_secret.mp4",
		"init_backup.mp4",
		"init_../foo.mp4",
		"init_0.txt",
		"init_-1.mp4",
		"init_1a.mp4",
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
