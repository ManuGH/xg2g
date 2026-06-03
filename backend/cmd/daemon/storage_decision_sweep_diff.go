package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func resolveStorageDecisionSweepConfigPath(explicit string, dataDir string) string {
	if path := strings.TrimSpace(explicit); path != "" {
		return path
	}
	if dir := strings.TrimSpace(dataDir); dir != "" {
		candidate := filepath.Join(dir, "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return resolveDefaultConfigPath()
}

func resolveStorageDecisionSweepStatePath(dataDir string, explicit string) string {
	if path := strings.TrimSpace(explicit); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(resolveStorageDBPath(dataDir, "decision_audit.sqlite")), "last_sweep.json")
}

func computeStorageDecisionSweepScopeKey(opts storageDecisionSweepOptions, playlistName string, clientFamilies []string) string {
	channelFilter := append([]string(nil), splitCSVString(opts.ChannelNamesCSV)...)
	serviceRefFilter := append([]string(nil), splitCSVString(opts.ServiceRefsCSV)...)
	sort.Strings(channelFilter)
	sort.Strings(serviceRefFilter)
	clients := append([]string(nil), clientFamilies...)
	sort.Strings(clients)
	return strings.Join([]string{
		"playlist=" + strings.TrimSpace(playlistName),
		"bouquet=" + strings.TrimSpace(opts.Bouquet),
		"channels=" + strings.Join(channelFilter, ","),
		"service_refs=" + strings.Join(serviceRefFilter, ","),
		"requested_profile=" + strings.TrimSpace(opts.RequestedProfile),
		"api_version=" + strings.TrimSpace(opts.APIVersion),
		"schema_type=" + strings.TrimSpace(opts.SchemaType),
		"skip_scan=" + fmt.Sprintf("%t", opts.SkipScan),
		"limit=" + fmt.Sprintf("%d", opts.Limit),
		"clients=" + strings.Join(clients, ","),
	}, "|")
}

func loadStorageDecisionSweepState(path string) (*storageDecisionSweep, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	// #nosec G304 -- state path is resolved from controlled dataDir/CLI input and file reads are the purpose of this command.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var snapshot storageDecisionSweep
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func persistStorageDecisionSweepState(path string, result storageDecisionSweep) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	snapshot := result
	snapshot.StatePath = ""
	snapshot.Diff = nil

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func diffStorageDecisionSweep(previous *storageDecisionSweep, current storageDecisionSweep, statePath string) *storageDecisionSweepDiff {
	diff := &storageDecisionSweepDiff{
		StatePath: statePath,
	}
	if previous == nil {
		return diff
	}
	diff.BaselineFound = true
	if !previous.GeneratedAt.IsZero() {
		ts := previous.GeneratedAt.UTC()
		diff.BaselineGeneratedAt = &ts
	}
	if strings.TrimSpace(previous.ScopeKey) == "" || previous.ScopeKey != current.ScopeKey {
		diff.ScopeChanged = true
		return diff
	}

	diff.ModeChanges = collectStorageDecisionSweepModeChanges(*previous, current)
	diff.TruthChanges = collectStorageDecisionSweepTruthChanges(*previous, current)
	diff.Coverage = collectStorageDecisionSweepCoverageChange(*previous, current)
	diff.RelevantChanges = len(diff.ModeChanges) + len(diff.TruthChanges)
	if diff.Coverage != nil && diff.Coverage.Regression {
		diff.RelevantChanges++
	}
	if len(diff.ModeChanges) == 0 {
		diff.ModeChanges = nil
	}
	if len(diff.TruthChanges) == 0 {
		diff.TruthChanges = nil
	}
	if diff.Coverage != nil && !diff.Coverage.Regression && len(diff.Coverage.NewFallback) == 0 && len(diff.Coverage.NewUnresolved) == 0 {
		diff.Coverage = nil
	}
	return diff
}

func collectStorageDecisionSweepModeChanges(previous storageDecisionSweep, current storageDecisionSweep) []storageDecisionSweepModeChange {
	type decisionKey struct {
		ServiceRef   string
		ClientFamily string
	}
	previousRows := make(map[decisionKey]storageDecisionSweepDecision)
	for _, row := range previous.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			continue
		}
		previousRows[decisionKey{ServiceRef: row.ServiceRef, ClientFamily: row.ClientFamily}] = row
	}

	out := make([]storageDecisionSweepModeChange, 0)
	for _, row := range current.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			continue
		}
		prev, ok := previousRows[decisionKey{ServiceRef: row.ServiceRef, ClientFamily: row.ClientFamily}]
		if !ok {
			continue
		}
		fromMode := storageDecisionSweepModeCode(prev)
		toMode := storageDecisionSweepModeCode(row)
		if !isRelevantStorageDecisionSweepModeShift(fromMode, toMode) {
			continue
		}
		out = append(out, storageDecisionSweepModeChange{
			ServiceRef:   row.ServiceRef,
			ChannelName:  firstNonEmptySweep(row.ChannelName, prev.ChannelName),
			ClientFamily: row.ClientFamily,
			FromMode:     presentDecisionMode(fromMode),
			ToMode:       presentDecisionMode(toMode),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ChannelName != out[j].ChannelName {
			return out[i].ChannelName < out[j].ChannelName
		}
		return out[i].ClientFamily < out[j].ClientFamily
	})
	return out
}

func collectStorageDecisionSweepTruthChanges(previous storageDecisionSweep, current storageDecisionSweep) []storageDecisionSweepTruthChange {
	previousRows := make(map[string]storageDecisionSweepScanRow, len(previous.ScannedServices))
	for _, row := range previous.ScannedServices {
		previousRows[row.ServiceRef] = row
	}

	out := make([]storageDecisionSweepTruthChange, 0)
	for _, row := range current.ScannedServices {
		prev, ok := previousRows[row.ServiceRef]
		if !ok {
			continue
		}
		if storageDecisionSweepTruthFingerprint(prev) == storageDecisionSweepTruthFingerprint(row) {
			continue
		}
		out = append(out, storageDecisionSweepTruthChange{
			ServiceRef:      row.ServiceRef,
			ChannelName:     firstNonEmptySweep(row.ChannelName, prev.ChannelName),
			FromTruth:       storageDecisionSweepTruthDisplay(prev),
			ToTruth:         storageDecisionSweepTruthDisplay(row),
			FromTruthStatus: prev.TruthStatus,
			ToTruthStatus:   row.TruthStatus,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ChannelName < out[j].ChannelName
	})
	return out
}

func collectStorageDecisionSweepCoverageChange(previous storageDecisionSweep, current storageDecisionSweep) *storageDecisionSweepCoverageChange {
	change := &storageDecisionSweepCoverageChange{
		FallbackBefore:   previous.Summary.TruthSourceFallback,
		FallbackAfter:    current.Summary.TruthSourceFallback,
		FallbackDelta:    current.Summary.TruthSourceFallback - previous.Summary.TruthSourceFallback,
		UnresolvedBefore: previous.Summary.TruthSourceUnresolved,
		UnresolvedAfter:  current.Summary.TruthSourceUnresolved,
		UnresolvedDelta:  current.Summary.TruthSourceUnresolved - previous.Summary.TruthSourceUnresolved,
	}
	previousFallback := storageDecisionSweepServicesByTruthSource(previous.ScannedServices, reportTruthSourceFallback)
	currentFallback := storageDecisionSweepServicesByTruthSource(current.ScannedServices, reportTruthSourceFallback)
	previousUnresolved := storageDecisionSweepServicesByTruthSource(previous.ScannedServices, reportTruthSourceUnresolved)
	currentUnresolved := storageDecisionSweepServicesByTruthSource(current.ScannedServices, reportTruthSourceUnresolved)
	change.NewFallback = storageDecisionSweepNewServices(previousFallback, currentFallback)
	change.NewUnresolved = storageDecisionSweepNewServices(previousUnresolved, currentUnresolved)
	change.Regression = change.FallbackDelta > 0 || change.UnresolvedDelta > 0
	return change
}

func storageDecisionSweepServicesByTruthSource(rows []storageDecisionSweepScanRow, truthSource string) map[string]storageDecisionSweepServiceNote {
	out := make(map[string]storageDecisionSweepServiceNote)
	for _, row := range rows {
		if row.TruthSource != truthSource {
			continue
		}
		out[row.ServiceRef] = storageDecisionSweepServiceNote{
			ServiceRef:  row.ServiceRef,
			ChannelName: row.ChannelName,
		}
	}
	return out
}

func storageDecisionSweepNewServices(previous map[string]storageDecisionSweepServiceNote, current map[string]storageDecisionSweepServiceNote) []storageDecisionSweepServiceNote {
	out := make([]storageDecisionSweepServiceNote, 0)
	for serviceRef, row := range current {
		if _, ok := previous[serviceRef]; ok {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ChannelName < out[j].ChannelName
	})
	return out
}

func storageDecisionSweepModeCode(row storageDecisionSweepDecision) string {
	if code := normalize.Token(row.ModeCode); code != "" {
		return code
	}
	switch normalize.Token(row.Mode) {
	case "remux":
		return string(decisionaudit.ModeDirectStream)
	default:
		return normalize.Token(row.Mode)
	}
}

func isRelevantStorageDecisionSweepModeShift(fromMode string, toMode string) bool {
	return fromMode != toMode &&
		((fromMode == string(decisionaudit.ModeDirectPlay) || fromMode == string(decisionaudit.ModeTranscode)) &&
			(toMode == string(decisionaudit.ModeDirectPlay) || toMode == string(decisionaudit.ModeTranscode)))
}

func storageDecisionSweepTruthFingerprint(row storageDecisionSweepScanRow) string {
	return strings.Join([]string{
		row.TruthStatus,
		normalize.Token(row.Container),
		normalize.Token(row.VideoCodec),
		normalize.Token(row.AudioCodec),
	}, "|")
}

func storageDecisionSweepTruthDisplay(row storageDecisionSweepScanRow) string {
	parts := []string{row.Container, row.VideoCodec, row.AudioCodec}
	return emptyDash(strings.Trim(strings.Join(parts, "/"), "/"))
}

func firstNonEmptySweep(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
