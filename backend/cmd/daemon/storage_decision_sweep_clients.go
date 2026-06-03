package main

import (
	"fmt"
	"sort"
	"strings"

	recordingcaps "github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func selectStorageDecisionSweepClientProfiles(requested []string) ([]storageDecisionSweepClientProfile, error) {
	available := make(map[string]storageDecisionSweepClientProfile, len(playbackprofile.ClientFixtureIDs()))
	for _, family := range playbackprofile.ClientFixtureIDs() {
		available[family] = storageDecisionSweepClientProfile{Name: family}
	}
	if len(requested) == 0 {
		requested = []string{"ios_safari_native", "chromium_hlsjs"}
	}

	out := make([]storageDecisionSweepClientProfile, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, raw := range requested {
		name := normalize.Token(raw)
		if name == "" || seen[name] {
			continue
		}
		profile, ok := available[name]
		if !ok {
			known := make([]string, 0, len(available))
			for key := range available {
				known = append(known, key)
			}
			sort.Strings(known)
			return nil, fmt.Errorf("unsupported client family %q (known: %s)", raw, strings.Join(known, ", "))
		}
		seen[name] = true
		out = append(out, profile)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid client families selected")
	}
	return out, nil
}

func storageDecisionSweepFamilyCaps(family string) recordingcaps.PlaybackCapabilities {
	return recordingcaps.ResolveRuntimeProbeCapabilities(recordingcaps.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		ClientFamilyFallback: family,
	})
}

func cloneStorageDecisionSweepCaps(in recordingcaps.PlaybackCapabilities) *recordingcaps.PlaybackCapabilities {
	out := in
	if in.AllowTranscode != nil {
		v := *in.AllowTranscode
		out.AllowTranscode = &v
	}
	if in.SupportsRange != nil {
		v := *in.SupportsRange
		out.SupportsRange = &v
	}
	if in.MaxVideo != nil {
		out.MaxVideo = &recordingcaps.MaxVideo{
			Width:  in.MaxVideo.Width,
			Height: in.MaxVideo.Height,
			Fps:    in.MaxVideo.Fps,
		}
	}
	out.Containers = append([]string(nil), in.Containers...)
	out.VideoCodecs = append([]string(nil), in.VideoCodecs...)
	out.AudioCodecs = append([]string(nil), in.AudioCodecs...)
	out.HLSEngines = append([]string(nil), in.HLSEngines...)
	out.VideoCodecSignals = append([]recordingcaps.VideoCodecSignal(nil), in.VideoCodecSignals...)
	return &out
}

func storageDecisionSweepClientFamilyNames(profiles []storageDecisionSweepClientProfile) []string {
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profile.Name)
	}
	return out
}

func storageDecisionSweepReasonStrings(reasons []decisionaudit.ReasonCode) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, string(reason))
	}
	return out
}

func storageDecisionSweepServiceNames(rows []storageDecisionSweepServiceNote) string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ChannelName) != "" {
			names = append(names, row.ChannelName)
			continue
		}
		names = append(names, row.ServiceRef)
	}
	return strings.Join(names, ", ")
}
