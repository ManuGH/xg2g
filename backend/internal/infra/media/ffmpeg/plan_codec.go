package ffmpeg

import (
	"fmt"
	"github.com/ManuGH/xg2g/internal/config"
	codecdecision "github.com/ManuGH/xg2g/internal/decision"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"strings"
)

func (a *LocalAdapter) planCodec(spec ports.StreamSpec) (codecPlan, error) {
	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, normalizeRequestedCodec(spec.Profile.VideoCodec)) {
		resolvedCodec := normalizeRequestedCodec(spec.Profile.VideoCodec)
		if resolvedCodec == "" {
			resolvedCodec = "h264"
		}
		metrics.RecordDecisionSummary(
			spec.Profile.Name,
			"direct",
			resolvedCodec,
			false,
			"direct_play_supported",
		)

		a.Logger.Info().
			Str("event", "decision.summary").
			Str("profile", spec.Profile.Name).
			Str("requested_codec", resolvedCodec).
			Strs("supported_hw_codecs", a.supportedHWCodecs()).
			Bool("hwaccel_available", false).
			Str("path", "direct").
			Str("output_codec", resolvedCodec).
			Bool("use_hwaccel", false).
			Str("reason", "direct_play_supported").
			Msg("decision summary")

		return codecPlan{
			resolvedCodec: resolvedCodec,
			useHW:         false,
			hwBackend:     profiles.GPUBackendNone,
			fullVAAPI:     false,
			preInputArgs:  nil,
		}, nil
	}

	requestedBackend := backendForHWAccel(spec.Profile.HWAccel)
	useHWPath := requestedBackend != profiles.GPUBackendNone

	hardHWRequest := requestedBackend != profiles.GPUBackendNone && !isPreferHWProfile(spec.Profile.Name)
	decisionIn := codecdecision.Input{
		Profile:        spec.Profile.Name,
		RequestedCodec: spec.Profile.VideoCodec,
		RequireHW:      hardHWRequest,
		Server: codecdecision.ServerCapabilities{
			HWAccelAvailable:  useHWPath,
			SupportedHWCodecs: a.supportedHWCodecs(),
			AutoHWCodecs:      a.autoHWCodecs(),
		},
	}
	neg := codecdecision.Decide(decisionIn)
	decisionInSummary := decisionIn.Summary()
	decisionOutSummary := neg.Summary()
	metrics.RecordDecisionSummary(
		decisionInSummary.Profile,
		decisionOutSummary.Path,
		decisionOutSummary.OutputCodec,
		decisionOutSummary.UseHWAccel,
		decisionOutSummary.Reason,
	)

	a.Logger.Info().
		Str("event", "decision.summary").
		Str("profile", decisionInSummary.Profile).
		Str("requested_codec", decisionInSummary.RequestedCodec).
		Strs("supported_hw_codecs", decisionInSummary.SupportedHWCodecs).
		Strs("auto_hw_codecs", decisionInSummary.AutoHWCodecs).
		Bool("hwaccel_available", decisionInSummary.HWAccelAvailable).
		Str("path", decisionOutSummary.Path).
		Str("output_codec", decisionOutSummary.OutputCodec).
		Bool("use_hwaccel", decisionOutSummary.UseHWAccel).
		Str("reason", decisionOutSummary.Reason).
		Msg("decision summary")

	if neg.Path == codecdecision.PathReject && !hardHWRequest {
		return codecPlan{}, fmt.Errorf("codec negotiation rejected (profile=%s codec=%s reason=%s)", spec.Profile.Name, spec.Profile.VideoCodec, neg.Reason)
	}

	resolvedCodec := neg.OutputCodec
	if resolvedCodec == "" && hardHWRequest {
		resolvedCodec = normalizeRequestedCodec(spec.Profile.VideoCodec)
	}
	if resolvedCodec == "" {
		resolvedCodec = "h264"
	}

	useHW := neg.Path == codecdecision.PathTranscodeHW
	if hardHWRequest {
		useHW = true
	}
	hwBackend := profiles.GPUBackendNone
	fullVAAPI := false
	pathID := ""

	preInputArgs := make([]string, 0, 6)
	if useHW {
		hwBackend = requestedBackend
		if hwBackend == profiles.GPUBackendNone {
			hwBackend = a.detector.preferredHardwareBackendForCodec(resolvedCodec)
		}
		if hwBackend == profiles.GPUBackendNone {
			return codecPlan{}, fmt.Errorf("hardware path selected but no verified backend is available for codec %s", resolvedCodec)
		}

		switch hwBackend {
		case profiles.GPUBackendVAAPI:
			if spec.Profile.Deinterlace && !vaapiInterlacedCodecIsSafe(resolvedCodec) {
				if experimentalAllowUnverifiedInterlacedVAAPICodec(resolvedCodec) {
					a.Logger.Warn().
						Str("requested_codec", resolvedCodec).
						Str("override_env", experimentalInterlacedVAAPICodecsEnv).
						Msg("allowing unverified interlaced vaapi codec via experimental override")
				} else if !a.anyVerifiedVAAPIInterlacedPathForCodec(resolvedCodec, profiles.IsFullVAAPIProfile(spec.Profile.HWAccel)) {
					a.Logger.Warn().
						Str("requested_codec", resolvedCodec).
						Str("fallback_codec", "h264").
						Msg("interlaced vaapi codec downgraded until path correctness is verified")
					resolvedCodec = "h264"
				}
			}
			if a.VaapiDevice == "" {
				return codecPlan{}, fmt.Errorf("vaapi requested by profile but no vaapi device configured on adapter")
			}
			reqEncoder, ok := codecToVAAPIEncoder(resolvedCodec)
			if !ok {
				return codecPlan{}, fmt.Errorf("unsupported vaapi codec resolved by decision engine: %s", resolvedCodec)
			}
			if !a.detector.VaapiEncoderVerified(reqEncoder) {
				return codecPlan{}, fmt.Errorf("vaapi encoder %s not verified by preflight (device=%s, deviceErr=%v)", reqEncoder, a.VaapiDevice, a.detector.vaapiDeviceErr)
			}
			preInputArgs = append(preInputArgs, "-vaapi_device", a.VaapiDevice)
			fullVAAPI = profiles.IsFullVAAPIProfile(spec.Profile.HWAccel)
			if normalizeRequestedCodec(resolvedCodec) == "av1" {
				// Keep AV1 on the encode-only path even when a caller requested
				// full VAAPI. This preserves a software-domain normalization step
				// before hwupload, which is required to avoid malformed 1080p AV1
				// output on current AMD VAAPI stacks.
				fullVAAPI = false
			}
			if spec.Profile.Deinterlace {
				fullPathID := vaapiPathCorrectnessIDFor(resolvedCodec, true)
				if fullVAAPI && fullPathID != "" {
					capability, ok := hardware.HardwarePathCapabilityFor(fullPathID)
					if !ok || capability.Status != hardware.PathStatusVerified {
						fullVAAPI = false
						a.Logger.Info().
							Str("path_id", fullPathID).
							Str("status", capability.Status).
							Str("reason", capability.Reason).
							Msg("vaapi full pipeline disabled by path correctness matrix")
					}
				}
				if fullVAAPI {
					pathID = fullPathID
				} else {
					pathID = vaapiPathCorrectnessIDFor(resolvedCodec, false)
				}
			}
			if fullVAAPI {
				preInputArgs = append(preInputArgs,
					"-hwaccel", "vaapi",
					"-hwaccel_output_format", "vaapi",
				)
			}
		case profiles.GPUBackendNVENC:
			reqEncoder, ok := codecToNVENCEncoder(resolvedCodec)
			if !ok {
				return codecPlan{}, fmt.Errorf("unsupported nvenc codec resolved by decision engine: %s", resolvedCodec)
			}
			if !a.detector.NVENCEncoderVerified(reqEncoder) {
				return codecPlan{}, fmt.Errorf("nvenc encoder %s not verified by preflight (nvencErr=%v)", reqEncoder, a.detector.nvencErr)
			}
		default:
			return codecPlan{}, fmt.Errorf("unsupported hardware backend %q", hwBackend)
		}
	}

	return codecPlan{
		resolvedCodec: resolvedCodec,
		useHW:         useHW,
		hwBackend:     hwBackend,
		fullVAAPI:     fullVAAPI,
		preInputArgs:  preInputArgs,
		pathID:        pathID,
	}, nil
}

// resolvedExecutedHWAccel maps the resolved codec plan to the hwAccel label the
// emitted argv actually carries: full VAAPI emits "-hwaccel vaapi" ("vaapi"); a
// *_vaapi encoder without it is encode-only ("vaapi_encode_only"); NVENC is
// "nvenc"; copy and CPU emit no "-hwaccel" (empty = none). Used to keep the
// effective profile — and the predicted FFmpeg plan derived from it — in
// lockstep with execution, so a planner downgrade (full VAAPI -> encode-only,
// or GPU -> CPU fallback) never diverges the prediction from the real argv.
func resolvedExecutedHWAccel(codec codecPlan) string {
	if !codec.useHW {
		return ""
	}
	switch codec.hwBackend {
	case profiles.GPUBackendVAAPI:
		if codec.fullVAAPI {
			return "vaapi"
		}
		return "vaapi_encode_only"
	case profiles.GPUBackendNVENC:
		return "nvenc"
	default:
		return ""
	}
}

func vaapiPathCorrectnessIDFor(codec string, full bool) string {
	switch strings.TrimSpace(codec) {
	case "hevc":
		if full {
			return hardware.PathVAAPIFullInterlacedHEVC
		}
		return hardware.PathVAAPIEncodeOnlyInterlacedHEVC
	case "av1":
		if full {
			return hardware.PathVAAPIFullInterlacedAV1
		}
		return hardware.PathVAAPIEncodeOnlyInterlacedAV1
	default:
		return ""
	}
}

func vaapiInterlacedCodecIsSafe(codec string) bool {
	switch strings.TrimSpace(codec) {
	case "hevc", "av1":
		return false
	default:
		return true
	}
}

func (a *LocalAdapter) anyVerifiedVAAPIInterlacedPathForCodec(codec string, fullRequested bool) bool {
	codec = normalizeRequestedCodec(codec)
	if codec == "av1" {
		// AV1 VAAPI is intentionally forced through encode-only later in the
		// planner, so a verified full path alone must not unlock it.
		fullRequested = false
	}

	candidates := []bool{false}
	if fullRequested {
		candidates = []bool{true, false}
	}
	for _, full := range candidates {
		pathID := vaapiPathCorrectnessIDFor(codec, full)
		if pathID == "" {
			continue
		}
		capability, ok := hardware.HardwarePathCapabilityFor(pathID)
		if ok && capability.Status == hardware.PathStatusVerified {
			return true
		}
	}
	return false
}

func experimentalAllowUnverifiedInterlacedVAAPICodec(codec string) bool {
	codec = normalizeRequestedCodec(codec)
	if codec != "hevc" && codec != "av1" {
		return false
	}
	raw := strings.TrimSpace(config.ParseString(experimentalInterlacedVAAPICodecsEnv, ""))
	if raw == "" {
		return false
	}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if normalizeRequestedCodec(item) == codec {
			return true
		}
	}
	return false
}

func strictLiveIngestCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "av1":
		return true
	default:
		return false
	}
}

func (a *LocalAdapter) supportedHWCodecs() []string {
	return a.supportedHWCodecsLocal()
}

func (a *LocalAdapter) autoHWCodecs() []string {
	return a.autoHWCodecsLocal()
}

func isPreferHWProfile(profileName string) bool {
	p := strings.ToLower(strings.TrimSpace(profileName))
	return p == "av1_hw" || strings.HasSuffix(p, "_hw") || strings.HasSuffix(p, "_hw_ll")
}

func codecToVAAPIEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		return "h264_vaapi", true
	case "hevc":
		return "hevc_vaapi", true
	case "av1":
		return "av1_vaapi", true
	default:
		return "", false
	}
}

func codecToNVENCEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		return "h264_nvenc", true
	case "hevc":
		return "hevc_nvenc", true
	case "av1":
		return "av1_nvenc", true
	default:
		return "", false
	}
}

func backendForHWAccel(hwaccel string) profiles.GPUBackend {
	switch strings.ToLower(strings.TrimSpace(hwaccel)) {
	case "vaapi", "vaapi_encode_only":
		return profiles.GPUBackendVAAPI
	case "nvenc":
		return profiles.GPUBackendNVENC
	default:
		return profiles.GPUBackendNone
	}
}

func normalizeRequestedCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch c {
	case "", "h264", "avc", "avc1", "libx264", "h264_vaapi", "h264_nvenc":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi", "hevc_nvenc":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "av1_nvenc", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return c
	}
}
