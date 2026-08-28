// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"testing"
)

func FuzzMasterRingPush(f *testing.F) {
	f.Add(make([]byte, TSPacketSize))
	f.Add([]byte{0x47, 0x01, 0x00, 0x10})
	f.Add(append([]byte{0x47, 0x41, 0x00, 0x30, 0xFF}, make([]byte, TSPacketSize-5)...))

	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewMasterRing(64 * TSPacketSize)
		defer r.Close()
		_, _ = r.Push(context.Background(), data)
		_, _ = r.LatestKeyframeOffset()
		_ = r.RandomAccess()
		_, _ = r.VideoDetails()
	})
}
