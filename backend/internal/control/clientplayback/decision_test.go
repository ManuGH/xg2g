package clientplayback

import "testing"

func TestDecideFailsClosedWithoutCompleteTruthOrProfile(t *testing.T) {
	t.Parallel()

	container, video, audio := "mp4", "h264", "aac"
	profile := &PlaybackInfoRequest{DeviceProfile: &DeviceProfile{
		DirectPlayProfiles: []DirectPlayProfile{{
			Type:       "Video",
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		}},
	}}

	tests := []struct {
		name  string
		req   *PlaybackInfoRequest
		truth Truth
	}{
		{name: "nil request", truth: Truth{Container: &container, VideoCodec: &video, AudioCodec: &audio}},
		{name: "nil device profile", req: &PlaybackInfoRequest{}, truth: Truth{Container: &container, VideoCodec: &video, AudioCodec: &audio}},
		{name: "unknown container", req: profile, truth: Truth{VideoCodec: &video, AudioCodec: &audio}},
		{name: "unknown video codec", req: profile, truth: Truth{Container: &container, AudioCodec: &audio}},
		{name: "unknown audio codec", req: profile, truth: Truth{Container: &container, VideoCodec: &video}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Decide(tt.req, tt.truth); got != DecisionTranscode {
				t.Fatalf("Decide() = %q, want %q", got, DecisionTranscode)
			}
		})
	}
}

func TestDecideMatchesNormalizedExplicitVideoProfile(t *testing.T) {
	t.Parallel()

	container, video, audio := " MP4 ", "H264", "AAC"
	req := &PlaybackInfoRequest{DeviceProfile: &DeviceProfile{
		DirectPlayProfiles: []DirectPlayProfile{
			{
				Type:       "Audio",
				Container:  "mp4",
				VideoCodec: "h264",
				AudioCodec: "aac",
			},
			{
				Type:       " video ",
				Container:  "mkv, MP4",
				VideoCodec: "hevc, h264",
				AudioCodec: "ac3, aac",
			},
		},
	}}

	if got := Decide(req, Truth{
		Container:  &container,
		VideoCodec: &video,
		AudioCodec: &audio,
	}); got != DecisionDirectPlay {
		t.Fatalf("Decide() = %q, want %q", got, DecisionDirectPlay)
	}
}

func TestDecideRejectsPartialOrMismatchedProfiles(t *testing.T) {
	t.Parallel()

	container, video, audio := "mp4", "h264", "aac"
	truth := Truth{Container: &container, VideoCodec: &video, AudioCodec: &audio}
	tests := []struct {
		name    string
		profile DirectPlayProfile
	}{
		{name: "empty container", profile: DirectPlayProfile{VideoCodec: "h264", AudioCodec: "aac"}},
		{name: "empty video", profile: DirectPlayProfile{Container: "mp4", AudioCodec: "aac"}},
		{name: "empty audio", profile: DirectPlayProfile{Container: "mp4", VideoCodec: "h264"}},
		{name: "container mismatch", profile: DirectPlayProfile{Container: "mkv", VideoCodec: "h264", AudioCodec: "aac"}},
		{name: "video mismatch", profile: DirectPlayProfile{Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac"}},
		{name: "audio mismatch", profile: DirectPlayProfile{Container: "mp4", VideoCodec: "h264", AudioCodec: "ac3"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &PlaybackInfoRequest{DeviceProfile: &DeviceProfile{
				DirectPlayProfiles: []DirectPlayProfile{tt.profile},
			}}
			if got := Decide(req, truth); got != DecisionTranscode {
				t.Fatalf("Decide() = %q, want %q", got, DecisionTranscode)
			}
		})
	}
}
