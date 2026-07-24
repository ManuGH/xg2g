// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package vod

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/delivery"
	"github.com/ManuGH/xg2g/internal/domain/delivery/cmaf"
)

// JITRemuxer handles on-the-fly packaging of copy-mode streams without disk materialization.
type JITRemuxer struct {
	packager delivery.Packager
}

// NewJITRemuxer constructs a JITRemuxer backed by a CMAF packager.
func NewJITRemuxer() *JITRemuxer {
	return &JITRemuxer{
		packager: cmaf.NewPackager(),
	}
}

// ServeInitHeader generates the initialization header segment for a copy-mode stream.
func (r *JITRemuxer) ServeInitHeader(ctx context.Context, trackID string) (*delivery.SegmentResponse, error) {
	if strings.TrimSpace(trackID) == "" {
		return nil, fmt.Errorf("trackID cannot be empty")
	}
	return r.packager.PackageInitSegment(ctx, trackID)
}

// ServeMediaFragment packages incoming raw stream data into a media fragment on the fly.
func (r *JITRemuxer) ServeMediaFragment(ctx context.Context, req delivery.SegmentRequest, rawStream io.Reader) (*delivery.SegmentResponse, error) {
	if rawStream == nil {
		return nil, fmt.Errorf("rawStream cannot be nil")
	}
	return r.packager.PackageMediaFragment(ctx, req, rawStream)
}
