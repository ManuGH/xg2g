// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package hls

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)


// AccessPointType identifies the classification of the starting access point of a segment.
type AccessPointType string

const (
	AccessPointIDR           AccessPointType = "IDR"
	AccessPointRecoveryPoint AccessPointType = "RecoveryPoint"
	AccessPointNone          AccessPointType = "None"
)

// SegmentAccessPoint holds authoritative RAP inspection results for a segment.
type SegmentAccessPoint struct {
	Safe               bool            `json:"safe"`
	Type               AccessPointType `json:"type"`
	RecoveryFrameCount uint64          `json:"recovery_frame_count"`
	ExactMatch         bool            `json:"exact_match"`
	BrokenLink         bool            `json:"broken_link"`
	HasSPS             bool            `json:"has_sps"`
	HasPPS             bool            `json:"has_pps"`
}

type cacheEntry struct {
	sap     *SegmentAccessPoint
	modTime time.Time
	size    int64
}

// RAPCache is a session-bounded LRU/FIFO cache for segment RAP inspection results.
type RAPCache struct {
	MaxEntries int
	mu         sync.Mutex
	entries    map[string]*cacheEntry
	keys       []string
}

var (
	rapCachesMu sync.Mutex
	rapCaches   = make(map[string]*RAPCache)
)

func getOrCreateRAPCache(sessionKey string) *RAPCache {
	if sessionKey == "" {
		sessionKey = "global"
	}
	rapCachesMu.Lock()
	defer rapCachesMu.Unlock()
	c, ok := rapCaches[sessionKey]
	if !ok {
		c = &RAPCache{
			MaxEntries: 128,
			entries:    make(map[string]*cacheEntry),
		}
		rapCaches[sessionKey] = c
	}
	return c
}

// EvictRAPCache removes all cached inspection results for the given session key or directory upon session termination.
func EvictRAPCache(sessionKey string) {
	if sessionKey == "" {
		return
	}
	rapCachesMu.Lock()
	defer rapCachesMu.Unlock()
	delete(rapCaches, sessionKey)
}

func (c *RAPCache) load(path string, modTime time.Time, size int64) (*SegmentAccessPoint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	if !ok {
		return nil, false
	}
	if entry.modTime.Equal(modTime) && entry.size == size {
		return entry.sap, true
	}
	return nil, false
}

func (c *RAPCache) store(path string, sap *SegmentAccessPoint, modTime time.Time, size int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[path]; !exists {
		c.keys = append(c.keys, path)
	}
	c.entries[path] = &cacheEntry{sap: sap, modTime: modTime, size: size}
	for len(c.keys) > c.MaxEntries {
		oldest := c.keys[0]
		c.keys = c.keys[1:]
		delete(c.entries, oldest)
	}
}

// InspectSegmentRAP checks whether the specified segment starts with a clean decodable
// Random Access Point according to strict H.264 Annex-B / SEI recovery_point criteria.
// It caches inspection results using the default global cache key when no session key is passed.
func InspectSegmentRAP(filepath string) (*SegmentAccessPoint, error) {
	return InspectSegmentRAPWithSession(filepath, filepath)
}

// InspectSegmentRAPWithSession checks the specified segment using a bounded session-aware cache (MaxEntries=128).
func InspectSegmentRAPWithSession(filepath, sessionKey string) (*SegmentAccessPoint, error) {
	if strings.HasSuffix(filepath, ".tmp") {
		return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, nil
	}

	info, err := os.Stat(filepath)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 || info.IsDir() {
		return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, nil
	}

	cache := getOrCreateRAPCache(sessionKey)
	if sap, ok := cache.load(filepath, info.ModTime(), info.Size()); ok {
		return sap, nil
	}

	f, err := os.Open(filepath) // #nosec G304 -- filepath validated by caller
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	sap, inspectErr := inspectTSReader(f)
	if inspectErr != nil && !errors.Is(inspectErr, io.EOF) && !errors.Is(inspectErr, io.ErrUnexpectedEOF) {
		// Even if inspection encountered EOF after checking first VCL, if we got a valid classification, use it.
		if sap == nil {
			return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, nil
		}
	}
	if sap == nil {
		sap = &SegmentAccessPoint{Safe: false, Type: AccessPointNone}
	}

	cache.store(filepath, sap, info.ModTime(), info.Size())
	return sap, nil
}

// inspectTSReader demuxes MPEG-TS packets from r, extracts PAT/PMT to locate the video PID,
// extracts PES elementary bitstream payload, and parses Annex-B NAL units of the opening Access Unit.
func inspectTSReader(r io.Reader) (*SegmentAccessPoint, error) {
	br := bufio.NewReader(r)
	var (
		pmtPID   = -1
		videoPID = -1

		// Access unit accumulated state before first VCL
		hasSPS             bool
		hasPPS             bool
		hasRecoveryPoint   bool
		recoveryFrameCount uint64
		exactMatch         bool
		brokenLink         bool
		seenFirstVCL       bool

		pesAssembler bytes.Buffer
	)

	// Helper to process accumulated PES / elementary Annex-B buffer
	processBuffer := func(data []byte) (*SegmentAccessPoint, bool) {
		i := 0
		n := len(data)
		for i < n-3 {
			// Find Annex B start code: 0x00 0x00 0x01 or 0x00 0x00 0x00 0x01
			if data[i] == 0x00 && data[i+1] == 0x00 {
				startLen := 0
				if data[i+2] == 0x01 {
					startLen = 3
				} else if i+3 < n && data[i+2] == 0x00 && data[i+3] == 0x01 {
					startLen = 4
				}
				if startLen > 0 {
					nalStart := i + startLen
					// Find next start code or end of buffer
					j := nalStart
					for j < n-2 {
						if data[j] == 0x00 && data[j+1] == 0x00 && (data[j+2] == 0x01 || (j+3 < n && data[j+2] == 0x00 && data[j+3] == 0x01)) {
							break
						}
						j++
					}
					if nalStart < j {
						nalData := data[nalStart:j]
						if len(nalData) > 0 {
							nalType := nalData[0] & 0x1F
							switch nalType {
							case 7: // SPS
								hasSPS = true
							case 8: // PPS
								hasPPS = true
							case 6: // SEI
								rbsp := removeEmulationPreventionBytes(nalData[1:])
								parseSEIMessages(rbsp, &hasRecoveryPoint, &recoveryFrameCount, &exactMatch, &brokenLink)
							case 5: // IDR slice (VCL)
								seenFirstVCL = true
								safe := hasSPS && hasPPS
								sap := &SegmentAccessPoint{
									Safe:               safe,
									Type:               AccessPointIDR,
									RecoveryFrameCount: recoveryFrameCount,
									ExactMatch:         exactMatch,
									BrokenLink:         brokenLink,
									HasSPS:             hasSPS,
									HasPPS:             hasPPS,
								}
								return sap, true
							case 1, 2, 3, 4: // Non-IDR slices (VCL)
								seenFirstVCL = true
								safe := false
								apt := AccessPointNone
								if hasRecoveryPoint {
									apt = AccessPointRecoveryPoint
									if recoveryFrameCount == 0 && exactMatch && hasSPS && hasPPS {
										safe = true
									}
								}
								sap := &SegmentAccessPoint{
									Safe:               safe,
									Type:               apt,
									RecoveryFrameCount: recoveryFrameCount,
									ExactMatch:         exactMatch,
									BrokenLink:         brokenLink,
									HasSPS:             hasSPS,
									HasPPS:             hasPPS,
								}
								return sap, true
							case 9: // AUD (Access Unit Delimiter)
								// If we already saw a previous VCL and now hit a new AUD, we crossed the AU boundary
								if seenFirstVCL {
									sap := buildCurrentSAP(hasSPS, hasPPS, hasRecoveryPoint, recoveryFrameCount, exactMatch, brokenLink, AccessPointNone)
									return sap, true
								}
							}
						}
					}
					i = j
					continue
				}
			}
			i++
		}
		return nil, false
	}

	packet := make([]byte, 188)
	for {
		_, err := io.ReadFull(br, packet)
		if err != nil {
			if pesAssembler.Len() > 0 {
				if sap, done := processBuffer(pesAssembler.Bytes()); done {
					return sap, nil
				}
			}
			if seenFirstVCL {
				return buildCurrentSAP(hasSPS, hasPPS, hasRecoveryPoint, recoveryFrameCount, exactMatch, brokenLink, AccessPointNone), nil
			}
			return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, err
		}

		if packet[0] != 0x47 {
			// Resynchronize TS packet stream
			for {
				b, err := br.ReadByte()
				if err != nil {
					return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, err
				}
				if b == 0x47 {
					// Check if next 187 bytes can be read
					rest := make([]byte, 187)
					if _, err := io.ReadFull(br, rest); err != nil {
						return &SegmentAccessPoint{Safe: false, Type: AccessPointNone}, err
					}
					packet[0] = 0x47
					copy(packet[1:], rest)
					break
				}
			}
		}

		tei := (packet[1] & 0x80) != 0
		if tei {
			continue
		}
		pusi := (packet[1] & 0x40) != 0
		pid := (int(packet[1]&0x1F) << 8) | int(packet[2])
		afc := (packet[3] >> 4) & 0x03

		payloadOffset := 4
		if afc == 0 || afc == 2 {
			continue // No payload in this TS packet
		}
		if afc == 3 {
			afLen := int(packet[payloadOffset])
			payloadOffset += 1 + afLen
			if payloadOffset >= 188 {
				continue
			}
		}
		tsPayload := packet[payloadOffset:]

		// 1. Parse PAT on PID 0
		if pid == 0x0000 && len(tsPayload) > 1 {
			pointer := int(tsPayload[0])
			sectionOffset := 1 + pointer
			if sectionOffset+8 < len(tsPayload) {
				section := tsPayload[sectionOffset:]
				if section[0] == 0x00 { // PAT section
					sectionLen := (int(section[1]&0x0F) << 8) | int(section[2])
					if 8 < sectionLen+3 && sectionLen+3 <= len(section) {
						for p := 8; p+4 <= sectionLen+3; p += 4 {
							programNum := (int(section[p]) << 8) | int(section[p+1])
							if programNum != 0 {
								pmtPID = (int(section[p+2]&0x1F) << 8) | int(section[p+3])
								break
							}
						}
					}
				}
			}
			continue
		}

		// 2. Parse PMT on PMT PID
		if pid == pmtPID && len(tsPayload) > 1 {
			pointer := int(tsPayload[0])
			sectionOffset := 1 + pointer
			if sectionOffset+12 < len(tsPayload) {
				section := tsPayload[sectionOffset:]
				if section[0] == 0x02 { // PMT section
					sectionLen := (int(section[1]&0x0F) << 8) | int(section[2])
					programInfoLen := (int(section[10]&0x0F) << 8) | int(section[11])
					esPos := 12 + programInfoLen
					for esPos+5 <= sectionLen+3 && esPos+5 <= len(section) {
						streamType := section[esPos]
						elemPID := (int(section[esPos+1]&0x1F) << 8) | int(section[esPos+2])
						esInfoLen := (int(section[esPos+3]&0x0F) << 8) | int(section[esPos+4])
						if streamType == 0x1B || streamType == 0x24 { // H.264/AVC or H.265/HEVC
							videoPID = elemPID
							break
						}
						esPos += 5 + esInfoLen
					}
				}
			}
			continue
		}

		// Fallback detection if PMT wasn't found early: check if PID has PES start with stream_id 0xE0
		if videoPID == -1 && pusi && len(tsPayload) >= 6 {
			if tsPayload[0] == 0x00 && tsPayload[1] == 0x00 && tsPayload[2] == 0x01 {
				streamID := tsPayload[3]
				if streamID >= 0xE0 && streamID <= 0xEF {
					videoPID = pid
				}
			}
		}

		// 3. Collect Video PES bitstream on videoPID
		if pid == videoPID {
			if pusi {
				// If we already accumulated some PES data from previous packet, inspect it before resetting
				if pesAssembler.Len() > 0 {
					if sap, done := processBuffer(pesAssembler.Bytes()); done {
						return sap, nil
					}
					pesAssembler.Reset()
				}
				// Parse PES header to locate Annex-B start inside this TS packet
				if len(tsPayload) >= 9 && tsPayload[0] == 0x00 && tsPayload[1] == 0x00 && tsPayload[2] == 0x01 {
					pesHeaderLen := int(tsPayload[8])
					esOffset := 9 + pesHeaderLen
					if esOffset <= len(tsPayload) {
						pesAssembler.Write(tsPayload[esOffset:])
					}
				}
			} else {
				if pesAssembler.Len() > 0 {
					pesAssembler.Write(tsPayload)
					// Periodically process buffer to avoid reading through entire segment if we found VCL
					if pesAssembler.Len() >= 16384 {
						if sap, done := processBuffer(pesAssembler.Bytes()); done {
							return sap, nil
						}
					}
				}
			}
		}
	}
}

func buildCurrentSAP(hasSPS, hasPPS, hasRecoveryPoint bool, recFrameCount uint64, exactMatch, brokenLink bool, apt AccessPointType) *SegmentAccessPoint {
	if apt == AccessPointNone && hasRecoveryPoint {
		apt = AccessPointRecoveryPoint
	}
	safe := false
	if apt == AccessPointIDR && hasSPS && hasPPS {
		safe = true
	} else if apt == AccessPointRecoveryPoint && recFrameCount == 0 && exactMatch && hasSPS && hasPPS {
		safe = true
	}
	return &SegmentAccessPoint{
		Safe:               safe,
		Type:               apt,
		RecoveryFrameCount: recFrameCount,
		ExactMatch:         exactMatch,
		BrokenLink:         brokenLink,
		HasSPS:             hasSPS,
		HasPPS:             hasPPS,
	}
}

func removeEmulationPreventionBytes(src []byte) []byte {
	n := len(src)
	dst := make([]byte, 0, n)
	i := 0
	for i < n {
		if i+2 < n && src[i] == 0x00 && src[i+1] == 0x00 && src[i+2] == 0x03 {
			dst = append(dst, 0x00, 0x00)
			i += 3
			continue
		}
		dst = append(dst, src[i])
		i++
	}
	return dst
}

func parseSEIMessages(rbsp []byte, hasRecoveryPoint *bool, recoveryFrameCount *uint64, exactMatch, brokenLink *bool) {
	i := 0
	n := len(rbsp)
	for i < n {
		payloadType := 0
		for i < n && rbsp[i] == 0xFF {
			payloadType += 255
			i++
		}
		if i >= n {
			break
		}
		payloadType += int(rbsp[i])
		i++

		payloadSize := 0
		for i < n && rbsp[i] == 0xFF {
			payloadSize += 255
			i++
		}
		if i >= n {
			break
		}
		payloadSize += int(rbsp[i])
		i++

		if i+payloadSize > n {
			break
		}
		payloadData := rbsp[i : i+payloadSize]
		if payloadType == 6 && len(payloadData) > 0 { // recovery_point SEI
			*hasRecoveryPoint = true
			br := newBitReader(payloadData)
			cnt, ok := br.readUE()
			if ok {
				*recoveryFrameCount = cnt
			}
			if em, ok := br.readBit(); ok {
				*exactMatch = (em == 1)
			}
			if bl, ok := br.readBit(); ok {
				*brokenLink = (bl == 1)
			}
		}
		i += payloadSize
	}
}

type bitReader struct {
	data    []byte
	bytePos int
	bitPos  uint // 0 to 7, MSB to LSB
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data, bitPos: 0}
}

func (b *bitReader) readBit() (uint, bool) {
	if b.bytePos >= len(b.data) {
		return 0, false
	}
	bit := (uint(b.data[b.bytePos]) >> (7 - b.bitPos)) & 1
	b.bitPos++
	if b.bitPos == 8 {
		b.bitPos = 0
		b.bytePos++
	}
	return bit, true
}

func (b *bitReader) readUE() (uint64, bool) {
	leadingZeroBits := 0
	for {
		bit, ok := b.readBit()
		if !ok {
			return 0, false
		}
		if bit == 0 {
			leadingZeroBits++
			if leadingZeroBits > 63 {
				return 0, false
			}
		} else {
			break
		}
	}
	if leadingZeroBits == 0 {
		return 0, true
	}
	var value uint64
	for i := 0; i < leadingZeroBits; i++ {
		bit, ok := b.readBit()
		if !ok {
			return 0, false
		}
		value = (value << 1) | uint64(bit)
	}
	value += (1 << leadingZeroBits) - 1
	return value, true
}

// ErrNoSafeSegmentAvailable is returned when a live playlist contains only non-decodable/unsafe segments so far.
var ErrNoSafeSegmentAvailable = errors.New("no safe RAP segment available yet")

// FilterPlaylistRAP scans a restricted ORF-/MPEG-TS-Copy live M3U8 playlist and filters out any leading
// unsafe (.ts) segments before the first verified decodable Random Access Point (RAP).
// It adjusts EXT-X-MEDIA-SEQUENCE and EXT-X-DISCONTINUITY-SEQUENCE accordingly and preserves stateful
// header tags (#EXT-X-KEY, #EXT-X-MAP) seen prior to the first retained segment.
func FilterPlaylistRAP(playlistContent []byte, sessionDir string) ([]byte, int, error) {
	if len(playlistContent) == 0 || sessionDir == "" {
		return playlistContent, 0, nil
	}
	// If it's a master playlist or VOD, do not filter out segments.
	if bytes.Contains(playlistContent, []byte("#EXT-X-STREAM-INF:")) || bytes.Contains(playlistContent, []byte("#EXT-X-PLAYLIST-TYPE:VOD")) || bytes.Contains(playlistContent, []byte("#EXT-X-ENDLIST")) {
		return playlistContent, 0, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(playlistContent))
	type segmentEntry struct {
		tags []string
		uri  string
	}

	var headerLines []string
	var segments []segmentEntry
	var currentTags []string
	var mediaSeq uint64
	var hasMediaSeq bool
	var discontinuitySeq uint64
	var hasDiscontinuitySeq bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			hasMediaSeq = true
			valStr := strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")
			if val, err := strconv.ParseUint(valStr, 10, 64); err == nil {
				mediaSeq = val
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:") {
			hasDiscontinuitySeq = true
			valStr := strings.TrimPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:")
			if val, err := strconv.ParseUint(valStr, 10, 64); err == nil {
				discontinuitySeq = val
			}
			continue
		}
		// If line starts with #EXTINF or other segment-scoped tags, accumulate into current segment tags
		if strings.HasPrefix(line, "#EXTINF:") || strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") || line == "#EXT-X-DISCONTINUITY" || strings.HasPrefix(line, "#EXT-X-BYTERANGE:") || strings.HasPrefix(line, "#EXT-X-GAP") || strings.HasPrefix(line, "#EXT-X-KEY:") || strings.HasPrefix(line, "#EXT-X-MAP:") || strings.HasPrefix(line, "#EXT-X-PART:") {
			currentTags = append(currentTags, line)
			continue
		}
		if strings.HasPrefix(line, "#") {
			if len(segments) == 0 && len(currentTags) == 0 {
				headerLines = append(headerLines, line)
			} else {
				currentTags = append(currentTags, line)
			}
			continue
		}

		segments = append(segments, segmentEntry{
			tags: currentTags,
			uri:  line,
		})
		currentTags = nil
	}
	if err := scanner.Err(); err != nil {
		return playlistContent, 0, err
	}

	if len(segments) == 0 {
		return playlistContent, 0, nil
	}

	for _, seg := range segments {
		cleanURI := seg.uri
		if idx := strings.IndexAny(cleanURI, "?#"); idx != -1 {
			cleanURI = cleanURI[:idx]
		}
		if !strings.HasSuffix(cleanURI, ".ts") {
			return playlistContent, 0, nil
		}
	}

	firstSafeIdx := -1
	for i, seg := range segments {
		cleanURI := seg.uri
		if idx := strings.IndexAny(cleanURI, "?#"); idx != -1 {
			cleanURI = cleanURI[:idx]
		}
		clean := filepath.Clean(cleanURI)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, 0, fmt.Errorf("invalid segment URI path traversal attempt: %s", seg.uri)
		}
		segPath := filepath.Join(sessionDir, clean)
		rel, err := filepath.Rel(sessionDir, segPath)
		if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "/") {
			return nil, 0, fmt.Errorf("segment URI escapes session directory: %s", seg.uri)
		}

		sap, err := InspectSegmentRAPWithSession(segPath, sessionDir)
		if err == nil && sap != nil && sap.Safe {
			firstSafeIdx = i
			break
		}
	}

	if firstSafeIdx <= 0 {
		if firstSafeIdx == 0 {
			return playlistContent, 0, nil
		}
		return nil, len(segments), ErrNoSafeSegmentAvailable
	}

	droppedCount := firstSafeIdx
	newMediaSeq := mediaSeq + uint64(droppedCount)

	// Preserve stateful tags (#EXT-X-KEY, #EXT-X-MAP) from dropped leading segments
	// and track dropped discontinuities to update #EXT-X-DISCONTINUITY-SEQUENCE.
	var preservedStatefulTags []string
	var droppedDiscontinuities uint64
	for i := 0; i < firstSafeIdx; i++ {
		for _, t := range segments[i].tags {
			if strings.HasPrefix(t, "#EXT-X-KEY:") || strings.HasPrefix(t, "#EXT-X-MAP:") {
				preservedStatefulTags = append(preservedStatefulTags, t)
			}
			if t == "#EXT-X-DISCONTINUITY" {
				droppedDiscontinuities++
			}
		}
	}

	var buf bytes.Buffer
	for _, h := range headerLines {
		buf.WriteString(h)
		buf.WriteByte('\n')
	}
	if hasDiscontinuitySeq || droppedDiscontinuities > 0 {
		buf.WriteString(fmt.Sprintf("#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", discontinuitySeq+droppedDiscontinuities))
	}
	if hasMediaSeq || droppedCount > 0 {
		buf.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", newMediaSeq))
	}
	for _, t := range preservedStatefulTags {
		buf.WriteString(t)
		buf.WriteByte('\n')
	}
	for i := firstSafeIdx; i < len(segments); i++ {
		for _, t := range segments[i].tags {
			buf.WriteString(t)
			buf.WriteByte('\n')
		}
		buf.WriteString(segments[i].uri)
		buf.WriteByte('\n')
	}
	for _, t := range currentTags {
		buf.WriteString(t)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), droppedCount, nil
}

// BatchRAPReport reports statistics across consecutive segment boundaries in a session.
type BatchRAPReport struct {
	TotalSegments     int  `json:"totalSegments"`
	SafeSegments      int  `json:"safeSegments"`
	FirstSafeIndex    int  `json:"firstSafeIndex"`
	UnsafeAfterFirst  int  `json:"unsafeAfterFirst"`
	AllSafeAfterFirst bool `json:"allSafeAfterFirst"`
}

// VerifyBatchSegmentRAPs checks up to max consecutive files in sessionDir and verifies
// whether boundaries after the first safe RAP are also safe.
func VerifyBatchSegmentRAPs(sessionDir string, segFiles []string) (*BatchRAPReport, error) {
	report := &BatchRAPReport{FirstSafeIndex: -1}
	for i, f := range segFiles {
		clean := filepath.Clean(f)
		segPath := filepath.Join(sessionDir, clean)
		sap, err := InspectSegmentRAPWithSession(segPath, sessionDir)
		if err != nil {
			return nil, err
		}
		report.TotalSegments++
		isSafe := sap != nil && sap.Safe
		if isSafe {
			report.SafeSegments++
			if report.FirstSafeIndex == -1 {
				report.FirstSafeIndex = i
			}
		} else if report.FirstSafeIndex != -1 {
			report.UnsafeAfterFirst++
		}
	}
	report.AllSafeAfterFirst = report.FirstSafeIndex != -1 && report.UnsafeAfterFirst == 0
	return report, nil
}


