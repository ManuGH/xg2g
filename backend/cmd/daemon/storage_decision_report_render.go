package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func renderStorageDecisionReportTable(w io.Writer, report storageDecisionReport) {
	_, _ = fmt.Fprintf(w, "Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintf(w, "DataDir:   %s\n", report.DataDir)
	_, _ = fmt.Fprintf(w, "Playlist:  %s\n", report.Playlist)
	if report.Bouquet != "" {
		_, _ = fmt.Fprintf(w, "Bouquet:   %s\n", report.Bouquet)
	}
	if report.Filters.ClientFamily != "" || report.Filters.Intent != "" || report.Filters.Origin != "" {
		_, _ = fmt.Fprintf(w, "Filters:   client_family=%s intent=%s origin=%s subject_kind=%s\n",
			emptyDash(report.Filters.ClientFamily),
			emptyDash(report.Filters.Intent),
			emptyDash(report.Filters.Origin),
			report.Filters.SubjectKind,
		)
	}
	_, _ = fmt.Fprintf(w, "Summary:   services=%d rows=%d with_decision=%d without_decision=%d truth_complete=%d truth_incomplete=%d truth_missing=%d truth_event_inactive=%d host_fingerprints=%d basis_host_pairs=%d multi_host_basis=%d unknown_host_rows=%d\n",
		report.Summary.ServicesTotal,
		report.Summary.RowsTotal,
		report.Summary.ServicesWithDecision,
		report.Summary.ServicesWithoutDecision,
		report.Summary.TruthComplete,
		report.Summary.TruthIncomplete,
		report.Summary.TruthMissing,
		report.Summary.TruthEventInactive,
		report.Summary.DistinctHostFingerprints,
		report.Summary.DistinctBasisHostPairs,
		report.Summary.BasisHashesWithMultiHost,
		report.Summary.UnknownHostRows,
	)
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(w, "Warning:   %s\n", warning)
	}
	_, _ = fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SERVICE_REF\tCHANNEL\tBOUQUET\tTRUTH_SOURCE\tTRUTH_STATUS\tORIGIN\tCLIENT\tCAPS_SOURCE\tHOST_FINGERPRINT\tREQUESTED_INTENT\tEFFECTIVE_INTENT\tMODE\tTARGET_PROFILE\tREASONS\tCHANGED_AT")
	for _, row := range report.Rows {
		changedAt := ""
		if row.ChangedAt != nil {
			changedAt = row.ChangedAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.Bouquet,
			row.TruthSource,
			row.TruthStatus,
			emptyDash(row.DecisionOrigin),
			emptyDash(row.ClientFamily),
			emptyDash(row.ClientCapsSource),
			emptyDash(row.HostFingerprint),
			emptyDash(row.RequestedIntent),
			emptyDash(row.EffectiveIntent),
			emptyDash(row.Mode),
			emptyDash(row.TargetProfileSummary),
			emptyDash(strings.Join(row.Reasons, ",")),
			emptyDash(changedAt),
		)
	}
	_ = tw.Flush()
}

func presentHostFingerprint(value string) string {
	if strings.TrimSpace(value) == "" {
		return reportUnknownHost
	}
	return value
}

func normalizeDecisionReportOrigin(value string) string {
	switch normalize.Token(value) {
	case "":
		return ""
	case decisionaudit.OriginRuntime:
		return decisionaudit.OriginRuntime
	case decisionaudit.OriginSweep:
		return decisionaudit.OriginSweep
	default:
		return normalize.Token(value)
	}
}

func summarizeTargetProfile(targetProfile *playbackprofile.TargetPlaybackProfile) string {
	if targetProfile == nil {
		return ""
	}
	videoCodec := strings.TrimSpace(targetProfile.Video.Codec)
	audioCodec := strings.TrimSpace(targetProfile.Audio.Codec)
	switch {
	case targetProfile.Container != "" && videoCodec != "" && audioCodec != "":
		return fmt.Sprintf("%s/%s/%s", targetProfile.Container, videoCodec, audioCodec)
	case targetProfile.Container != "" && videoCodec != "":
		return fmt.Sprintf("%s/%s", targetProfile.Container, videoCodec)
	case targetProfile.Container != "":
		return targetProfile.Container
	default:
		return videoCodec
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
