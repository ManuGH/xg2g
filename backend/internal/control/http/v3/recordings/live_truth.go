package recordings

import (
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/log"
)

// assumeLiveTruth formalizes the media properties of a live Enigma2 stream.
// Live streams cannot be predictably probed without delaying playback.
func assumeLiveTruth(serviceRef string) playback.MediaTruth {
	log.L().Debug().Str("serviceRef", serviceRef).Msg("Bypassing media truth probe for live playback with deterministic structural truth")
	return playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}
}
