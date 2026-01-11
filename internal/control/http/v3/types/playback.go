package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// PlaybackIntent defines what the client wants to do with the media.
type PlaybackIntent string

const (
	IntentMetadata PlaybackIntent = "metadata" // Just probe/info
	IntentBuild    PlaybackIntent = "build"    // Prepare artifacts (MP4/HLS)
	IntentStream   PlaybackIntent = "stream"   // Active streaming
)

// ClientProfile represents the capabilities of the playback client.
type ClientProfile struct {
	Name           string   `json:"name"`
	VideoCodecs    []string `json:"video_codecs"`
	AudioCodecs    []string `json:"audio_codecs"`
	Containers     []string `json:"containers"`
	MaxVideoWidth  int      `json:"max_video_width"`
	MaxVideoHeight int      `json:"max_video_height"`
	SupportsHLS    bool     `json:"supports_hls"`
}

// Hash returns a stable hash of the profile capabilities.
func (p ClientProfile) Hash() string {
	h := sha256.New()

	// Sort slices to ensure stable hash
	vCodecs := make([]string, len(p.VideoCodecs))
	copy(vCodecs, p.VideoCodecs)
	sort.Strings(vCodecs)

	aCodecs := make([]string, len(p.AudioCodecs))
	copy(aCodecs, p.AudioCodecs)
	sort.Strings(aCodecs)

	conts := make([]string, len(p.Containers))
	copy(conts, p.Containers)
	sort.Strings(conts)

	data := fmt.Sprintf("v:%s|a:%s|c:%s|w:%d|h:%d|hls:%v",
		strings.Join(vCodecs, ","),
		strings.Join(aCodecs, ","),
		strings.Join(conts, ","),
		p.MaxVideoWidth,
		p.MaxVideoHeight,
		p.SupportsHLS,
	)

	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))[:12] // 12 chars sufficient for collision-free within common profiles
}
