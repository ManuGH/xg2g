package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
)

func TestResolve_ClientFamilyMatrix(t *testing.T) {
	cases := []struct {
		name                string
		clientFixture       string
		requestedProfile    string
		cap                 *scan.Capability
		hasGPU              bool
		hwaccelMode         HWAccelMode
		wantInternalProfile string
		wantPublicProfile   string
		wantTranscodeVideo  bool
		wantContainer       string
		wantDeinterlace     bool
		wantVideoCRF        int
		wantVideoQP         int
		wantPreset          string
	}{
		{
			name:                "safari macos auto progressive stays safari-compatible copy path",
			clientFixture:       playbackprofile.ClientSafariNative,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileSafari,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  false,
			wantContainer:       "mpegts",
			wantDeinterlace:     false,
		},
		{
			name:                "safari macos auto interlaced stays safari-compatible transcode path",
			clientFixture:       playbackprofile.ClientSafariNative,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: true},
			hasGPU:              true,
			hwaccelMode:         HWAccelAuto,
			wantInternalProfile: ProfileSafari,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  true,
			wantContainer:       "fmp4",
			wantDeinterlace:     true,
			wantVideoQP:         20,
		},
		{
			name:                "safari macos auto interlaced without gpu uses higher quality cpu transcode rung",
			clientFixture:       playbackprofile.ClientSafariNative,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: true},
			hasGPU:              false,
			hwaccelMode:         HWAccelAuto,
			wantInternalProfile: ProfileSafari,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  true,
			wantContainer:       "fmp4",
			wantDeinterlace:     true,
			wantVideoCRF:        20,
			wantPreset:          "slow",
		},
		{
			name:                "ios safari auto uses same native safari family semantics",
			clientFixture:       playbackprofile.ClientIOSSafariNative,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileSafari,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  false,
			wantContainer:       "mpegts",
			wantDeinterlace:     false,
		},
		{
			name:                "firefox hlsjs auto resolves to compatible high profile",
			clientFixture:       playbackprofile.ClientFirefoxHLSJS,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileHigh,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  false,
			wantContainer:       "",
			wantDeinterlace:     false,
		},
		{
			name:                "chromium hlsjs auto resolves to compatible high profile",
			clientFixture:       playbackprofile.ClientChromiumHLSJS,
			requestedProfile:    "auto",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileHigh,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  false,
			wantContainer:       "",
			wantDeinterlace:     false,
		},
		{
			name:                "firefox quality request stays in compatible live family today",
			clientFixture:       playbackprofile.ClientFirefoxHLSJS,
			requestedProfile:    "quality",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileHigh,
			wantPublicProfile:   PublicProfileCompatible,
			wantTranscodeVideo:  false,
			wantContainer:       "",
			wantDeinterlace:     false,
		},
		{
			name:                "chromium repair request resolves to repair family",
			clientFixture:       playbackprofile.ClientChromiumHLSJS,
			requestedProfile:    "repair",
			cap:                 &scan.Capability{Interlaced: false},
			wantInternalProfile: ProfileRepair,
			wantPublicProfile:   PublicProfileRepair,
			wantTranscodeVideo:  true,
			wantContainer:       "",
			wantDeinterlace:     false,
			wantVideoCRF:        28,
			wantPreset:          "veryfast",
		},
		{
			name:                "chromium transcode bridge profile remains repair family",
			clientFixture:       playbackprofile.ClientChromiumHLSJS,
			requestedProfile:    ProfileH264FMP4,
			cap:                 &scan.Capability{Interlaced: true},
			wantInternalProfile: ProfileH264FMP4,
			wantPublicProfile:   PublicProfileRepair,
			wantTranscodeVideo:  true,
			wantContainer:       "fmp4",
			wantDeinterlace:     true,
			wantVideoCRF:        28,
			wantPreset:          "veryfast",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := Resolve(tc.requestedProfile, userAgentForClientFixture(t, tc.clientFixture), 0, tc.cap, tc.hasGPU, tc.hwaccelMode)

			assert.Equal(t, tc.wantInternalProfile, spec.Name)
			assert.Equal(t, tc.wantPublicProfile, PublicProfileName(spec.Name))
			assert.Equal(t, tc.wantTranscodeVideo, spec.TranscodeVideo)
			assert.Equal(t, tc.wantContainer, spec.Container)
			assert.Equal(t, tc.wantDeinterlace, spec.Deinterlace)
			if tc.wantVideoCRF != 0 {
				assert.Equal(t, tc.wantVideoCRF, spec.VideoCRF)
			}
			if tc.wantVideoQP != 0 {
				assert.Equal(t, tc.wantVideoQP, spec.VideoQP)
			}
			if tc.wantPreset != "" {
				assert.Equal(t, tc.wantPreset, spec.Preset)
			}
		})
	}
}

func userAgentForClientFixture(t *testing.T, fixtureID string) string {
	t.Helper()

	switch fixtureID {
	case playbackprofile.ClientSafariNative:
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	case playbackprofile.ClientIOSSafariNative:
		return "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	case playbackprofile.ClientFirefoxHLSJS:
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:148.0) Gecko/20100101 Firefox/148.0"
	case playbackprofile.ClientChromiumHLSJS:
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	default:
		t.Fatalf("unknown client fixture: %s", fixtureID)
		return ""
	}
}
