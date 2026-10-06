// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package edge_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func setupTestResolver(t *testing.T) (*edge.Resolver, *sourceref.Registry, *sourceref.Parser, *int64) {
	t.Helper()
	parser, err := sourceref.NewParser(testKey)
	require.NoError(t, err)

	reg := sourceref.NewRegistry()
	var ingressCount int64

	resolver := edge.NewResolver(reg, parser, func(endpoint string) {
		atomic.AddInt64(&ingressCount, 1)
	})

	return resolver, reg, parser, &ingressCount
}

func TestResolver_Matrix(t *testing.T) {
	resolver, reg, parser, ingressCount := setupTestResolver(t)

	// Seed one valid IPTV source into the registry
	rawCanaryRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid%3a8080/live/token/stream.ts:Canary Channel"
	source, err := parser.Parse(rawCanaryRef)
	require.NoError(t, err)
	err = reg.Replace([]sourceref.Source{source})
	require.NoError(t, err)
	validOpaqueID := source.ID().String()

	tests := []struct {
		name          string
		endpoint      string
		ref           string
		wantRaw       string
		wantKind      edge.Kind
		wantErr       error
		expectCountIn int64
	}{
		{
			name:          "DVB ZDF HD pass-through",
			endpoint:      "intents",
			ref:           "1:0:19:2B66:3F3:1:C00000:0:0:0:",
			wantRaw:       "1:0:19:2B66:3F3:1:C00000:0:0:0:",
			wantKind:      edge.KindPassThrough,
			wantErr:       nil,
			expectCountIn: 0,
		},
		{
			name:          "DVB Radio pass-through",
			endpoint:      "intents",
			ref:           "1:0:2:1234:567:1:C00000:0:0:0:",
			wantRaw:       "1:0:2:1234:567:1:C00000:0:0:0:",
			wantKind:      edge.KindPassThrough,
			wantErr:       nil,
			expectCountIn: 0,
		},
		{
			name:          "Empty string pass-through",
			endpoint:      "intents",
			ref:           "",
			wantRaw:       "",
			wantKind:      edge.KindPassThrough,
			wantErr:       nil,
			expectCountIn: 0,
		},
		{
			name:          "Raw 4097 IPTV legacy ingress",
			endpoint:      "intents",
			ref:           "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts:News",
			wantRaw:       "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts:News",
			wantKind:      edge.KindLegacyRaw,
			wantErr:       nil,
			expectCountIn: 1,
		},
		{
			name:          "Raw 5001 Exteplayer legacy ingress",
			endpoint:      "playback_info",
			ref:           "5001:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/exte.ts:Exte",
			wantRaw:       "5001:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/exte.ts:Exte",
			wantKind:      edge.KindLegacyRaw,
			wantErr:       nil,
			expectCountIn: 1,
		},
		{
			name:          "Raw 5002 GstPlayer legacy ingress",
			endpoint:      "now_next",
			ref:           "5002:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/gst.ts:Gst",
			wantRaw:       "5002:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/gst.ts:Gst",
			wantKind:      edge.KindLegacyRaw,
			wantErr:       nil,
			expectCountIn: 1,
		},
		{
			name:          "Raw 4097 with leading and trailing whitespace",
			endpoint:      "timers",
			ref:           "   4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts:News   ",
			wantRaw:       "   4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts:News   ",
			wantKind:      edge.KindLegacyRaw,
			wantErr:       nil,
			expectCountIn: 1,
		},
		{
			name:          "Valid registered opaque ID",
			endpoint:      "intents",
			ref:           validOpaqueID,
			wantRaw:       rawCanaryRef,
			wantKind:      edge.KindOpaque,
			wantErr:       nil,
			expectCountIn: 0,
		},
		{
			name:          "Valid registered opaque ID with whitespace",
			endpoint:      "stream_live",
			ref:           "  " + validOpaqueID + " \n ",
			wantRaw:       rawCanaryRef,
			wantKind:      edge.KindOpaque,
			wantErr:       nil,
			expectCountIn: 0,
		},
		{
			name:          "Valid format opaque ID but NOT in registry",
			endpoint:      "intents",
			ref:           "iptv_abcdefghijklmnopqrstuvwx23", // exactly 26 base32 chars
			wantRaw:       "",
			wantKind:      0,
			wantErr:       edge.ErrNotFound,
			expectCountIn: 0,
		},
		{
			name:          "Invalid opaque ID - too short",
			endpoint:      "intents",
			ref:           "iptv_short",
			wantRaw:       "",
			wantKind:      0,
			wantErr:       edge.ErrInvalidID,
			expectCountIn: 0,
		},
		{
			name:          "Invalid opaque ID - bad characters (0, 1, 8, 9 not in base32)",
			endpoint:      "intents",
			ref:           "iptv_00000000000000000000000000",
			wantRaw:       "",
			wantKind:      0,
			wantErr:       edge.ErrInvalidID,
			expectCountIn: 0,
		},
		{
			name:          "Invalid opaque ID - uppercase chars",
			endpoint:      "intents",
			ref:           "iptv_ABCDEFGHIJKLMNOPQRSTUVWX23",
			wantRaw:       "",
			wantKind:      0,
			wantErr:       edge.ErrInvalidID,
			expectCountIn: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeCount := atomic.LoadInt64(ingressCount)
			raw, kind, err := resolver.ResolveInbound(tt.endpoint, tt.ref)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, "", raw)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantRaw, raw)
				assert.Equal(t, tt.wantKind, kind)
			}

			afterCount := atomic.LoadInt64(ingressCount)
			assert.Equal(t, beforeCount+tt.expectCountIn, afterCount)
		})
	}
}

func TestResolver_DisabledMode(t *testing.T) {
	// 1. Nil *edge.Resolver
	var nilResolver *edge.Resolver

	raw, kind, err := nilResolver.ResolveInbound("intents", "1:0:19:2B66:3F3:1:C00000:0:0:0:")
	require.NoError(t, err)
	assert.Equal(t, "1:0:19:2B66:3F3:1:C00000:0:0:0:", raw)
	assert.Equal(t, edge.KindPassThrough, kind)

	raw, kind, err = nilResolver.ResolveInbound("intents", "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts")
	require.NoError(t, err)
	assert.Equal(t, "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts", raw)
	assert.Equal(t, edge.KindLegacyRaw, kind)

	// Valid format opaque ID under nil resolver returns ErrNotFound
	raw, kind, err = nilResolver.ResolveInbound("intents", "iptv_abcdefghijklmnopqrstuvwx23")
	require.ErrorIs(t, err, edge.ErrNotFound)
	assert.Equal(t, "", raw)

	// Invalid format returns ErrInvalidID even under nil resolver
	raw, kind, err = nilResolver.ResolveInbound("intents", "iptv_invalid_format")
	require.ErrorIs(t, err, edge.ErrInvalidID)
	assert.Equal(t, "", raw)

	// 2. Resolver with nil registry/parser
	unwiredResolver := edge.NewResolver(nil, nil, nil)
	raw, kind, err = unwiredResolver.ResolveInbound("intents", "iptv_abcdefghijklmnopqrstuvwx23")
	require.ErrorIs(t, err, edge.ErrNotFound)
	assert.Equal(t, "", raw)
}

func TestResolver_NoInputEchoInErrors(t *testing.T) {
	resolver, _, _, _ := setupTestResolver(t)

	sensitiveCanary := "SECRET_TOKEN_XYZ_9876543210"
	invalidInput := fmt.Sprintf("iptv_%s_bad_chars!", sensitiveCanary)

	_, _, err := resolver.ResolveInbound("intents", invalidInput)
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), sensitiveCanary), "error must never echo the input token")
	assert.False(t, strings.Contains(err.Error(), "iptv_"), "error must not echo prefix or partial input")

	// Unknown valid ID
	validUnknownID := "iptv_abcdefghijklmnopqrstuvwx23"
	_, _, err = resolver.ResolveInbound("intents", validUnknownID)
	require.ErrorIs(t, err, edge.ErrNotFound)
	assert.False(t, strings.Contains(err.Error(), validUnknownID), "ErrNotFound must never echo the input ID")
}

func TestResolver_RawRefsNeverGrowRegistry(t *testing.T) {
	resolver, reg, _, ingressCount := setupTestResolver(t)
	require.Equal(t, 0, reg.Len())

	rawRefs := []string{
		"4097:0:1:0:0:0:0:0:0:0:http%3a//first.invalid/stream.ts:Channel 1",
		"4097:0:1:0:0:0:0:0:0:0:http%3a//second.invalid/stream.ts:Channel 2",
		"5001:0:1:0:0:0:0:0:0:0:http%3a//third.invalid/stream.ts:Channel 3",
		"5002:0:1:0:0:0:0:0:0:0:http%3a//fourth.invalid/stream.ts:Channel 4",
	}

	for _, ref := range rawRefs {
		raw, kind, err := resolver.ResolveInbound("intents", ref)
		require.NoError(t, err)
		assert.Equal(t, ref, raw)
		assert.Equal(t, edge.KindLegacyRaw, kind)
	}

	assert.Equal(t, int64(len(rawRefs)), atomic.LoadInt64(ingressCount))
	// Invariant: Registry length MUST remain 0! Client raw refs never grow the registry!
	assert.Equal(t, 0, reg.Len(), "resolving raw refs must never populate or grow the registry")
}

func TestResolver_ConcurrentResolveAndReplace(t *testing.T) {
	resolver, reg, parser, _ := setupTestResolver(t)

	// Pre-seed 50 sources
	sources := make([]sourceref.Source, 50)
	ids := make([]string, 50)
	for i := 0; i < 50; i++ {
		ref := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:http%%3a//canary%d.invalid/stream.ts:Canary %d", i, i)
		src, err := parser.Parse(ref)
		require.NoError(t, err)
		sources[i] = src
		ids[i] = src.ID().String()
	}
	require.NoError(t, reg.Replace(sources))

	var readersWg sync.WaitGroup
	var writersWg sync.WaitGroup
	stop := make(chan struct{})

	// 10 Reader goroutines constantly calling ResolveInbound
	for r := 0; r < 10; r++ {
		readersWg.Add(1)
		go func(readerID int) {
			defer readersWg.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					id := ids[i%len(ids)]
					raw, kind, err := resolver.ResolveInbound("intents", id)
					if err == nil {
						if kind != edge.KindOpaque || raw == "" {
							t.Errorf("reader %d saw inconsistent state", readerID)
						}
					} else if !errors.Is(err, edge.ErrNotFound) {
						t.Errorf("reader %d unexpected error: %v", readerID, err)
					}
					i++
				}
			}
		}(r)
	}

	// 2 Writer goroutines constantly swapping snapshot
	for w := 0; w < 2; w++ {
		writersWg.Add(1)
		go func(writerID int) {
			defer writersWg.Done()
			for iter := 0; iter < 100; iter++ {
				// Alternate between 50 items and 25 items
				var subset []sourceref.Source
				if iter%2 == 0 {
					subset = sources
				} else {
					subset = sources[:25]
				}
				if err := reg.Replace(subset); err != nil {
					t.Errorf("writer %d replace failed: %v", writerID, err)
				}
			}
		}(w)
	}

	// Wait for writers to finish their swaps, then signal readers to stop
	writersWg.Wait()
	close(stop)
	readersWg.Wait()
}

func TestResolver_EdgeTranslator(t *testing.T) {
	resolver, reg, _, _ := setupTestResolver(t)

	rawCanaryRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live/token/stream.ts:Canary Channel"
	dvbRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"

	t.Run("DVB preservation invariant", func(t *testing.T) {
		assert.Equal(t, dvbRef, resolver.MaskServiceRef(dvbRef))
		ptr := &dvbRef
		assert.Equal(t, dvbRef, *resolver.MaskServiceRefPtr(ptr))
		assert.Equal(t, "/logos/1_0_19_283D.png", resolver.MaskLogoURL("/logos/1_0_19_283D.png", dvbRef))
		assert.Equal(t, "", resolver.MaskPiconURL(dvbRef))
	})

	t.Run("IPTV masking and mask-on-emit dynamic registration", func(t *testing.T) {
		// Registry initially empty
		assert.Equal(t, 0, reg.Len())

		maskedID := resolver.MaskServiceRef(rawCanaryRef)
		assert.True(t, strings.HasPrefix(maskedID, "iptv_"))
		assert.Equal(t, 1, reg.Len(), "MaskServiceRef must dynamically register IPTV source")

		// Subsequent ResolveOpaqueID immediately succeeds
		resolvedRaw, ok := resolver.ResolveOpaqueID(maskedID)
		assert.True(t, ok)
		assert.Equal(t, rawCanaryRef, resolvedRaw)

		// Calling MaskServiceRef on already masked ID returns it as-is
		assert.Equal(t, maskedID, resolver.MaskServiceRef(maskedID))

		// MaskServiceRefPtr
		rawPtr := &rawCanaryRef
		maskedPtr := resolver.MaskServiceRefPtr(rawPtr)
		require.NotNil(t, maskedPtr)
		assert.Equal(t, maskedID, *maskedPtr)

		// MaskLogoURL and MaskPiconURL
		logo := resolver.MaskLogoURL("/logos/raw_canary.png?v=123", rawCanaryRef)
		assert.Equal(t, fmt.Sprintf("/logos/%s.png?v=123", maskedID), logo)

		picon := resolver.MaskPiconURL(rawCanaryRef)
		assert.Equal(t, fmt.Sprintf("/logos/%s.png", maskedID), picon)
	})

	t.Run("Fail closed on malformed IPTV reference", func(t *testing.T) {
		badIPTV := "4097:0:1:0:0:0:0:0:0:0:not-a-valid-url"
		assert.Equal(t, "", resolver.MaskServiceRef(badIPTV))
		assert.Nil(t, resolver.MaskServiceRefPtr(&badIPTV))
		assert.Equal(t, "", resolver.MaskLogoURL("/logos/bad.png", badIPTV))
	})

	t.Run("Disabled or nil resolver fails closed for IPTV and preserves DVB", func(t *testing.T) {
		var nilResolver *edge.Resolver
		assert.Equal(t, dvbRef, nilResolver.MaskServiceRef(dvbRef))
		assert.Equal(t, dvbRef, *nilResolver.MaskServiceRefPtr(&dvbRef))
		assert.Equal(t, "/logos/dvb.png", nilResolver.MaskLogoURL("/logos/dvb.png", dvbRef))

		assert.Equal(t, "", nilResolver.MaskServiceRef(rawCanaryRef))
		assert.Nil(t, nilResolver.MaskServiceRefPtr(&rawCanaryRef))
		assert.Equal(t, "", nilResolver.MaskLogoURL("/logos/raw.png", rawCanaryRef))

		raw, ok := nilResolver.ResolveOpaqueID("iptv_abcdefghijklmnopqrstuvwxyz")
		assert.False(t, ok)
		assert.Empty(t, raw)
	})
}
