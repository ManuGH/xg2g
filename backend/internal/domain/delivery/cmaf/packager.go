// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package cmaf

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/delivery"
)

// Packager implements delivery.Packager for CMAF / fMP4 fragments.
type Packager struct{}

// NewPackager creates a new CMAF/fMP4 packager.
func NewPackager() *Packager {
	return &Packager{}
}

// Format returns FormatCMAF.
func (p *Packager) Format() delivery.PackagingFormat {
	return delivery.FormatCMAF
}

// PackageInitSegment generates the fMP4/CMAF initialization header.
func (p *Packager) PackageInitSegment(ctx context.Context, trackID string) (*delivery.SegmentResponse, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, fmt.Errorf("trackID cannot be empty")
	}

	// Basic CMAF ftyp + moov setup for stream header initialization
	ftyp := []byte{
		0x00, 0x00, 0x00, 0x18, // size: 24
		'f', 't', 'y', 'p',
		'c', 'm', 'f', 'c', // major brand: cmfc
		0x00, 0x00, 0x00, 0x00, // minor version: 0
		'c', 'm', 'f', 'c', // compatible brands
		'i', 's', 'o', '6',
	}

	return &delivery.SegmentResponse{
		Type:        delivery.SegmentTypeInit,
		Format:      delivery.FormatCMAF,
		ContentType: "video/mp4",
		Data:        ftyp,
	}, nil
}

// PackageMediaFragment packages media payload into a CMAF/fMP4 fragment (.m4s).
func (p *Packager) PackageMediaFragment(ctx context.Context, req delivery.SegmentRequest, rawStream io.Reader) (*delivery.SegmentResponse, error) {
	if rawStream == nil {
		return nil, fmt.Errorf("rawStream cannot be nil")
	}

	payload, err := io.ReadAll(rawStream)
	if err != nil {
		return nil, fmt.Errorf("read raw payload: %w", err)
	}

	// Wrap payload in minimal stms/mdat fragment container if unboxed
	var data []byte
	if len(payload) >= 8 && string(payload[4:8]) == "moof" {
		data = payload
	} else {
		// Box payload into mdat box
		mdatHeader := make([]byte, 8)
		// #nosec G115 -- segment payload is an in-memory fragment well within uint32 capacity
		binary.BigEndian.PutUint32(mdatHeader[:4], uint32(len(payload)+8))
		copy(mdatHeader[4:8], "mdat")
		data = append(mdatHeader, payload...)
	}

	return &delivery.SegmentResponse{
		Type:        delivery.SegmentTypeMedia,
		Format:      delivery.FormatCMAF,
		ContentType: "video/iso.segment",
		Data:        data,
	}, nil
}
