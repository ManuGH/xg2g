package clientplayback

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/normalize"
)

type Truth struct {
	Container  *string
	VideoCodec *string
	AudioCodec *string
}

type Decision string

const (
	DecisionDirectPlay Decision = "direct_play"
	DecisionTranscode  Decision = "transcode"
)

// Decide implements strict fail-closed DirectPlay eligibility.
// Rules:
// - If any of container/video/audio is unknown => Transcode.
// - If no DeviceProfile => Transcode (fail-closed).
// - If any DirectPlayProfile explicitly matches => DirectPlay.
func Decide(req *PlaybackInfoRequest, t Truth) Decision {
	if t.Container == nil || t.VideoCodec == nil || t.AudioCodec == nil {
		return DecisionTranscode
	}
	if req == nil || req.DeviceProfile == nil {
		return DecisionTranscode
	}

	c := normalize.Token(*t.Container)
	v := normalize.Token(*t.VideoCodec)
	a := normalize.Token(*t.AudioCodec)

	for _, p := range req.DeviceProfile.DirectPlayProfiles {
		if p.Type != "" && normalize.Token(p.Type) != "video" {
			continue
		}
		if !containsToken(p.Container, c) {
			continue
		}
		if !containsToken(p.VideoCodec, v) {
			continue
		}
		if !containsToken(p.AudioCodec, a) {
			continue
		}
		return DecisionDirectPlay
	}

	return DecisionTranscode
}

// containsToken checks if list contains the token, where list is comma-separated.
// Empty list => no match (fail-closed).
func containsToken(list, want string) bool {
	list = strings.TrimSpace(list)
	want = normalize.Token(want)
	if list == "" || want == "" {
		return false
	}
	for _, part := range strings.Split(list, ",") {
		if normalize.Token(part) == want {
			return true
		}
	}
	return false
}
