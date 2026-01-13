package clientplayback

import "strings"

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

	c := norm(*t.Container)
	v := norm(*t.VideoCodec)
	a := norm(*t.AudioCodec)

	for _, p := range req.DeviceProfile.DirectPlayProfiles {
		if p.Type != "" && norm(p.Type) != "video" {
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

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// containsToken checks if list contains the token, where list is comma-separated.
// Empty list => no match (fail-closed).
func containsToken(list, want string) bool {
	list = strings.TrimSpace(list)
	if list == "" || want == "" {
		return false
	}
	for _, part := range strings.Split(list, ",") {
		if norm(part) == want {
			return true
		}
	}
	return false
}
