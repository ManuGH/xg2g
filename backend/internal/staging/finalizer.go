// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package staging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
)

var (
	ErrNoSegmentsForAssembly  = errors.New("no valid segment files found for assembly")
	ErrCorruptSegmentFilename = errors.New("segment filename corrupted or failed validation")
)

// SequenceRange details a missing segment sequence interval.
type SequenceRange struct {
	StartSeq uint64 `json:"start_seq"`
	EndSeq   uint64 `json:"end_seq"`
}

// AssemblyReport details media invariants and gap statistics generated during segment finalization.
type AssemblyReport struct {
	SegmentCount    int             `json:"segment_count"`
	MissingRanges   []SequenceRange `json:"missing_ranges"`
	Discontinuities int             `json:"discontinuities"`
	CodecChanges    int             `json:"codec_changes"`
	TotalBytes      int64           `json:"total_bytes"`
	Complete        bool            `json:"complete"`
	FinalizedPath   string          `json:"finalized_path"`
}

// Finalizer interface abstracts media concatenation (.ts, .mp4 remuxing).
type Finalizer interface {
	Finalize(ctx context.Context, jobID string, sourceSegmentsDir string, targetFilePath string) (*AssemblyReport, error)
}

// TSFinalizer implements Finalizer for MPEG-TS streams.
type TSFinalizer struct{}

// NewTSFinalizer initializes a TSFinalizer.
func NewTSFinalizer() *TSFinalizer {
	return &TSFinalizer{}
}

// Finalize parses, orders, validates, and concatenates .ts segments into targetFilePath.
func (f *TSFinalizer) Finalize(ctx context.Context, jobID string, sourceSegmentsDir string, targetFilePath string) (*AssemblyReport, error) {
	entries, err := os.ReadDir(sourceSegmentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read segments dir %s: %w", sourceSegmentsDir, err)
	}

	type parsedSeg struct {
		path     string
		sequence uint64
	}

	var validSegs []parsedSeg
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "seg_") && strings.HasSuffix(name, ".ts") {
			var seq uint64
			if _, err := fmt.Sscanf(name, "seg_%d.ts", &seq); err != nil {
				return nil, fmt.Errorf("%w: '%s'", ErrCorruptSegmentFilename, name)
			}
			validSegs = append(validSegs, parsedSeg{
				path:     filepath.Join(sourceSegmentsDir, name),
				sequence: seq,
			})
		}
	}

	if len(validSegs) == 0 {
		return nil, ErrNoSegmentsForAssembly
	}

	// Sort segments strictly by sequence
	sort.Slice(validSegs, func(i, j int) bool {
		return validSegs[i].sequence < validSegs[j].sequence
	})

	// Detect missing sequence ranges (gaps)
	var missingRanges []SequenceRange
	for i := 1; i < len(validSegs); i++ {
		prevSeq := validSegs[i-1].sequence
		currSeq := validSegs[i].sequence
		if currSeq > prevSeq+1 {
			missingRanges = append(missingRanges, SequenceRange{
				StartSeq: prevSeq + 1,
				EndSeq:   currSeq - 1,
			})
		}
	}

	targetDir := filepath.Dir(targetFilePath)
	if err := os.MkdirAll(targetDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create target dir: %w", err)
	}

	tmpPath := targetFilePath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) //nolint:gosec // G304: target tmp path
	if err != nil {
		return nil, fmt.Errorf("failed to open output file: %w", err)
	}

	var totalBytes int64
	for _, seg := range validSegs {
		in, err := os.Open(seg.path)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to open segment %s: %w", seg.path, err)
		}
		n, err := io.Copy(out, in)
		_ = in.Close()
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to copy segment %s: %w", seg.path, err)
		}
		totalBytes += n
	}

	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to sync output file: %w", err)
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to close output file cleanly: %w", err)
	}

	if err := os.Rename(tmpPath, targetFilePath); err != nil {
		return nil, fmt.Errorf("failed to rename output file: %w", err)
	}

	// Parent directory fsync to guarantee directory entry persistence
	if pDir, err := os.Open(targetDir); err == nil { //nolint:gosec // G304: target dir path
		_ = pDir.Sync()
		_ = pDir.Close()
	}

	report := &AssemblyReport{
		SegmentCount:  len(validSegs),
		MissingRanges: missingRanges,
		TotalBytes:    totalBytes,
		Complete:      len(missingRanges) == 0,
		FinalizedPath: targetFilePath,
	}

	// Suppress unused import warning for ringbuffer if needed
	_ = ringbuffer.ParseSegmentFilename

	return report, nil
}
