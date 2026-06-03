package main

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

type storageDecisionReportOptions struct {
	DataDir      string
	PlaylistName string
	Bouquet      string
	ClientFamily string
	Intent       string
	Origin       string
	Format       string
	OutPath      string
}

type storageDecisionReport struct {
	GeneratedAt time.Time                    `json:"generatedAt"`
	DataDir     string                       `json:"dataDir"`
	Playlist    string                       `json:"playlist"`
	Bouquet     string                       `json:"bouquet,omitempty"`
	Filters     storageDecisionReportFilters `json:"filters"`
	Summary     storageDecisionReportSummary `json:"summary"`
	Warnings    []string                     `json:"warnings,omitempty"`
	Rows        []storageDecisionReportRow   `json:"rows"`
}

type storageDecisionReportFilters struct {
	ClientFamily string `json:"clientFamily,omitempty"`
	Intent       string `json:"intent,omitempty"`
	Origin       string `json:"origin,omitempty"`
	SubjectKind  string `json:"subjectKind"`
}

type storageDecisionReportSummary struct {
	ServicesTotal            int `json:"servicesTotal"`
	RowsTotal                int `json:"rowsTotal"`
	ServicesWithDecision     int `json:"servicesWithDecision"`
	ServicesWithoutDecision  int `json:"servicesWithoutDecision"`
	TruthComplete            int `json:"truthComplete"`
	TruthIncomplete          int `json:"truthIncomplete"`
	TruthMissing             int `json:"truthMissing"`
	TruthEventInactive       int `json:"truthEventInactive"`
	TruthSourceScan          int `json:"truthSourceScan"`
	TruthSourceFallback      int `json:"truthSourceFallback"`
	TruthSourceUnresolved    int `json:"truthSourceUnresolved"`
	TruthSourceEventInactive int `json:"truthSourceEventInactive"`
	DistinctHostFingerprints int `json:"distinctHostFingerprints"`
	DistinctBasisHostPairs   int `json:"distinctBasisHostPairs"`
	BasisHashesWithMultiHost int `json:"basisHashesWithMultiHost"`
	UnknownHostRows          int `json:"unknownHostRows"`
}

type storageDecisionReportRow struct {
	ServiceRef           string                                 `json:"serviceRef"`
	ChannelName          string                                 `json:"channelName"`
	Bouquet              string                                 `json:"bouquet"`
	TruthSource          string                                 `json:"truthSource"`
	TruthStatus          string                                 `json:"truthStatus"`
	DecisionPresent      bool                                   `json:"decisionPresent"`
	ScanState            string                                 `json:"scanState,omitempty"`
	ScanFailureReason    string                                 `json:"scanFailureReason,omitempty"`
	ScanContainer        string                                 `json:"scanContainer,omitempty"`
	ScanVideoCodec       string                                 `json:"scanVideoCodec,omitempty"`
	ScanAudioCodec       string                                 `json:"scanAudioCodec,omitempty"`
	ScanResolution       string                                 `json:"scanResolution,omitempty"`
	DecisionOrigin       string                                 `json:"decisionOrigin,omitempty"`
	ClientFamily         string                                 `json:"clientFamily,omitempty"`
	ClientCapsSource     string                                 `json:"clientCapsSource,omitempty"`
	ClientCapsSourceCode string                                 `json:"clientCapsSourceCode,omitempty"`
	HostFingerprint      string                                 `json:"hostFingerprint,omitempty"`
	RequestedIntent      string                                 `json:"requestedIntent,omitempty"`
	EffectiveIntent      string                                 `json:"effectiveIntent,omitempty"`
	Mode                 string                                 `json:"mode,omitempty"`
	ModeCode             string                                 `json:"modeCode,omitempty"`
	TargetProfileSummary string                                 `json:"targetProfileSummary,omitempty"`
	TargetProfile        *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	Reasons              []string                               `json:"reasons,omitempty"`
	BasisHash            string                                 `json:"basisHash,omitempty"`
	ChangedAt            *time.Time                             `json:"changedAt,omitempty"`
	LastSeenAt           *time.Time                             `json:"lastSeenAt,omitempty"`
}

type storageDecisionAuditRow struct {
	ServiceRef       string
	Origin           string
	ClientFamily     string
	ClientCapsSource string
	HostFingerprint  string
	RequestedIntent  string
	EffectiveIntent  string
	Mode             string
	TargetProfile    *playbackprofile.TargetPlaybackProfile
	Reasons          []string
	BasisHash        string
	ChangedAt        *time.Time
	LastSeenAt       *time.Time
}
