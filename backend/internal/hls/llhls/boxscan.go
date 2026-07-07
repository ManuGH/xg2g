// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

// Package llhls implements the server-side packaging layer for Low-Latency
// HLS: it indexes the CMAF fragments FFmpeg writes into growing fMP4
// segments (moof/mdat pairs produced by frag_duration) and exposes them as
// EXT-X-PART byte ranges with a blocking-reload playlist.
package llhls

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Fragment is one CMAF chunk (optional sidx boxes + moof + mdat) inside an
// fMP4 segment file, addressable as an EXT-X-PART byte range.
type Fragment struct {
	// Offset is the byte offset of the fragment's first box (a sidx when
	// present, else the moof).
	Offset int64
	// Size is the byte length through the end of the fragment's mdat.
	Size int64
	// Independent reports whether the fragment starts with a sync sample
	// (from the trun/tfhd sample flags), i.e. is decodable on its own.
	Independent bool
}

type box struct {
	offset  int64
	size    int64
	boxType string
}

// errTruncatedBox signals that the file ends inside a box — expected on a
// growing segment; the scanner simply stops before the incomplete box.
var errTruncatedBox = fmt.Errorf("truncated box")

func readBoxHeader(r io.ReaderAt, offset, fileSize int64) (box, error) {
	if offset < 0 || fileSize-offset < 8 {
		return box{}, errTruncatedBox
	}
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], offset); err != nil {
		return box{}, err
	}
	size := int64(binary.BigEndian.Uint32(hdr[:4]))
	typ := string(hdr[4:8])
	if size == 1 {
		if fileSize-offset < 16 {
			return box{}, errTruncatedBox
		}
		var ext [8]byte
		if _, err := r.ReadAt(ext[:], offset+8); err != nil {
			return box{}, err
		}
		extSize := binary.BigEndian.Uint64(ext[:])
		if extSize > uint64(1<<63-1) {
			return box{}, fmt.Errorf("extended box size %d overflows int64 at offset %d", extSize, offset)
		}
		size = int64(extSize)
	}
	if size < 8 {
		return box{}, fmt.Errorf("invalid box size %d at offset %d", size, offset)
	}
	if size > fileSize-offset {
		return box{}, errTruncatedBox
	}
	return box{offset: offset, size: size, boxType: typ}, nil
}

// ScanFragments walks the top-level boxes of an fMP4 segment starting at
// `from` and returns every complete fragment plus the offset scanning should
// resume from on the next call (i.e. after the last complete fragment).
// Incomplete trailing data on a growing file is left for the next scan.
func ScanFragments(r io.ReaderAt, fileSize, from int64) ([]Fragment, int64, error) {
	var frags []Fragment
	next := from
	fragStart := int64(-1)
	var moofOffset int64 = -1

	for off := from; off < fileSize; {
		b, err := readBoxHeader(r, off, fileSize)
		if err == errTruncatedBox {
			break
		}
		if err != nil {
			return frags, next, err
		}
		switch b.boxType {
		case "sidx", "styp", "prft":
			if fragStart < 0 {
				fragStart = b.offset
			}
		case "moof":
			if fragStart < 0 {
				fragStart = b.offset
			}
			moofOffset = b.offset
		case "mdat":
			if moofOffset >= 0 {
				independent, ierr := fragmentIsIndependent(r, moofOffset, fileSize)
				if ierr != nil {
					independent = false
				}
				frags = append(frags, Fragment{
					Offset:      fragStart,
					Size:        b.offset + b.size - fragStart,
					Independent: independent,
				})
				next = b.offset + b.size
			}
			fragStart = -1
			moofOffset = -1
		default:
			// ftyp/moov/free/... reset any partial fragment grouping.
			fragStart = -1
			moofOffset = -1
			next = b.offset + b.size
		}
		off = b.offset + b.size
	}
	return frags, next, nil
}

const (
	// ISO 14496-12 sample flags: bit 16 of the 32-bit flags word is
	// sample_is_non_sync_sample. A cleared bit means sync sample.
	sampleIsNonSyncSampleMask = 0x00010000

	trunFirstSampleFlagsPresent = 0x000004
	trunSampleFlagsPresent      = 0x000400
	trunDataOffsetPresent       = 0x000001
	tfhdDefaultSampleFlags      = 0x000020
	tfhdBaseDataOffsetPresent   = 0x000001
	tfhdSampleDescriptionIndex  = 0x000002
	tfhdDefaultSampleDuration   = 0x000008
	tfhdDefaultSampleSize       = 0x000010
)

// fragmentIsIndependent inspects the first video traf's sample flags to
// decide whether the fragment starts on a sync sample (IDR). We look at the
// first traf's trun: first-sample-flags, else per-sample flags, else the
// tfhd default flags.
func fragmentIsIndependent(r io.ReaderAt, moofOffset, fileSize int64) (bool, error) {
	moof, err := readBoxHeader(r, moofOffset, fileSize)
	if err != nil {
		return false, err
	}
	end := moof.offset + moof.size
	for off := moof.offset + 8; off < end; {
		b, err := readBoxHeader(r, off, end)
		if err != nil {
			return false, err
		}
		if b.boxType == "traf" {
			ok, found, err := trafStartsWithSyncSample(r, b)
			if err != nil {
				return false, err
			}
			if found {
				return ok, nil
			}
		}
		off = b.offset + b.size
	}
	return false, nil
}

func trafStartsWithSyncSample(r io.ReaderAt, traf box) (independent, found bool, err error) {
	var tfhdDefaults uint32
	var haveTfhdDefaults bool

	end := traf.offset + traf.size
	for off := traf.offset + 8; off < end; {
		b, err := readBoxHeader(r, off, end)
		if err != nil {
			return false, false, err
		}
		switch b.boxType {
		case "tfhd":
			if b.size < 12 {
				return false, false, fmt.Errorf("tfhd box too small at offset %d", b.offset)
			}
			flagsWord, err := readFullBoxFlags(r, b)
			if err != nil {
				return false, false, err
			}
			pos := b.offset + 12 + 4 // fullbox header + track_id
			boxEnd := b.offset + b.size
			if flagsWord&tfhdBaseDataOffsetPresent != 0 {
				pos += 8
			}
			if flagsWord&tfhdSampleDescriptionIndex != 0 {
				pos += 4
			}
			if flagsWord&tfhdDefaultSampleDuration != 0 {
				pos += 4
			}
			if flagsWord&tfhdDefaultSampleSize != 0 {
				pos += 4
			}
			if flagsWord&tfhdDefaultSampleFlags != 0 {
				if pos+4 > boxEnd {
					return false, false, fmt.Errorf("tfhd box truncated")
				}
				v, err := readUint32At(r, pos)
				if err != nil {
					return false, false, err
				}
				tfhdDefaults = v
				haveTfhdDefaults = true
			}
		case "trun":
			if b.size < 16 {
				return false, false, fmt.Errorf("trun box too small at offset %d", b.offset)
			}
			flagsWord, err := readFullBoxFlags(r, b)
			if err != nil {
				return false, false, err
			}
			pos := b.offset + 12 + 4 // fullbox header + sample_count
			boxEnd := b.offset + b.size
			if flagsWord&trunDataOffsetPresent != 0 {
				pos += 4
			}
			if flagsWord&trunFirstSampleFlagsPresent != 0 {
				if pos+4 > boxEnd {
					return false, false, fmt.Errorf("trun box truncated")
				}
				v, err := readUint32At(r, pos)
				if err != nil {
					return false, false, err
				}
				return v&sampleIsNonSyncSampleMask == 0, true, nil
			}
			if flagsWord&trunSampleFlagsPresent != 0 {
				// Per-sample flags: the first sample's flags follow optional
				// duration/size fields whose presence we cannot know without
				// the full layout; fall through to tfhd defaults instead of
				// guessing offsets.
				break
			}
			if haveTfhdDefaults {
				return tfhdDefaults&sampleIsNonSyncSampleMask == 0, true, nil
			}
		}
		off = b.offset + b.size
	}
	return false, false, nil
}

func readFullBoxFlags(r io.ReaderAt, b box) (uint32, error) {
	if b.size < 12 {
		return 0, fmt.Errorf("fullbox too small: size=%d at offset %d", b.size, b.offset)
	}
	v, err := readUint32At(r, b.offset+8)
	if err != nil {
		return 0, err
	}
	return v & 0x00FFFFFF, nil
}

func readUint32At(r io.ReaderAt, offset int64) (uint32, error) {
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], offset); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}

// RepairLeakedInit fixes FFmpeg 8.x behavior where, with frag_duration set,
// the first segment's fragments are written into the init file: init.mp4
// ends up as ftyp+moov followed by the first segment's sidx/moof/mdat
// chain while seg 0 on disk contains only a bare styp. The repair moves the
// leaked chain into the first segment file (after its styp) and truncates
// the init to ftyp+moov. Both writes go through temp files + rename so a
// concurrently reading client never sees a half-repaired artifact.
// It returns true when a repair was performed.
func RepairLeakedInit(initPath, firstSegmentPath string) (bool, error) {
	initData, err := os.ReadFile(initPath) // #nosec G304 -- session-confined artifact path
	if err != nil {
		return false, err
	}

	// Find the end of the pure init part (ftyp+moov...) = offset of the
	// first box that belongs to a fragment chain.
	var leakStart int64 = -1
	size := int64(len(initData))
	rd := readerAt(initData)
	for off := int64(0); off < size; {
		b, err := readBoxHeader(rd, off, size)
		if err != nil {
			break
		}
		if b.boxType == "sidx" || b.boxType == "moof" || b.boxType == "styp" || b.boxType == "prft" {
			leakStart = b.offset
			break
		}
		off = b.offset + b.size
	}
	if leakStart < 0 {
		return false, nil // init is clean
	}

	segData, err := os.ReadFile(firstSegmentPath) // #nosec G304 -- session-confined artifact path
	if err != nil {
		return false, err
	}

	repairedSeg := make([]byte, 0, len(segData)+len(initData)-int(leakStart))
	repairedSeg = append(repairedSeg, segData...)
	repairedSeg = append(repairedSeg, initData[leakStart:]...)

	if err := atomicWrite(firstSegmentPath, repairedSeg); err != nil {
		return false, err
	}
	if err := atomicWrite(initPath, initData[:leakStart]); err != nil {
		return false, err
	}
	return true, nil
}

type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".repair.tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:gosec // G703: tmp path is os.CreateTemp result, not user-controlled
		return err
	}
	return os.Rename(tmp, path)
}
