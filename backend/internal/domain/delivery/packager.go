// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package delivery

import (
	"context"
	"io"
)

// PackagingFormat identifies media fragment container formatting.
type PackagingFormat string

const (
	FormatCMAF   PackagingFormat = "cmaf"
	FormatFMP4   PackagingFormat = "fmp4"
	FormatMPEGTS PackagingFormat = "ts"
)

// SegmentType distinguishes initialization headers from media fragments.
type SegmentType string

const (
	SegmentTypeInit  SegmentType = "init"
	SegmentTypeMedia SegmentType = "media"
)

// SegmentRequest encapsulates a request for a packaged fragment.
type SegmentRequest struct {
	TrackID      string
	Type         SegmentType
	SequenceNum  uint64
	StartTimeSec float64
	DurationSec  float64
}

// SegmentResponse represents the packaged output payload.
type SegmentResponse struct {
	Type        SegmentType
	Format      PackagingFormat
	ContentType string
	Data        []byte
}

// Packager defines the contract for fragmenting media streams into delivery formats.
type Packager interface {
	// Format returns the packaging format handled by this packager.
	Format() PackagingFormat

	// PackageInitSegment generates the initialization header (e.g. init.mp4).
	PackageInitSegment(ctx context.Context, trackID string) (*SegmentResponse, error)

	// PackageMediaFragment packages a media payload into a fragment (e.g. .m4s segment).
	PackageMediaFragment(ctx context.Context, req SegmentRequest, rawStream io.Reader) (*SegmentResponse, error)
}
