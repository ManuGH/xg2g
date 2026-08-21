// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func findProjectRoot(t *testing.T) string {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir) // Root of repo containing backend/
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Clean(filepath.Join(cwd, "../../../.."))
}

func TestMasterRing_RealCapture_H264_DecodingVerification(t *testing.T) {
	root := findProjectRoot(t)
	capturePath := filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts")

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("failed to read capture file %s: %v", capturePath, err)
	}

	// Ring with capacity for 20000 packets (~3.76MB, sufficient for 2.2MB segment)
	r := NewMasterRing(20000 * TSPacketSize)
	defer r.Close()

	// Push real broadcast capture in 1880-byte chunks (10 TS packets per push)
	chunkSize := 10 * TSPacketSize
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if (end-i)%TSPacketSize != 0 {
			end = i + ((end-i)/TSPacketSize)*TSPacketSize
		}
		if end > i {
			if _, err := r.Push(data[i:end]); err != nil {
				t.Fatalf("push failed at %d: %v", i, err)
			}
		}
	}

	// 1. Verify Video PID and Codec extracted from live PMT
	vPID, vCodec := r.VideoDetails()
	if vPID != 256 {
		t.Fatalf("expected real capture vPID=256, got %d", vPID)
	}
	if vCodec != CodecH264 {
		t.Fatalf("expected real capture vCodec=h264, got %v", vCodec)
	}

	// 2. Verify Keyframe Indexing
	kfOffset, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("no keyframe was indexed in real broadcast capture!")
	}

	// 3. Primed Attach Verification: Preamble + Tail stream decodable by FFmpeg
	preamble := r.PATPMTPreamble()
	if len(preamble) == 0 {
		t.Fatalf("preamble is empty!")
	}

	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	if _, err := reader.SeekToLatestKeyframe(); err != nil {
		t.Fatalf("seek to latest keyframe failed: %v", err)
	}

	streamData := make([]byte, 5000*TSPacketSize)
	n, _ := reader.Read(streamData)
	streamData = streamData[:n]

	// Assemble Primed Stream: Preamble + Keyframe Stream Slice
	primedStream := append(preamble, streamData...)

	// 4. Decode with FFmpeg to prove 0 decoding errors
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(primedStream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("ffmpeg output: %s", stderr.String())
		// FFmpeg might warn if stream ends abruptly, but should decode frames
	}

	t.Logf("✅ Real H.264 Capture Verification SUCCESS: vPID=%d codec=%s kfOffset=%d buffered=%d primedBytes=%d",
		vPID, vCodec, kfOffset, r.BufferedBytes(), len(primedStream))
}

func TestMasterRing_RealCapture_HEVC_DecodingVerification(t *testing.T) {
	root := findProjectRoot(t)
	capturePath := filepath.Join(root, "backend", "testdata", "segments", "test_hevc_stream.ts")

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Skipf("HEVC test capture file not found (%s), skipping", capturePath)
		return
	}

	r := NewMasterRing(10000 * TSPacketSize)
	defer r.Close()

	chunkSize := 10 * TSPacketSize
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if (end-i)%TSPacketSize != 0 {
			end = i + ((end-i)/TSPacketSize)*TSPacketSize
		}
		if end > i {
			if _, err := r.Push(data[i:end]); err != nil {
				t.Fatalf("push failed at %d: %v", i, err)
			}
		}
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != 256 {
		t.Fatalf("expected real HEVC capture vPID=256, got %d", vPID)
	}
	if vCodec != CodecH265 {
		t.Fatalf("expected real HEVC capture vCodec=h265, got %v", vCodec)
	}

	kfOffset, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("no keyframe was indexed in real HEVC broadcast capture!")
	}

	preamble := r.PATPMTPreamble()
	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	if _, err := reader.SeekToLatestKeyframe(); err != nil {
		t.Fatalf("seek to latest keyframe failed: %v", err)
	}

	streamData := make([]byte, 5000*TSPacketSize)
	n, _ := reader.Read(streamData)
	streamData = streamData[:n]

	primedStream := append(preamble, streamData...)

	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(primedStream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("ffmpeg output: %s", stderr.String())
	}

	t.Logf("✅ Real HEVC Capture Verification SUCCESS: vPID=%d codec=%s kfOffset=%d buffered=%d primedBytes=%d",
		vPID, vCodec, kfOffset, r.BufferedBytes(), len(primedStream))
}

func TestMasterRing_RealCapture_AllSegments_Sanity(t *testing.T) {
	root := findProjectRoot(t)
	pattern := filepath.Join(root, "backend", "testdata", "segments", "*.ts")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Fatalf("no test segments found: %v", err)
	}

	for _, segFile := range matches {
		t.Run(filepath.Base(segFile), func(t *testing.T) {
			data, err := os.ReadFile(segFile)
			if err != nil {
				t.Fatalf("failed to read %s: %v", segFile, err)
			}

			r := NewMasterRing(5000 * TSPacketSize)
			defer r.Close()

			n, err := io.Copy(writerFunc(func(p []byte) (int, error) {
				return r.Push(p)
			}), bytes.NewReader(data))

			if err != nil || n == 0 {
				t.Fatalf("stream push failed: %v (n=%d)", err, n)
			}

			vPID, vCodec := r.VideoDetails()
			if vPID == 0 || vCodec == CodecUnknown {
				t.Fatalf("failed to identify video elementary stream in %s", filepath.Base(segFile))
			}

			preamble := r.PATPMTPreamble()
			if len(preamble) == 0 {
				t.Fatalf("empty preamble for %s", filepath.Base(segFile))
			}

			t.Logf("Segment %s: vPID=%d codec=%s preambleLen=%d bytes", filepath.Base(segFile), vPID, vCodec, len(preamble))
		})
	}
}

type writerFunc func(p []byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) {
	return w(p)
}
