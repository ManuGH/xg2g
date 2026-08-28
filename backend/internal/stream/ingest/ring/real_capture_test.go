// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Clean(filepath.Join(cwd, "../../../.."))
}

func countDecodedVideoFrames(t *testing.T, data []byte) int {
	cmd := exec.Command("ffprobe", "-v", "error", "-count_frames", "-select_streams", "v:0",
		"-show_entries", "stream=nb_read_frames", "-of", "default=nokey=1:noprint_wrappers=1", "pipe:0")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("ffprobe frame counting failed: %v (stderr: %s)", err, stderr.String())
	}

	fields := strings.Fields(stdout.String())
	if len(fields) == 0 {
		t.Fatalf("empty ffprobe frame counting output")
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("failed to parse decoded frame count from ffprobe output %q: %v", stdout.String(), err)
	}
	return count
}

func TestMasterRing_RealCapture_H264_DecodingVerification(t *testing.T) {
	skipIfNoFFmpeg(t)
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
			if _, err := r.Push(context.Background(), data[i:end]); err != nil {
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

	streamData := make([]byte, 20000*TSPacketSize)
	n, err := reader.Read(streamData)
	if err != nil && err != io.EOF {
		t.Fatalf("reader.Read failed: %v", err)
	}
	if n == 0 {
		t.Fatalf("reader returned 0 bytes after SeekToLatestKeyframe")
	}

	// Assemble Primed Stream: Preamble + Keyframe Stream Slice
	primedStream := append(preamble, streamData[:n]...)

	// 4. Strict FFmpeg Decoding Gate: fails test if cmd.Run returns error
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(primedStream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("strict FFmpeg decoding failed: %v (stderr: %s)", err, stderr.String())
	}

	// 5. Hard proof of decoded video frames
	decodedFrames := countDecodedVideoFrames(t, primedStream)
	if decodedFrames <= 0 {
		t.Fatalf("expected at least 1 decoded video frame, got %d", decodedFrames)
	}

	t.Logf("✅ Real H.264 Capture Verification SUCCESS: vPID=%d codec=%s kfOffset=%d decodedFrames=%d primedBytes=%d",
		vPID, vCodec, kfOffset, decodedFrames, len(primedStream))
}

func TestMasterRing_RemuxedHEVC_TS_DecodingVerification(t *testing.T) {
	skipIfNoFFmpeg(t)
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
			if _, err := r.Push(context.Background(), data[i:end]); err != nil {
				t.Fatalf("push failed at %d: %v", i, err)
			}
		}
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != 256 {
		t.Fatalf("expected HEVC capture vPID=256, got %d", vPID)
	}
	if vCodec != CodecH265 {
		t.Fatalf("expected HEVC capture vCodec=h265, got %v", vCodec)
	}

	kfOffset, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("no keyframe was indexed in HEVC capture!")
	}

	preamble := r.PATPMTPreamble()
	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	if _, err := reader.SeekToLatestKeyframe(); err != nil {
		t.Fatalf("seek to latest keyframe failed: %v", err)
	}

	streamData := make([]byte, 10000*TSPacketSize)
	n, err := reader.Read(streamData)
	if err != nil && err != io.EOF {
		t.Fatalf("reader.Read failed: %v", err)
	}
	if n == 0 {
		t.Fatalf("reader returned 0 bytes after SeekToLatestKeyframe")
	}

	primedStream := append(preamble, streamData[:n]...)

	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(primedStream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("strict FFmpeg HEVC decoding failed: %v (stderr: %s)", err, stderr.String())
	}

	decodedFrames := countDecodedVideoFrames(t, primedStream)
	if decodedFrames <= 0 {
		t.Fatalf("expected at least 1 decoded video frame, got %d", decodedFrames)
	}

	t.Logf("✅ Remuxed HEVC TS Verification SUCCESS: vPID=%d codec=%s kfOffset=%d decodedFrames=%d primedBytes=%d",
		vPID, vCodec, kfOffset, decodedFrames, len(primedStream))
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
				return r.Push(context.Background(), p)
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
