package intents

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestReusableLiveClientPathCompatible_AllowsEquivalentProfilesAcrossPaths(t *testing.T) {
	session := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "transcode",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			HWAccel:        "vaapi_encode_only",
			VideoQP:        20,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
			Container:      "fmp4",
		},
	}
	candidate := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "native_hls",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			HWAccel:        "vaapi_encode_only",
			VideoQP:        20,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
			Container:      "fmp4",
		},
	}

	if !reusableLiveClientPathCompatible(session, candidate) {
		t.Fatal("expected equivalent live profiles to be reusable across client paths")
	}
}

func TestReusableLiveClientPathCompatible_RejectsDifferentProfilesAcrossPaths(t *testing.T) {
	session := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "transcode",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			HWAccel:        "vaapi_encode_only",
			VideoQP:        20,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
			Container:      "fmp4",
		},
	}
	candidate := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "native_hls",
		},
		Profile: model.ProfileSpec{
			Name:           "safari",
			TranscodeVideo: false,
			AudioBitrateK:  192,
			Container:      "mpegts",
		},
	}

	if reusableLiveClientPathCompatible(session, candidate) {
		t.Fatal("expected different live profiles to stay isolated across client paths")
	}
}

func TestReusableLiveClientPathCompatible_PreservesExactPathMatch(t *testing.T) {
	session := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "native_hls",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			Container:      "fmp4",
		},
	}
	candidate := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientPath: "native_hls",
		},
		Profile: model.ProfileSpec{
			Name:           "different",
			TranscodeVideo: false,
			Container:      "mpegts",
		},
	}

	if !reusableLiveClientPathCompatible(session, candidate) {
		t.Fatal("expected exact client path match to remain reusable")
	}
}

func TestMatchSessionIdentity_AllowsEquivalentProfilesForSamePrincipalAcrossCapHashes(t *testing.T) {
	session := &model.SessionRecord{
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			HWAccel:        "vaapi_encode_only",
			VideoQP:        20,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
			Container:      "fmp4",
		},
	}
	intent := Intent{
		PrincipalID:   "viewer-1",
		ClientCapHash: "cap-a",
	}
	candidate := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyPrincipalID: "viewer-1",
			"capHash":               "cap-b",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			HWAccel:        "vaapi_encode_only",
			VideoQP:        20,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
			Container:      "fmp4",
		},
	}

	if !matchSessionIdentity(intent, session, candidate) {
		t.Fatal("expected same-principal sessions with equivalent profiles to match across cap hashes")
	}
}

func TestMatchSessionIdentity_RejectsDifferentCapHashesWithoutPrincipalBridge(t *testing.T) {
	session := &model.SessionRecord{
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			Container:      "fmp4",
		},
	}
	intent := Intent{
		ClientCapHash: "cap-a",
	}
	candidate := &model.SessionRecord{
		ContextData: map[string]string{
			"capHash": "cap-b",
		},
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			TranscodeVideo: true,
			VideoCodec:     "hevc",
			Container:      "fmp4",
		},
	}

	if matchSessionIdentity(intent, session, candidate) {
		t.Fatal("expected cap-hash mismatch without principal bridge to stay isolated")
	}
}
