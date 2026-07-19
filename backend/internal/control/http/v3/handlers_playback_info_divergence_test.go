package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlaybackInfoDivergenceMatrix_FourWayCombinations verifies the 4-way cross product
// of Legacy vs Planner decisions. Since Planner is authoritative for live playback,
// the final DTO behavior strictly follows the Planner decision regardless of what Legacy evaluates.
func TestPlaybackInfoDivergenceMatrix_FourWayCombinations(t *testing.T) {
	tests := []struct {
		name              string
		legacyAllow       bool
		plannerAllow      bool
		capabilitiesBody  string
		wantMode          PlaybackInfoMode
		wantToken         bool
		wantDecisionMode  PlaybackDecisionMode
		wantReasonsLength int
	}{
		{
			name:         "1. Legacy Allow + Planner Allow",
			legacyAllow:  true,
			plannerAllow: true,
			capabilitiesBody: `{
				"capabilitiesVersion":3,
				"container":["hls","mpegts","ts"],
				"videoCodecs":["h264"],
				"audioCodecs":["aac"],
				"supportsHls":true,
				"allowTranscode":true
			}`,
			wantMode:          PlaybackInfoModeHls,
			wantToken:         true,
			wantDecisionMode:  PlaybackDecisionModeDirectStream,
			wantReasonsLength: 0,
		},
		{
			name:         "2. Legacy Allow + Planner Deny",
			legacyAllow:  true,
			plannerAllow: false,
			capabilitiesBody: `{
				"capabilitiesVersion":3,
				"container":["mp4"],
				"videoCodecs":["hevc"],
				"audioCodecs":["ac3"],
				"supportsHls":false,
				"allowTranscode":false
			}`,
			wantMode:          PlaybackInfoModeDeny,
			wantToken:         false,
			wantDecisionMode:  PlaybackDecisionModeDeny,
			wantReasonsLength: 1, // policy_denies_transcode
		},
		{
			name:         "3. Legacy Deny + Planner Allow",
			legacyAllow:  false,
			plannerAllow: true,
			capabilitiesBody: `{
				"capabilitiesVersion":3,
				"container":["hls","mpegts","ts"],
				"videoCodecs":["h264"],
				"audioCodecs":["aac"],
				"supportsHls":true,
				"allowTranscode":true
			}`,
			wantMode:          PlaybackInfoModeHls,
			wantToken:         true,
			wantDecisionMode:  PlaybackDecisionModeDirectStream,
			wantReasonsLength: 0,
		},
		{
			name:         "4. Legacy Deny + Planner Deny",
			legacyAllow:  false,
			plannerAllow: false,
			capabilitiesBody: `{
				"capabilitiesVersion":3,
				"container":["mp4"],
				"videoCodecs":["hevc"],
				"audioCodecs":["ac3"],
				"supportsHls":false,
				"allowTranscode":false
			}`,
			wantMode:          PlaybackInfoModeDeny,
			wantToken:         false,
			wantDecisionMode:  PlaybackDecisionModeDeny,
			wantReasonsLength: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Runtime live requests no longer execute the legacy resolver. Model its
			// characterized outcome here so the complete comparison matrix remains a
			// CI-only assertion instead of reintroducing a request-path dependency.
			legacyDecision := &decision.Decision{Mode: decision.ModeDirectStream}
			if !tc.legacyAllow {
				legacyDecision.Mode = decision.ModeDeny
			}
			plannerPlan := playbackplanner.PlaybackPlan{
				Decision: playbackplanner.DecisionAllow,
				Outcome:  playbackplanner.DecisionAllow,
				Mode:     "remux",
			}
			if !tc.plannerAllow {
				plannerPlan.Decision = playbackplanner.DecisionDeny
				plannerPlan.Outcome = playbackplanner.DecisionDeny
				plannerPlan.Mode = "none"
			}
			legacyComparable := playbackshadow.ComparableFromLegacy(legacyDecision)
			plannerComparable := playbackshadow.ComparableFromPlanner(plannerPlan)
			assert.Equal(t, map[bool]string{true: "allow", false: "deny"}[tc.legacyAllow], legacyComparable.Outcome)
			assert.Equal(t, map[bool]string{true: "allow", false: "deny"}[tc.plannerAllow], plannerComparable.Outcome)
			if tc.legacyAllow != tc.plannerAllow {
				assert.Contains(t, playbackshadow.DiffComparablePlans(legacyComparable, plannerComparable), "outcome_mismatch")
			}

			scanner := &fixedPlaybackInfoScanner{
				found: true,
				capability: scan.Capability{
					State:      scan.CapabilityStateOK,
					Container:  "mpegts",
					VideoCodec: "h264",
					AudioCodec: "aac",
					Width:      1920,
					Height:     1080,
					FPS:        25,
				},
			}

			cfg := config.AppConfig{}
			cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
			cfg.HLS.Root = "/tmp/hls"
			cfg.HLS.DVRWindow = 20 * time.Second
			store := v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{TTL: time.Minute})
			s := &Server{
				cfg:                    cfg,
				recordingsService:      new(MockRecordingsService),
				JWTSecret:              v3auth.TestSecret(),
				plannerReceiptStore:    store,
				plannerReceiptEnabled:  true,
				plannerReceiptRequired: true,
			}
			s.SetDependencies(Dependencies{Scan: scanner, RecordingsService: s.recordingsService})

			body := `{"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:","capabilities":` + tc.capabilitiesBody + `}`
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-4way-test"))

			s.PostLivePlaybackInfo(w, r)
			require.Equal(t, http.StatusOK, w.Code)

			var info PlaybackInfo
			err := json.Unmarshal(w.Body.Bytes(), &info)
			require.NoError(t, err)

			assert.Equal(t, tc.wantMode, info.Mode)
			if tc.wantToken {
				require.NotNil(t, info.PlaybackDecisionToken)
				require.NotNil(t, info.DvrWindowSeconds)
				assert.Equal(t, int64(20), *info.DvrWindowSeconds)
				claims, err := v3auth.VerifyStrict(*info.PlaybackDecisionToken, s.JWTSecret, "xg2g/v3/intents", "xg2g")
				require.NoError(t, err)
				assert.NotEmpty(t, claims.ReceiptID)
			} else {
				assert.Nil(t, info.PlaybackDecisionToken)
				assert.Nil(t, info.Url)
			}

			require.NotNil(t, info.Decision)
			assert.Equal(t, tc.wantDecisionMode, info.Decision.Mode)
			assert.Len(t, info.Decision.Reasons, tc.wantReasonsLength)
		})
	}
}
