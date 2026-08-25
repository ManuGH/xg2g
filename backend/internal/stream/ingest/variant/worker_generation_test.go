// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// The captures under testdata/segments carry a single PMT version each, so none of
// them contains a topology change on its own. Two of them do differ in topology at
// identical PIDs, which is enough to build one at run time:
//
//	verify_final_v3.ts    H.264 on PID 256, AAC on PID 257
//	test_hevc_stream.ts   HEVC  on PID 256, AAC on PID 257
//	verify_seg.ts         H.264 on PID 256, MP2 on PID 257
//
// Concatenating two of them yields real elementary data on both sides of the cut,
// which a synthetic fixture could not. The second capture's PMT version has to be
// rewritten first: the ring decides a topology changed from the version number and
// the program number, and both captures use version 0 for program 1. Without the
// bump the change would be invisible - which is itself worth knowing about spliced
// sources, even though spec-conformant DVB always increments.

const capturePMTPID = 4096

// bumpPMTVersion rewrites every complete PMT section in a capture to the given
// version, recomputing the section CRC. Sections that span packets are left alone;
// the captures repeat their PMT often enough that the single-packet ones suffice.
func bumpPMTVersion(t *testing.T, data []byte, version uint8) []byte {
	t.Helper()

	out := append([]byte(nil), data...)
	offset := -1
	for i := 0; i < ring.TSPacketSize && offset < 0; i++ {
		aligned := true
		for k := 0; k < 5; k++ {
			if i+k*ring.TSPacketSize >= len(out) || out[i+k*ring.TSPacketSize] != ring.SyncByte {
				aligned = false
				break
			}
		}
		if aligned {
			offset = i
		}
	}
	if offset < 0 {
		t.Fatal("capture is not TS aligned")
	}

	rewritten := 0
	for k := 0; offset+(k+1)*ring.TSPacketSize <= len(out); k++ {
		s := offset + k*ring.TSPacketSize
		pid := (uint16(out[s+1]&0x1F) << 8) | uint16(out[s+2])
		pusi := out[s+1]&0x40 != 0
		afc := (out[s+3] >> 4) & 0x03
		if pid != capturePMTPID || !pusi || afc == 0 || afc == 2 {
			continue
		}

		base := s + 4
		if afc == 3 {
			base = s + 5 + int(out[s+4])
		}
		if base >= s+ring.TSPacketSize {
			continue
		}
		sec := base + 1 + int(out[base])
		if sec+12 >= s+ring.TSPacketSize || out[sec] != 0x02 {
			continue
		}

		length := 3 + int(uint16(out[sec+1]&0x0F)<<8|uint16(out[sec+2]))
		if sec+length > s+ring.TSPacketSize {
			continue // section continues in the next packet
		}

		out[sec+5] = (out[sec+5] & 0xC1) | ((version & 0x1F) << 1)
		crc := ring.CalculateMPEG2CRC32(out[sec : sec+length-4])
		binary.BigEndian.PutUint32(out[sec+length-4:sec+length], crc)
		rewritten++
	}

	if rewritten == 0 {
		t.Fatal("no complete PMT section found to bump")
	}
	return out
}

func loadCapture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile("../../../../testdata/segments/" + name)
	if err != nil {
		t.Skipf("skipping: capture %s not available: %v", name, err)
	}
	return data
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("skipping: ffmpeg not available")
	}
}

// runGenerationCut feeds a worker the first capture, waits for it to be producing,
// then feeds the second one with a bumped PMT version, and reports how the worker
// ended.
func runGenerationCut(t *testing.T, firstCapture, secondCapture string, key AudioVariantKey) error {
	t.Helper()
	requireFFmpeg(t)

	first := loadCapture(t, firstCapture)
	second := bumpPMTVersion(t, loadCapture(t, secondCapture), 1)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()

	push := func(data []byte) {
		for i := 0; i < len(data); i += 64 * ring.TSPacketSize {
			end := i + 64*ring.TSPacketSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := masterRing.Push(data[i:end]); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	// Generation N has to be decodable before the worker attaches, otherwise the
	// primed attach has nothing to attach to.
	push(first[:len(first)/2])
	if _, ok := masterRing.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: first capture produced no random access point")
	}
	generationN := masterRing.Generation()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	worker := NewAudioVariantWorker(key, masterRing, 8*1024*1024)
	worker.Start(ctx)
	defer worker.Stop()

	// The rest of generation N has to keep flowing while the worker settles:
	// FFmpeg needs input to produce output, and the primed attach below waits on
	// that output.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		push(first[len(first)/2:])
	}()

	// Let the worker settle on generation N before anything changes.
	_, reader, err := worker.PrimedAttachWithTimeout(ctx, 15*time.Second)
	if err != nil {
		t.Skipf("skipping: worker never became primed on the first capture: %v", err)
	}
	buf := make([]byte, 32*1024)
	if _, rerr := reader.Read(buf); rerr != nil {
		t.Fatalf("worker produced no output on generation %d: %v", generationN, rerr)
	}
	_ = reader.Close()

	<-firstDone
	push(second)

	select {
	case <-worker.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("worker did not end after the upstream topology changed")
	}

	if got := masterRing.Generation(); got == generationN {
		t.Fatalf("ring generation never advanced past %d; the PMT bump was not seen", generationN)
	}
	return worker.Err()
}

// The hard case: the video codec changes under an unchanged PID while video is
// being copied. A running FFmpeg cannot follow that, so the worker must end rather
// than keep feeding HEVC into a process that probed H.264.
func TestAudioVariantWorker_VideoCodecChange_EndsWithGenerationCut(t *testing.T) {
	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "generation-cut-video",
	}

	err := runGenerationCut(t, "verify_final_v3.ts", "test_hevc_stream.ts", key)
	if !errors.Is(err, ErrUpstreamGenerationChanged) {
		t.Fatalf("worker ended with %v, want ErrUpstreamGenerationChanged", err)
	}
}

// The milder case: video stays H.264 and only the audio elementary stream changes.
// The generation is program identity, not video identity, so this must cut too -
// FFmpeg keeps its AAC decoder and would be handed MP2 payload otherwise.
func TestAudioVariantWorker_AudioCodecChange_EndsWithGenerationCut(t *testing.T) {
	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "generation-cut-audio",
	}

	err := runGenerationCut(t, "verify_final_v3.ts", "verify_seg.ts", key)
	if !errors.Is(err, ErrUpstreamGenerationChanged) {
		t.Fatalf("worker ended with %v, want ErrUpstreamGenerationChanged", err)
	}
}

// A terminated worker is never handed back out. The manager replaces it, so the
// next caller gets one attached to the current topology instead of a closed ring.
func TestAudioVariantManager_ReplacesTerminatedWorker(t *testing.T) {
	masterRing := ring.NewMasterRing(1024 * 1024)
	defer masterRing.Close()

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		StreamFingerprint: "replace-terminated",
	}

	ctx := context.Background()
	first, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("first GetOrCreateWorker: %v", err)
	}

	// Nothing decodable is ever pushed, so the attach times out and the worker ends.
	select {
	case <-first.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not end without a decodable upstream")
	}
	if !first.Terminated() {
		t.Fatal("Terminated() is false for a worker whose run loop has returned")
	}

	second, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("second GetOrCreateWorker: %v", err)
	}
	defer mgr.ReleaseWorker(key)

	if second == first {
		t.Fatal("manager handed back the terminated worker instead of building a new one")
	}
	if mgr.ActiveWorkerCount() != 1 {
		t.Fatalf("manager holds %d workers, want exactly the replacement", mgr.ActiveWorkerCount())
	}
}
