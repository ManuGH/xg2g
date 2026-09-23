// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import "github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"

// MediaCommit bundles transport stream bytes with their authoritative interpreted facts
// for atomic commit into the MasterRing stream coordinator.
//
// Invariant: "Kein Byte ohne seine Wahrheit und keine Wahrheit ohne die zugehörigen Bytes."
// Either the metadata updates (timeline.MediaIndex and attachIndex) and the byte payload
// (packetStore) are committed simultaneously into the published stream state under MasterRing.mu,
// or the transaction fails closed with zero byte and zero index mutation.
type MediaCommit struct {
	// StartOffset is the expected absolute monotonic byte offset where this chunk begins.
	StartOffset int64

	// Data is the raw packet-aligned transport stream chunk to be written to packetStore.
	Data []byte

	// Result is the complete, canonical parse result returned by mediafacts.Core.
	Result mediafacts.ParseResult
}
