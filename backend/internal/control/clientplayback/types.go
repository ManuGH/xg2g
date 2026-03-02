package clientplayback

// Minimal client-facing DTOs for PlaybackInfo.
// Scoped strictly to DirectPlay-vs-Transcode decisions.

type PlaybackInfoRequest struct {
	DeviceProfile *DeviceProfile `json:"DeviceProfile,omitempty"`
}

type DeviceProfile struct {
	DirectPlayProfiles []DirectPlayProfile `json:"DirectPlayProfiles,omitempty"`
}

type DirectPlayProfile struct {
	Type       string `json:"Type,omitempty"`       // commonly "Video"
	Container  string `json:"Container,omitempty"`  // e.g. "mp4,m4v"
	VideoCodec string `json:"VideoCodec,omitempty"` // e.g. "h264,hevc"
	AudioCodec string `json:"AudioCodec,omitempty"` // e.g. "aac,ac3"
}

type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo `json:"MediaSources"`
	PlaySessionId *string           `json:"PlaySessionId,omitempty"`
}

type MediaSourceInfo struct {
	Id        *string `json:"Id,omitempty"`
	Path      string  `json:"Path"`
	Protocol  string  `json:"Protocol"`            // "Http"
	Container *string `json:"Container,omitempty"` // "mp4" etc.

	// Runtime ticks: 10,000,000 ticks per second.
	RunTimeTicks *int64 `json:"RunTimeTicks,omitempty"`

	SupportsDirectPlay   bool `json:"SupportsDirectPlay"`
	SupportsDirectStream bool `json:"SupportsDirectStream"`
	SupportsTranscoding  bool `json:"SupportsTranscoding"`

	TranscodingUrl         *string `json:"TranscodingUrl,omitempty"`
	TranscodingContainer   *string `json:"TranscodingContainer,omitempty"`   // e.g. "m3u8"
	TranscodingSubProtocol *string `json:"TranscodingSubProtocol,omitempty"` // e.g. "hls"
}
