package testfixtures

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

type CharacterizationTest struct {
	Name            string
	Mode            string
	SourceCap       scan.Capability
	ClientFam       string
	HostPressure    playbackprofile.HostPressureBand
	NetworkKbps     int
	NetworkRTT      int
	TruthConfidence float64
	Params          map[string]string
	WantOutcome     string
	WantProfile     string
	WantVideoRung   string
	WantVideoCodec  string
	WantContainer   string
	WantResolved    string
	AllowedDiffs    []string
}

var Cases = []CharacterizationTest{
	{
		Name:           "1_Safari_Native_H264",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		ClientFam:      playbackprofile.ClientSafariNative,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "2_Safari_Native_HEVC_4K",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "aac", Width: 3840, Height: 2160, FPS: 50},
		ClientFam:      playbackprofile.ClientSafariNative,
		Params:         map[string]string{"native_hevc_safari": "1"},
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "3_iOS_Safari",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720, FPS: 50},
		ClientFam:      playbackprofile.ClientIOSSafariNative,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "4_Chromium_HLSJS",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		ClientFam:      playbackprofile.ClientChromiumHLSJS,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "5_Constrained_WAN_Fallback",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		ClientFam:      playbackprofile.ClientChromiumHLSJS,
		HostPressure:   playbackprofile.HostPressureConstrained,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "6_Dirty_DVB_Fallback",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStatePartial, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25, Interlaced: true},
		ClientFam:      playbackprofile.ClientChromiumHLSJS,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
	{
		Name:           "7_Recording_Playback",
		Mode:           model.ModeRecording,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		ClientFam:      playbackprofile.ClientChromiumHLSJS,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},

	{
		Name:            "9_Allow_Stale_Truth",
		Mode:            model.ModeLive,
		SourceCap:       scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		TruthConfidence: 0.2, // Partial truth below usable threshold
		ClientFam:       playbackprofile.ClientChromiumHLSJS,
		WantProfile:     "high",
		WantResolved:    "compatible",
	},
	{
		Name:           "10_Host_Pressure_Clamping",
		Mode:           model.ModeLive,
		SourceCap:      scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
		HostPressure:   playbackprofile.HostPressureConstrained, // Maps to 1500 kbps constraint
		ClientFam:      playbackprofile.ClientChromiumHLSJS,
		WantProfile:    "high",
		WantVideoRung:  "",
		WantVideoCodec: "",
		WantContainer:  "",
		WantResolved:   "compatible",
	},
}
