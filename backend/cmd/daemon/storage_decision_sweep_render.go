package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

func renderStorageDecisionSweepTable(w io.Writer, result storageDecisionSweep) {
	_, _ = fmt.Fprintf(w, "Generated: %s\n", result.GeneratedAt.Format(time.RFC3339))
	if strings.TrimSpace(result.ConfigPath) != "" {
		_, _ = fmt.Fprintf(w, "Config:    %s\n", result.ConfigPath)
	}
	_, _ = fmt.Fprintf(w, "DataDir:   %s\n", result.DataDir)
	_, _ = fmt.Fprintf(w, "Playlist:  %s\n", result.Playlist)
	if result.SkipScan {
		_, _ = fmt.Fprintln(w, "ScanMode:  skip-scan")
	}
	if strings.TrimSpace(result.StatePath) != "" {
		_, _ = fmt.Fprintf(w, "State:     %s\n", result.StatePath)
	}
	if result.Bouquet != "" {
		_, _ = fmt.Fprintf(w, "Bouquet:   %s\n", result.Bouquet)
	}
	_, _ = fmt.Fprintf(w, "Clients:   %s\n", strings.Join(result.ClientFamilies, ","))
	_, _ = fmt.Fprintf(w, "Summary:   selected=%d truth_complete=%d truth_incomplete=%d truth_missing=%d truth_event_inactive=%d fallback=%d unresolved=%d services_with_decision=%d decision_rows=%d decision_errors=%d\n",
		result.Summary.ServicesSelected,
		result.Summary.TruthComplete,
		result.Summary.TruthIncomplete,
		result.Summary.TruthMissing,
		result.Summary.TruthEventInactive,
		result.Summary.TruthSourceFallback,
		result.Summary.TruthSourceUnresolved,
		result.Summary.ServicesWithDecision,
		result.Summary.DecisionRows,
		result.Summary.DecisionErrors,
	)
	if result.Diff != nil {
		switch {
		case !result.Diff.BaselineFound:
			_, _ = fmt.Fprintln(w, "Diff:      first run (no baseline)")
		case result.Diff.ScopeChanged:
			_, _ = fmt.Fprintln(w, "Diff:      baseline reset (scope changed)")
		default:
			coverageRegression := "no"
			if result.Diff.Coverage != nil && result.Diff.Coverage.Regression {
				coverageRegression = "yes"
			}
			_, _ = fmt.Fprintf(w, "Diff:      relevant=%d mode_changes=%d truth_changes=%d coverage_regression=%s\n",
				result.Diff.RelevantChanges,
				len(result.Diff.ModeChanges),
				len(result.Diff.TruthChanges),
				coverageRegression,
			)
		}
	}
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintln(w, "Scanned Services:")
	scanWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(scanWriter, "SERVICE_REF\tCHANNEL\tBOUQUET\tTRUTH_STATUS\tTRUTH_SOURCE\tSCAN_STATE\tTRUTH\tFAILURE")
	for _, row := range result.ScannedServices {
		truth := emptyDash(strings.Trim(strings.Join([]string{row.Container, row.VideoCodec, row.AudioCodec}, "/"), "/"))
		_, _ = fmt.Fprintf(scanWriter, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.Bouquet,
			row.TruthStatus,
			row.TruthSource,
			emptyDash(row.ScanState),
			truth,
			emptyDash(row.FailureReason),
		)
	}
	_ = scanWriter.Flush()

	if len(result.Decisions) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Decisions:")
	decisionWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(decisionWriter, "SERVICE_REF\tCHANNEL\tCLIENT\tCAPS_SOURCE\tTRUTH_SOURCE\tEFFECTIVE_INTENT\tMODE\tTARGET_PROFILE\tREASONS\tERROR")
	for _, row := range result.Decisions {
		_, _ = fmt.Fprintf(decisionWriter, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.ClientFamily,
			emptyDash(row.ClientCapsSource),
			row.TruthSource,
			emptyDash(row.EffectiveIntent),
			emptyDash(row.Mode),
			emptyDash(row.TargetProfileSummary),
			emptyDash(strings.Join(row.Reasons, ",")),
			emptyDash(row.Error),
		)
	}
	_ = decisionWriter.Flush()

	if result.Diff == nil || !result.Diff.BaselineFound || result.Diff.ScopeChanged {
		return
	}
	if len(result.Diff.ModeChanges) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Mode Changes:")
		modeWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(modeWriter, "CHANNEL\tCLIENT\tFROM\tTO")
		for _, row := range result.Diff.ModeChanges {
			_, _ = fmt.Fprintf(modeWriter, "%s\t%s\t%s\t%s\n", row.ChannelName, row.ClientFamily, row.FromMode, row.ToMode)
		}
		_ = modeWriter.Flush()
	}
	if len(result.Diff.TruthChanges) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Truth Changes:")
		truthWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(truthWriter, "CHANNEL\tFROM\tTO\tSTATUS")
		for _, row := range result.Diff.TruthChanges {
			_, _ = fmt.Fprintf(truthWriter, "%s\t%s\t%s\t%s -> %s\n",
				row.ChannelName,
				row.FromTruth,
				row.ToTruth,
				emptyDash(row.FromTruthStatus),
				emptyDash(row.ToTruthStatus),
			)
		}
		_ = truthWriter.Flush()
	}
	if result.Diff.Coverage != nil && result.Diff.Coverage.Regression {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Coverage Regression: fallback %d -> %d, unresolved %d -> %d\n",
			result.Diff.Coverage.FallbackBefore,
			result.Diff.Coverage.FallbackAfter,
			result.Diff.Coverage.UnresolvedBefore,
			result.Diff.Coverage.UnresolvedAfter,
		)
		if len(result.Diff.Coverage.NewFallback) > 0 {
			_, _ = fmt.Fprintf(w, "New fallback: %s\n", storageDecisionSweepServiceNames(result.Diff.Coverage.NewFallback))
		}
		if len(result.Diff.Coverage.NewUnresolved) > 0 {
			_, _ = fmt.Fprintf(w, "New unresolved: %s\n", storageDecisionSweepServiceNames(result.Diff.Coverage.NewUnresolved))
		}
	}
}
