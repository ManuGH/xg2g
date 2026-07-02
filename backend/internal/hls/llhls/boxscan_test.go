package llhls

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func mkBox(boxType string, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(buf[:4], uint32(8+len(payload)))
	copy(buf[4:8], boxType)
	copy(buf[8:], payload)
	return buf
}

func mkFullBox(boxType string, versionFlags uint32, payload []byte) []byte {
	body := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(body[:4], versionFlags)
	copy(body[4:], payload)
	return mkBox(boxType, body)
}

// mkMoof builds a moof containing one traf with a trun using
// first-sample-flags to signal (non-)sync.
func mkMoof(sync bool) []byte {
	var firstFlags uint32
	if !sync {
		firstFlags = sampleIsNonSyncSampleMask
	}
	trunPayload := make([]byte, 8) // sample_count + first_sample_flags
	binary.BigEndian.PutUint32(trunPayload[:4], 1)
	binary.BigEndian.PutUint32(trunPayload[4:8], firstFlags)
	trun := mkFullBox("trun", trunFirstSampleFlagsPresent, trunPayload)

	tfhdPayload := make([]byte, 4) // track_id
	binary.BigEndian.PutUint32(tfhdPayload, 1)
	tfhd := mkFullBox("tfhd", 0, tfhdPayload)

	traf := mkBox("traf", append(tfhd, trun...))
	mfhd := mkFullBox("mfhd", 0, []byte{0, 0, 0, 1})
	return mkBox("moof", append(mfhd, traf...))
}

func mkFragment(sync bool, mdatLen int) []byte {
	sidx := mkFullBox("sidx", 0, make([]byte, 24))
	moof := mkMoof(sync)
	mdat := mkBox("mdat", bytes.Repeat([]byte{0xAB}, mdatLen))
	out := append([]byte{}, sidx...)
	out = append(out, moof...)
	return append(out, mdat...)
}

func TestScanFragmentsCompleteSegment(t *testing.T) {
	styp := mkBox("styp", []byte("msdhmsdh12345678")[:16])
	f1 := mkFragment(true, 100)
	f2 := mkFragment(false, 50)
	f3 := mkFragment(true, 75)
	data := append(append(append(append([]byte{}, styp...), f1...), f2...), f3...)

	frags, next, err := ScanFragments(readerAt(data), int64(len(data)), 0)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(frags) != 3 {
		t.Fatalf("expected 3 fragments, got %d: %+v", len(frags), frags)
	}
	if next != int64(len(data)) {
		t.Fatalf("next = %d, want %d", next, len(data))
	}
	// The segment prologue (styp) attaches to the first fragment so the
	// first EXT-X-PART byte range starts at offset 0 and a range-fetching
	// client receives a self-contained chunk.
	if frags[0].Offset != 0 {
		t.Errorf("frag0 offset = %d, want 0", frags[0].Offset)
	}
	if frags[0].Size != int64(len(styp)+len(f1)) {
		t.Errorf("frag0 size = %d, want %d", frags[0].Size, len(styp)+len(f1))
	}
	if !frags[0].Independent || frags[1].Independent || !frags[2].Independent {
		t.Errorf("independence flags wrong: %+v", frags)
	}
	// Fragments must tile contiguously.
	if frags[1].Offset != frags[0].Offset+frags[0].Size {
		t.Errorf("frag1 not contiguous: %+v", frags)
	}
}

func TestScanFragmentsGrowingFile(t *testing.T) {
	styp := mkBox("styp", make([]byte, 16))
	f1 := mkFragment(true, 100)
	f2 := mkFragment(true, 80)
	full := append(append(append([]byte{}, styp...), f1...), f2...)

	// Cut inside f2's mdat: only f1 is complete.
	cut := len(styp) + len(f1) + len(f2) - 10
	frags, next, err := ScanFragments(readerAt(full[:cut]), int64(cut), 0)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(frags) != 1 {
		t.Fatalf("expected 1 complete fragment, got %d", len(frags))
	}

	// Resume from `next` once the file has grown: f2 appears.
	frags2, next2, err := ScanFragments(readerAt(full), int64(len(full)), next)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if len(frags2) != 1 {
		t.Fatalf("expected 1 new fragment after growth, got %d", len(frags2))
	}
	if frags2[0].Offset != int64(len(styp)+len(f1)) {
		t.Errorf("resumed fragment offset wrong: %+v", frags2[0])
	}
	if next2 != int64(len(full)) {
		t.Errorf("next2 = %d, want %d", next2, len(full))
	}
}

func TestRepairLeakedInit(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.mp4")
	segPath := filepath.Join(dir, "seg_000000.m4s")

	ftyp := mkBox("ftyp", []byte("iso5....next....")[:16])
	moov := mkBox("moov", make([]byte, 32))
	leak := mkFragment(true, 60)
	styp := mkBox("styp", make([]byte, 16))

	leakedInit := append(append(append([]byte{}, ftyp...), moov...), leak...)
	if err := os.WriteFile(initPath, leakedInit, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(segPath, styp, 0o644); err != nil {
		t.Fatal(err)
	}

	repaired, err := RepairLeakedInit(initPath, segPath)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !repaired {
		t.Fatal("expected repair to run")
	}

	gotInit, _ := os.ReadFile(initPath)
	if !bytes.Equal(gotInit, append(append([]byte{}, ftyp...), moov...)) {
		t.Errorf("init not truncated to ftyp+moov (len=%d)", len(gotInit))
	}
	gotSeg, _ := os.ReadFile(segPath)
	if !bytes.Equal(gotSeg, append(append([]byte{}, styp...), leak...)) {
		t.Errorf("segment did not receive leaked fragments (len=%d)", len(gotSeg))
	}

	// The repaired segment must scan as one valid fragment after the styp.
	frags, _, err := ScanFragments(readerAt(gotSeg), int64(len(gotSeg)), 0)
	if err != nil || len(frags) != 1 || !frags[0].Independent {
		t.Fatalf("repaired segment does not scan: frags=%v err=%v", frags, err)
	}

	// Idempotence: a clean init must not be repaired again.
	repaired2, err := RepairLeakedInit(initPath, segPath)
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	if repaired2 {
		t.Fatal("repair must be idempotent")
	}
}
