package diagnostics

import (
	"time"
)

// HealthStatus represents the health state of a subsystem.
type HealthStatus int

const (
	Unknown HealthStatus = iota
	OK
	Degraded
	Unavailable
)

func (h HealthStatus) String() string {
	switch h {
	case Unknown:
		return "unknown"
	case OK:
		return "ok"
	case Degraded:
		return "degraded"
	case Unavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

func (h HealthStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + h.String() + `"`), nil
}

// Criticality defines whether a subsystem is critical or optional.
type Criticality int

const (
	Critical Criticality = iota
	Optional
)

func (c Criticality) String() string {
	if c == Critical {
		return "critical"
	}
	return "optional"
}

func (c Criticality) MarshalJSON() ([]byte, error) {
	return []byte(`"` + c.String() + `"`), nil
}

// Source indicates how the health status was determined.
type Source string

const (
	SourceProbe    Source = "probe"    // Active health check
	SourceCache    Source = "cache"    // Last-known-good cache
	SourceDerived  Source = "derived"  // Computed from other state
	SourceInferred Source = "inferred" // Inferred (e.g., DVR unknown when receiver unavailable)
)

// Subsystem identifies which system component is being reported on.
type Subsystem string

const (
	SubsystemReceiver Subsystem = "receiver"
	SubsystemDVR      Subsystem = "dvr"
	SubsystemEPG      Subsystem = "epg"
	SubsystemLibrary  Subsystem = "library"
	SubsystemPlayback Subsystem = "playback"
)

// SubsystemHealth represents the health state of a single subsystem.
// Per ADR-SRE-002 P0-B: Separates measured vs derived state.
type SubsystemHealth struct {
	Subsystem    Subsystem    `json:"subsystem"`
	Status       HealthStatus `json:"status"`
	MeasuredAt   time.Time    `json:"measured_at"`
	Source       Source       `json:"source"`
	Criticality  Criticality  `json:"criticality"`
	LastOK       *time.Time   `json:"last_ok,omitempty"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	Details      interface{}  `json:"details,omitempty"` // Subsystem-specific metadata
}

// DiagnosticsReport contains the overall system health status.
type DiagnosticsReport struct {
	MeasuredAt         time.Time                     `json:"measured_at"` // When subsystems were probed
	DerivedAt          time.Time                     `json:"derived_at"`  // When overall status was computed
	OverallStatus      HealthStatus                  `json:"overall_status"`
	Subsystems         map[Subsystem]SubsystemHealth `json:"subsystems"`
	DegradationSummary []DegradationItem             `json:"degradation_summary,omitempty"`
}

// DegradationItem provides actionable information about a degraded/unavailable subsystem.
type DegradationItem struct {
	Subsystem        Subsystem    `json:"subsystem"`
	Status           HealthStatus `json:"status"`
	Since            time.Time    `json:"since"`
	ErrorCode        string       `json:"error_code"`
	SuggestedActions []string     `json:"suggested_actions,omitempty"`
}

// ReceiverDetails contains receiver-specific health metadata.
type ReceiverDetails struct {
	ReceiverID     string `json:"receiver_id"`
	ResponseTimeMS int64  `json:"response_time_ms"`
	Version        string `json:"version,omitempty"`
}

// DVRDetails contains DVR-specific health metadata.
type DVRDetails struct {
	CachedRecordingCount int   `json:"cached_recording_count,omitempty"`
	CachedTimerCount     int   `json:"cached_timer_count,omitempty"`
	CacheAgeSeconds      int64 `json:"cache_age_seconds,omitempty"`
}

// EPGDetails contains EPG-specific health metadata.
type EPGDetails struct {
	EventCount   int       `json:"event_count,omitempty"`
	ServiceCount int       `json:"service_count,omitempty"`
	OldestEvent  time.Time `json:"oldest_event,omitempty"`
	NewestEvent  time.Time `json:"newest_event,omitempty"`
}

// LibraryRootHealth represents the health of a single library root.
type LibraryRootHealth struct {
	RootID         string       `json:"root_id"`
	Status         HealthStatus `json:"status"`
	Type           string       `json:"type"` // nfs|smb|local
	LastScanTime   time.Time    `json:"last_scan_time"`
	NextScanTime   *time.Time   `json:"next_scan_time,omitempty"`
	TotalItems     int          `json:"total_items"`
	ScanDurationMS int64        `json:"scan_duration_ms"`
	ErrorCount     int          `json:"error_count"`
	LastErrorCode  string       `json:"last_error_code,omitempty"`
}

// LibraryDetails contains library-specific health metadata.
type LibraryDetails struct {
	Roots         []LibraryRootHealth `json:"roots"`
	OverallStatus HealthStatus        `json:"overall_status"`
}

// PlaybackDetails contains playback-specific health metadata.
type PlaybackDetails struct {
	ActiveSessions   int    `json:"active_sessions"`
	MaxSessions      int    `json:"max_sessions"`
	UtilizationPct   int    `json:"utilization_pct"`
	FFmpegVersion    string `json:"ffmpeg_version,omitempty"`
	RecentFailures1h int    `json:"recent_failures_1h"`
}
