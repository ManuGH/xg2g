// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package vod

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/delivery"
)

func TestJITRemuxer_ServeInitHeader(t *testing.T) {
	remuxer := NewJITRemuxer()

	t.Run("empty trackID returns error", func(t *testing.T) {
		resp, err := remuxer.ServeInitHeader(context.Background(), "")
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("valid trackID returns init header", func(t *testing.T) {
		resp, err := remuxer.ServeInitHeader(context.Background(), "v1")
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, delivery.SegmentTypeInit, resp.Type)
		assert.Equal(t, delivery.FormatCMAF, resp.Format)
		assert.Equal(t, "video/mp4", resp.ContentType)
		assert.True(t, len(resp.Data) > 0)
	})
}

func TestJITRemuxer_ServeMediaFragment(t *testing.T) {
	remuxer := NewJITRemuxer()

	t.Run("nil stream returns error", func(t *testing.T) {
		req := delivery.SegmentRequest{TrackID: "v1", Type: delivery.SegmentTypeMedia}
		resp, err := remuxer.ServeMediaFragment(context.Background(), req, nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("valid stream packages fragment on the fly", func(t *testing.T) {
		rawPayload := []byte("test_raw_payload_chunk")
		req := delivery.SegmentRequest{
			TrackID:     "v1",
			Type:        delivery.SegmentTypeMedia,
			SequenceNum: 42,
		}

		resp, err := remuxer.ServeMediaFragment(context.Background(), req, bytes.NewReader(rawPayload))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, delivery.SegmentTypeMedia, resp.Type)
		assert.Equal(t, delivery.FormatCMAF, resp.Format)
		assert.Equal(t, "video/iso.segment", resp.ContentType)
		assert.Equal(t, len(rawPayload)+8, len(resp.Data))
	})
}
