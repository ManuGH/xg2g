package main

import (
	"time"
)

type storageDecisionSweepOptions struct {
	ConfigPath        string
	DataDir           string
	PlaylistName      string
	Bouquet           string
	ChannelNamesCSV   string
	ServiceRefsCSV    string
	ClientFamiliesCSV string
	RequestedProfile  string
	APIVersion        string
	SchemaType        string
	Limit             int
	SkipScan          bool
	StatePath         string
	NoState           bool
	Timeout           time.Duration
	ProbeDelay        time.Duration
	Format            string
	OutPath           string
}

type storageDecisionSweep struct {
	GeneratedAt      time.Time                      `json:"generatedAt"`
	ConfigPath       string                         `json:"configPath,omitempty"`
	DataDir          string                         `json:"dataDir"`
	Playlist         string                         `json:"playlist"`
	Bouquet          string                         `json:"bouquet,omitempty"`
	RequestedProfile string                         `json:"requestedProfile"`
	SkipScan         bool                           `json:"skipScan,omitempty"`
	StatePath        string                         `json:"statePath,omitempty"`
	ScopeKey         string                         `json:"scopeKey,omitempty"`
	ClientFamilies   []string                       `json:"clientFamilies"`
	Summary          storageDecisionSweepSummary    `json:"summary"`
	Diff             *storageDecisionSweepDiff      `json:"diff,omitempty"`
	ScannedServices  []storageDecisionSweepScanRow  `json:"scannedServices"`
	Decisions        []storageDecisionSweepDecision `json:"decisions"`
}

type storageDecisionSweepSummary struct {
	ServicesSelected         int `json:"servicesSelected"`
	TruthComplete            int `json:"truthComplete"`
	TruthIncomplete          int `json:"truthIncomplete"`
	TruthMissing             int `json:"truthMissing"`
	TruthEventInactive       int `json:"truthEventInactive"`
	ServicesWithDecision     int `json:"servicesWithDecision"`
	DecisionRows             int `json:"decisionRows"`
	DecisionErrors           int `json:"decisionErrors"`
	TruthSourceScan          int `json:"truthSourceScan"`
	TruthSourceFallback      int `json:"truthSourceFallback"`
	TruthSourceUnresolved    int `json:"truthSourceUnresolved"`
	TruthSourceEventInactive int `json:"truthSourceEventInactive"`
}

type storageDecisionSweepScanRow struct {
	ServiceRef    string `json:"serviceRef"`
	ChannelName   string `json:"channelName"`
	Bouquet       string `json:"bouquet"`
	TruthStatus   string `json:"truthStatus"`
	TruthSource   string `json:"truthSource"`
	ScanState     string `json:"scanState,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
	Container     string `json:"container,omitempty"`
	VideoCodec    string `json:"videoCodec,omitempty"`
	AudioCodec    string `json:"audioCodec,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
}

type storageDecisionSweepDecision struct {
	ServiceRef           string   `json:"serviceRef"`
	ChannelName          string   `json:"channelName"`
	Bouquet              string   `json:"bouquet"`
	ClientFamily         string   `json:"clientFamily"`
	ClientCapsSource     string   `json:"clientCapsSource,omitempty"`
	ClientCapsSourceCode string   `json:"clientCapsSourceCode,omitempty"`
	TruthStatus          string   `json:"truthStatus"`
	TruthSource          string   `json:"truthSource"`
	Mode                 string   `json:"mode,omitempty"`
	ModeCode             string   `json:"modeCode,omitempty"`
	EffectiveIntent      string   `json:"effectiveIntent,omitempty"`
	TargetProfileSummary string   `json:"targetProfileSummary,omitempty"`
	Reasons              []string `json:"reasons,omitempty"`
	Error                string   `json:"error,omitempty"`
}

type storageDecisionSweepDiff struct {
	StatePath           string                              `json:"statePath,omitempty"`
	BaselineFound       bool                                `json:"baselineFound"`
	BaselineGeneratedAt *time.Time                          `json:"baselineGeneratedAt,omitempty"`
	ScopeChanged        bool                                `json:"scopeChanged,omitempty"`
	RelevantChanges     int                                 `json:"relevantChanges"`
	ModeChanges         []storageDecisionSweepModeChange    `json:"modeChanges,omitempty"`
	TruthChanges        []storageDecisionSweepTruthChange   `json:"truthChanges,omitempty"`
	Coverage            *storageDecisionSweepCoverageChange `json:"coverage,omitempty"`
}

type storageDecisionSweepModeChange struct {
	ServiceRef   string `json:"serviceRef"`
	ChannelName  string `json:"channelName"`
	ClientFamily string `json:"clientFamily"`
	FromMode     string `json:"fromMode"`
	ToMode       string `json:"toMode"`
}

type storageDecisionSweepTruthChange struct {
	ServiceRef      string `json:"serviceRef"`
	ChannelName     string `json:"channelName"`
	FromTruth       string `json:"fromTruth"`
	ToTruth         string `json:"toTruth"`
	FromTruthStatus string `json:"fromTruthStatus,omitempty"`
	ToTruthStatus   string `json:"toTruthStatus,omitempty"`
}

type storageDecisionSweepCoverageChange struct {
	FallbackBefore   int                               `json:"fallbackBefore"`
	FallbackAfter    int                               `json:"fallbackAfter"`
	FallbackDelta    int                               `json:"fallbackDelta"`
	UnresolvedBefore int                               `json:"unresolvedBefore"`
	UnresolvedAfter  int                               `json:"unresolvedAfter"`
	UnresolvedDelta  int                               `json:"unresolvedDelta"`
	Regression       bool                              `json:"regression"`
	NewFallback      []storageDecisionSweepServiceNote `json:"newFallback,omitempty"`
	NewUnresolved    []storageDecisionSweepServiceNote `json:"newUnresolved,omitempty"`
}

type storageDecisionSweepServiceNote struct {
	ServiceRef  string `json:"serviceRef"`
	ChannelName string `json:"channelName"`
}

type storageDecisionSweepClientProfile struct {
	Name string
}

type storageDecisionSweepSelection struct {
	Name       string
	Group      string
	ServiceRef string
	Raw        string
	URL        string
}
