package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestBuildStorageDecisionReport_JoinsPlaylistTruthAndCurrentDecisions(t *testing.T) {
	dataDir := t.TempDir()
	playlistPath := filepath.Join(dataDir, "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(testPremiumPlaylist()), 0600))

	scanStore, err := scan.NewSqliteStore(filepath.Join(dataDir, "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, scanStore.Close()) }()

	scanStore.Update(scan.Capability{
		ServiceRef:  "1:0:1:AAA:1:1:1:0:0:0:",
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "hevc",
		AudioCodec:  "ac3",
		Width:       3840,
		Height:      2160,
		Resolution:  "3840x2160",
		Interlaced:  true,
		LastScan:    time.Now().UTC(),
		LastSuccess: time.Now().UTC(),
		LastAttempt: time.Now().UTC(),
	})
	scanStore.Update(scan.Capability{
		ServiceRef:  "1:0:1:BBB:1:1:1:0:0:0:",
		State:       scan.CapabilityStatePartial,
		VideoCodec:  "h264",
		Resolution:  "1920x1080",
		Width:       1920,
		Height:      1080,
		LastAttempt: time.Now().UTC(),
	})
	scanStore.Update(scan.Capability{
		ServiceRef:  "1:0:1:DDD:1:1:1:0:0:0:",
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Resolution:  "1280x720",
		Width:       1280,
		Height:      720,
		LastScan:    time.Now().UTC(),
		LastSuccess: time.Now().UTC(),
		LastAttempt: time.Now().UTC(),
	})

	auditStore, err := decisionaudit.NewSqliteAuditStore(filepath.Join(dataDir, "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, auditStore.DB.Close()) }()

	require.NoError(t, auditStore.Record(context.Background(), decisionaudit.Event{
		ServiceRef:       "1:0:1:AAA:1:1:1:0:0:0:",
		SubjectKind:      "live",
		ClientFamily:     "safari",
		ClientCapsSource: "runtime_plus_family",
		DeviceType:       "tv",
		RequestedIntent:  "quality",
		ResolvedIntent:   "quality",
		Mode:             decisionaudit.ModeTranscode,
		Selected: decisionaudit.SelectedFormats{
			Container:  "fmp4",
			VideoCodec: "hevc",
			AudioCodec: "aac",
		},
		Reasons:    []decisionaudit.ReasonCode{decisionaudit.ReasonVideoCodecNotSupported},
		BasisHash:  "basis-aaa",
		TruthHash:  "truth-aaa",
		OutputHash: "output-aaa",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "fmp4",
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeTranscode,
				Codec: "hevc",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeTranscode,
				Codec: "aac",
			},
		},
		DecidedAt: time.Now().UTC(),
	}))

	require.NoError(t, auditStore.Record(context.Background(), decisionaudit.Event{
		ServiceRef:       "1:0:1:BBB:1:1:1:0:0:0:",
		SubjectKind:      "live",
		ClientFamily:     "safari",
		ClientCapsSource: "runtime_plus_family",
		DeviceType:       "tv",
		RequestedIntent:  "quality",
		ResolvedIntent:   "quality",
		Mode:             decisionaudit.ModeDirectStream,
		Selected: decisionaudit.SelectedFormats{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Reasons:    []decisionaudit.ReasonCode{decisionaudit.ReasonDirectStreamMatch},
		BasisHash:  "basis-bbb",
		TruthHash:  "truth-bbb",
		OutputHash: "output-bbb",
		DecidedAt:  time.Now().UTC(),
	}))

	report, err := buildStorageDecisionReport(storageDecisionReportOptions{
		DataDir:      dataDir,
		PlaylistName: "playlist.m3u8",
		Bouquet:      "Premium",
		Format:       "json",
	})
	require.NoError(t, err)

	require.Equal(t, 4, report.Summary.ServicesTotal)
	require.Equal(t, 4, report.Summary.RowsTotal)
	require.Equal(t, 2, report.Summary.ServicesWithDecision)
	require.Equal(t, 2, report.Summary.ServicesWithoutDecision)
	require.Equal(t, 2, report.Summary.TruthComplete)
	require.Equal(t, 1, report.Summary.TruthIncomplete)
	require.Equal(t, 1, report.Summary.TruthMissing)
	require.Equal(t, 0, report.Summary.TruthEventInactive)
	require.Equal(t, 2, report.Summary.TruthSourceScan)
	require.Equal(t, 1, report.Summary.TruthSourceFallback)
	require.Equal(t, 1, report.Summary.TruthSourceUnresolved)
	require.Equal(t, 0, report.Summary.TruthSourceEventInactive)

	rowsByName := make(map[string]storageDecisionReportRow, len(report.Rows))
	for _, row := range report.Rows {
		rowsByName[row.ChannelName] = row
	}

	alpha := rowsByName["Alpha HD"]
	require.True(t, alpha.DecisionPresent)
	assert.Equal(t, reportTruthSourceScan, alpha.TruthSource)
	assert.Equal(t, reportTruthComplete, alpha.TruthStatus)
	assert.Equal(t, decisionaudit.OriginRuntime, alpha.DecisionOrigin)
	assert.Equal(t, "runtime+family", alpha.ClientCapsSource)
	assert.Equal(t, "runtime_plus_family", alpha.ClientCapsSourceCode)
	assert.Equal(t, "quality", alpha.EffectiveIntent)
	assert.Equal(t, "transcode", alpha.Mode)
	assert.Equal(t, "transcode", alpha.ModeCode)
	assert.Equal(t, "fmp4/hevc/aac", alpha.TargetProfileSummary)

	beta := rowsByName["Beta SD"]
	require.True(t, beta.DecisionPresent)
	assert.Equal(t, reportTruthSourceFallback, beta.TruthSource)
	assert.Equal(t, reportTruthIncomplete, beta.TruthStatus)
	assert.Equal(t, decisionaudit.OriginRuntime, beta.DecisionOrigin)
	assert.Equal(t, "partial", beta.ScanState)
	assert.Equal(t, "runtime+family", beta.ClientCapsSource)
	assert.Equal(t, "runtime_plus_family", beta.ClientCapsSourceCode)
	assert.Equal(t, "quality", beta.EffectiveIntent)
	assert.Equal(t, "remux", beta.Mode)
	assert.Equal(t, "direct_stream", beta.ModeCode)

	gamma := rowsByName["Gamma News"]
	require.False(t, gamma.DecisionPresent)
	assert.Equal(t, reportTruthSourceUnresolved, gamma.TruthSource)
	assert.Equal(t, reportTruthMissing, gamma.TruthStatus)

	delta := rowsByName["Delta Plus"]
	require.False(t, delta.DecisionPresent)
	assert.Equal(t, reportTruthSourceScan, delta.TruthSource)
	assert.Equal(t, reportTruthComplete, delta.TruthStatus)
}

func TestBuildStorageDecisionReport_FindsStoresUnderDataDirStore(t *testing.T) {
	dataDir := t.TempDir()
	playlistPath := filepath.Join(dataDir, "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(testPremiumPlaylist()), 0600))
	storeDir := filepath.Join(dataDir, "store")
	require.NoError(t, os.MkdirAll(storeDir, 0755))

	db, err := sql.Open("sqlite", filepath.Join(storeDir, "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	now := time.Now().UTC()
	_, err = db.Exec(`
		CREATE TABLE capabilities (
			service_ref TEXT PRIMARY KEY,
			interlaced BOOLEAN NOT NULL DEFAULT 0,
			last_scan TEXT NOT NULL,
			resolution TEXT NOT NULL,
			codec TEXT NOT NULL,
			fps REAL NOT NULL DEFAULT 0,
			bitrate_k INTEGER NOT NULL DEFAULT 0
		)
	`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO capabilities(service_ref, interlaced, last_scan, resolution, codec, fps, bitrate_k)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "1:0:1:AAA:1:1:1:0:0:0:", false, now.Format(time.RFC3339Nano), "3840x2160", "hevc", 50.0, 0)
	require.NoError(t, err)

	auditStore, err := decisionaudit.NewSqliteAuditStore(filepath.Join(storeDir, "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, auditStore.DB.Close()) }()

	require.NoError(t, auditStore.Record(context.Background(), decisionaudit.Event{
		ServiceRef:       "1:0:1:AAA:1:1:1:0:0:0:",
		SubjectKind:      "live",
		ClientFamily:     "safari",
		ClientCapsSource: "runtime_plus_family",
		RequestedIntent:  "quality",
		ResolvedIntent:   "quality",
		Mode:             decisionaudit.ModeTranscode,
		Selected: decisionaudit.SelectedFormats{
			Container:  "fmp4",
			VideoCodec: "hevc",
			AudioCodec: "aac",
		},
		Reasons:    []decisionaudit.ReasonCode{decisionaudit.ReasonVideoCodecNotSupported},
		BasisHash:  "basis-aaa",
		TruthHash:  "truth-aaa",
		OutputHash: "output-aaa",
		DecidedAt:  now,
	}))

	report, err := buildStorageDecisionReport(storageDecisionReportOptions{
		DataDir:      dataDir,
		PlaylistName: "playlist.m3u8",
		Bouquet:      "Premium",
		Format:       "json",
	})
	require.NoError(t, err)

	require.Equal(t, 4, report.Summary.ServicesTotal)
	require.Equal(t, 1, report.Summary.ServicesWithDecision)
	require.Equal(t, 1, report.Summary.TruthIncomplete)
	require.Equal(t, 0, report.Summary.TruthEventInactive)

	rowsByName := make(map[string]storageDecisionReportRow, len(report.Rows))
	for _, row := range report.Rows {
		rowsByName[row.ChannelName] = row
	}

	alpha := rowsByName["Alpha HD"]
	require.True(t, alpha.DecisionPresent)
	assert.Equal(t, reportTruthSourceFallback, alpha.TruthSource)
	assert.Equal(t, reportTruthIncomplete, alpha.TruthStatus)
	assert.Equal(t, decisionaudit.OriginRuntime, alpha.DecisionOrigin)
	assert.Equal(t, "runtime+family", alpha.ClientCapsSource)
	assert.Equal(t, "transcode", alpha.Mode)
}

func TestBuildStorageDecisionReport_FiltersByDecisionOrigin(t *testing.T) {
	dataDir := t.TempDir()
	playlistPath := filepath.Join(dataDir, "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(testPremiumPlaylist()), 0600))

	scanStore, err := scan.NewSqliteStore(filepath.Join(dataDir, "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, scanStore.Close()) }()

	now := time.Now().UTC()
	scanStore.Update(scan.Capability{
		ServiceRef:  "1:0:1:AAA:1:1:1:0:0:0:",
		State:       scan.CapabilityStateOK,
		Container:   "ts",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Resolution:  "1920x1080",
		Width:       1920,
		Height:      1080,
		LastScan:    now,
		LastSuccess: now,
		LastAttempt: now,
	})

	auditStore, err := decisionaudit.NewSqliteAuditStore(filepath.Join(dataDir, "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, auditStore.DB.Close()) }()

	runtimeEvent := decisionaudit.Event{
		ServiceRef:       "1:0:1:AAA:1:1:1:0:0:0:",
		SubjectKind:      "live",
		Origin:           decisionaudit.OriginRuntime,
		ClientFamily:     "ios_safari_native",
		ClientCapsSource: "family_fallback",
		RequestedIntent:  "quality",
		ResolvedIntent:   "quality",
		Mode:             decisionaudit.ModeTranscode,
		Selected: decisionaudit.SelectedFormats{
			Container:  "fmp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Reasons:    []decisionaudit.ReasonCode{decisionaudit.ReasonContainerNotSupported},
		BasisHash:  "basis-runtime",
		TruthHash:  "truth-runtime",
		OutputHash: "output-runtime",
		DecidedAt:  now,
	}
	require.NoError(t, auditStore.Record(context.Background(), runtimeEvent))

	sweepEvent := runtimeEvent
	sweepEvent.Origin = decisionaudit.OriginSweep
	sweepEvent.ClientCapsSource = "family_fallback"
	sweepEvent.BasisHash = "basis-sweep"
	sweepEvent.OutputHash = "output-sweep"
	sweepEvent.Mode = decisionaudit.ModeDirectPlay
	sweepEvent.Selected = decisionaudit.SelectedFormats{Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac"}
	sweepEvent.Reasons = []decisionaudit.ReasonCode{decisionaudit.ReasonDirectPlayMatch}
	sweepEvent.DecidedAt = now.Add(time.Minute)
	require.NoError(t, auditStore.Record(context.Background(), sweepEvent))

	report, err := buildStorageDecisionReport(storageDecisionReportOptions{
		DataDir:      dataDir,
		PlaylistName: "playlist.m3u8",
		Bouquet:      "Premium",
		Origin:       decisionaudit.OriginSweep,
	})
	require.NoError(t, err)
	require.Equal(t, decisionaudit.OriginSweep, report.Filters.Origin)
	require.Len(t, report.Rows, 4)

	alphaRows := make([]storageDecisionReportRow, 0, 1)
	for _, row := range report.Rows {
		if row.ChannelName == "Alpha HD" {
			alphaRows = append(alphaRows, row)
		}
	}
	require.Len(t, alphaRows, 1)
	assert.True(t, alphaRows[0].DecisionPresent)
	assert.Equal(t, decisionaudit.OriginSweep, alphaRows[0].DecisionOrigin)
	assert.Equal(t, "family", alphaRows[0].ClientCapsSource)
	assert.Equal(t, "direct_play", alphaRows[0].Mode)
}

func TestBuildStorageDecisionReport_ClassifiesInactiveEventFeedSeparately(t *testing.T) {
	dataDir := t.TempDir()
	playlistPath := filepath.Join(dataDir, "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(testPremiumPlaylist()), 0600))

	scanStore, err := scan.NewSqliteStore(filepath.Join(dataDir, "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, scanStore.Close()) }()

	scanStore.Update(scan.Capability{
		ServiceRef:    "1:0:1:CCC:1:1:1:0:0:0:",
		State:         scan.CapabilityStateInactiveEventFeed,
		FailureReason: "ffprobe failed: signal: killed (stderr: )",
		LastAttempt:   time.Now().UTC(),
	})

	report, err := buildStorageDecisionReport(storageDecisionReportOptions{
		DataDir:      dataDir,
		PlaylistName: "playlist.m3u8",
		Bouquet:      "Premium",
	})
	require.NoError(t, err)

	rowsByName := make(map[string]storageDecisionReportRow, len(report.Rows))
	for _, row := range report.Rows {
		rowsByName[row.ChannelName] = row
	}

	gamma := rowsByName["Gamma News"]
	require.False(t, gamma.DecisionPresent)
	assert.Equal(t, reportTruthEventInactive, gamma.TruthStatus)
	assert.Equal(t, reportTruthSourceEventInactive, gamma.TruthSource)
	assert.Equal(t, string(scan.CapabilityStateInactiveEventFeed), gamma.ScanState)
	assert.Equal(t, 1, report.Summary.TruthEventInactive)
	assert.Equal(t, 1, report.Summary.TruthSourceEventInactive)
	assert.Equal(t, 3, report.Summary.TruthSourceUnresolved)
}

func TestSummarizeStorageDecisionReportTracksHostBuckets(t *testing.T) {
	rows := []storageDecisionReportRow{
		{
			ServiceRef:      "svc-1",
			TruthSource:     reportTruthSourceScan,
			TruthStatus:     reportTruthComplete,
			DecisionPresent: true,
			BasisHash:       "basis-a",
			HostFingerprint: "df1:host-a",
		},
		{
			ServiceRef:      "svc-2",
			TruthSource:     reportTruthSourceScan,
			TruthStatus:     reportTruthComplete,
			DecisionPresent: true,
			BasisHash:       "basis-a",
			HostFingerprint: "df1:host-b",
		},
		{
			ServiceRef:      "svc-3",
			TruthSource:     reportTruthSourceFallback,
			TruthStatus:     reportTruthIncomplete,
			DecisionPresent: true,
			BasisHash:       "basis-c",
		},
	}

	summary := summarizeStorageDecisionReport(rows)
	require.Equal(t, 3, summary.DistinctHostFingerprints)
	require.Equal(t, 3, summary.DistinctBasisHostPairs)
	require.Equal(t, 1, summary.BasisHashesWithMultiHost)
	require.Equal(t, 1, summary.UnknownHostRows)

	warnings := buildStorageDecisionReportWarnings(summary)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], reportUnknownHost)
}

func TestRenderStorageDecisionReportTableIncludesHostFingerprintWarnings(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	report := storageDecisionReport{
		GeneratedAt: now,
		DataDir:     "/tmp/xg2g",
		Playlist:    "playlist.m3u8",
		Filters: storageDecisionReportFilters{
			SubjectKind: "live",
		},
		Summary: storageDecisionReportSummary{
			ServicesTotal:            1,
			RowsTotal:                1,
			ServicesWithDecision:     1,
			DistinctHostFingerprints: 1,
			DistinctBasisHostPairs:   1,
			UnknownHostRows:          1,
		},
		Warnings: []string{"1 decision row(s) are bucketed as unknown_host"},
		Rows: []storageDecisionReportRow{
			{
				ServiceRef:       "svc-1",
				ChannelName:      "Sky Sport Austria 1",
				Bouquet:          "Sports",
				TruthSource:      reportTruthSourceScan,
				TruthStatus:      reportTruthComplete,
				DecisionPresent:  true,
				DecisionOrigin:   "runtime",
				ClientFamily:     "safari_native",
				ClientCapsSource: "runtime",
				HostFingerprint:  reportUnknownHost,
				RequestedIntent:  "quality",
				EffectiveIntent:  "quality",
				Mode:             "transcode",
				BasisHash:        "basis-a",
				ChangedAt:        &now,
			},
		},
	}

	var out strings.Builder
	renderStorageDecisionReportTable(&out, report)

	rendered := out.String()
	require.Contains(t, rendered, "HOST_FINGERPRINT")
	require.Contains(t, rendered, "unknown_host")
	require.Contains(t, rendered, "Warning:")
}

func TestQueryDecisionCurrentRows_ReadsOptionalHostFingerprint(t *testing.T) {
	db, err := sql.Open("sqlite", t.TempDir()+"/decision_audit.sqlite")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
	CREATE TABLE decision_current (
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		shadow_json TEXT,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		host_fingerprint TEXT,
		changed_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER NOT NULL
	);
	INSERT INTO decision_current (
		service_ref, subject_kind, origin, client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		shadow_json, resolved_intent, host_pressure_band, client_caps_source, device_type, host_fingerprint, changed_at_ms, last_seen_at_ms
	) VALUES (
		'1:0:1:2B66:3F3:1:C00000:0:0:0:', 'live', 'runtime', 'safari_native', 'quality', 'basis-a', 'truth-a', 'output-a',
		'transcode', 'fmp4', 'h264', 'aac', NULL, '[]', NULL, 'quality', 'normal', 'runtime', 'web', 'df1:host-a', 1000, 1000
	);
	`)
	require.NoError(t, err)

	columns, err := loadSQLiteColumnSet(db, "decision_current")
	require.NoError(t, err)

	rows, err := queryDecisionCurrentRows(db, columns, "1:0:1:2B66:3F3:1:C00000:0:0:0:", "safari_native", "quality", "runtime")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "df1:host-a", rows[0].HostFingerprint)
}

func testPremiumPlaylist() string {
	return `#EXTM3U
#EXTINF:-1 tvg-id="Alpha" group-title="Premium",Alpha HD
http://127.0.0.1/web/stream.m3u?ref=1:0:1:AAA:1:1:1:0:0:0:&name=Alpha%20HD
#EXTINF:-1 tvg-id="Beta" group-title="Premium",Beta SD
http://127.0.0.1/web/stream.m3u?ref=1:0:1:BBB:1:1:1:0:0:0:&name=Beta%20SD
#EXTINF:-1 tvg-id="Gamma" group-title="Premium",Gamma News
http://127.0.0.1/web/stream.m3u?ref=1:0:1:CCC:1:1:1:0:0:0:&name=Gamma%20News
#EXTINF:-1 tvg-id="Delta" group-title="Premium",Delta Plus
http://127.0.0.1/web/stream.m3u?ref=1:0:1:DDD:1:1:1:0:0:0:&name=Delta%20Plus
`
}
