// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package cmaf

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/delivery"
)

func TestPackager_Format(t *testing.T) {
	p := NewPackager()
	assert.Equal(t, delivery.FormatCMAF, p.Format())
}

func TestPackager_PackageInitSegment(t *testing.T) {
	p := NewPackager()

	t.Run("empty trackID returns error", func(t *testing.T) {
		resp, err := p.PackageInitSegment(context.Background(), "")
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("valid trackID returns init segment", func(t *testing.T) {
		resp, err := p.PackageInitSegment(context.Background(), "v1")
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, delivery.SegmentTypeInit, resp.Type)
		assert.Equal(t, delivery.FormatCMAF, resp.Format)
		assert.Equal(t, "video/mp4", resp.ContentType)
		assert.True(t, len(resp.Data) >= 24)
		assert.Equal(t, "ftyp", string(resp.Data[4:8]))
	})
}

func TestPackager_PackageMediaFragment(t *testing.T) {
	p := NewPackager()

	t.Run("nil stream returns error", func(t *testing.T) {
		req := delivery.SegmentRequest{TrackID: "v1", Type: delivery.SegmentTypeMedia}
		resp, err := p.PackageMediaFragment(context.Background(), req, nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("valid raw payload wraps into mdat container", func(t *testing.T) {
		raw := []byte("raw_sample_data")
		req := delivery.SegmentRequest{
			TrackID:     "v1",
			Type:        delivery.SegmentTypeMedia,
			SequenceNum: 1,
		}

		resp, err := p.PackageMediaFragment(context.Background(), req, bytes.NewReader(raw))
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, delivery.SegmentTypeMedia, resp.Type)
		assert.Equal(t, delivery.FormatCMAF, resp.Format)
		assert.Equal(t, "video/iso.segment", resp.ContentType)
		assert.Equal(t, len(raw)+8, len(resp.Data))
		assert.Equal(t, "mdat", string(resp.Data[4:8]))
	})
}
