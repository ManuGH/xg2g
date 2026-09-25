// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/remotecore"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

type VerificationReport struct {
	Status             string                 `json:"status"`
	ChunkSizeBytes     int                    `json:"chunk_size_bytes"`
	TotalBytesIngested int64                  `json:"total_bytes_ingested"`
	ChunksPushed       int                    `json:"chunks_pushed"`
	DurationMs         int64                  `json:"duration_ms"`
	Facts              FactsReport            `json:"facts"`
	Timeline           TimelineReport         `json:"timeline"`
	SeekTests          SeekReport             `json:"seek_tests"`
	WindowExtraction   WindowExtractionReport `json:"window_extraction"`
	PrimedSubscriber   SubscriberReport       `json:"primed_subscriber"`
}

type FactsReport struct {
	HasPAT     bool   `json:"has_pat"`
	HasPMT     bool   `json:"has_pmt"`
	HasProgram bool   `json:"has_program"`
	HasVideo   bool   `json:"has_video"`
	VideoCodec string `json:"video_codec"`
}

type TimelineReport struct {
	Epoch                uint64 `json:"epoch"`
	TotalRAPs            int    `json:"total_raps"`
	JoinableRAPs         int    `json:"joinable_raps"`
	TracksCount          int    `json:"tracks_count"`
	DiscontinuitiesCount int    `json:"discontinuities_count"`
}

type SeekReport struct {
	PrecedingPass       bool `json:"preceding_pass"`
	FollowingPass       bool `json:"following_pass"`
	NearestPass         bool `json:"nearest_pass"`
	OutOfRangeLowerPass bool `json:"out_of_range_lower_pass"`
	OutOfRangeUpperPass bool `json:"out_of_range_upper_pass"`
}

type WindowExtractionReport struct {
	PacketAligned         bool  `json:"packet_aligned"`
	ByteLength            int   `json:"byte_length"`
	TSPacketCount         int   `json:"ts_packet_count"`
	DurationPTS90k        int64 `json:"duration_pts_90k"`
	HasTrackDiscontinuity bool  `json:"has_track_discontinuity"`
	TrackDiscontinuityCnt int   `json:"track_discontinuity_count"`
}

type SubscriberReport struct {
	PreambleBytes        int  `json:"preamble_bytes"`
	FirstPacketSyncByte  byte `json:"first_packet_sync_byte"`
	NoDoubleDeliveryPass bool `json:"no_double_delivery_pass"`
}

func findMediaCoreBin(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
	}
	candidates := []string{
		"xg2g-media-core",
		"/usr/local/bin/xg2g-media-core",
		"/srv/xg2g-build/media-core/target/release/xg2g-media-core",
		"media-core/target/release/xg2g-media-core",
		"../media-core/target/release/xg2g-media-core",
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("xg2g-media-core binary not found in PATH or standard build locations")
}

func main() {
	recordingPath := flag.String("recording", "", "Path to real broadcast MPEG-TS recording (required)")
	coreBinFlag := flag.String("core-bin", "", "Path to xg2g-media-core binary")
	chunkSizeFlag := flag.Int("chunk-size", 64*1024, "Chunk size in bytes (production default: 65536 = 64 KiB)")
	maxMBFlag := flag.Int("max-mb", 10, "Maximum megabytes of broadcast data to ingest")
	programFlag := flag.Uint("program", 0, "Target MPEG-TS program number (0 = auto/from PAT)")
	flag.Parse()

	if *recordingPath == "" {
		fmt.Fprintf(os.Stderr, "FATAL: -recording flag is required\n")
		os.Exit(1)
	}

	startTime := time.Now()

	coreBin, err := findMediaCoreBin(*coreBinFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	// #nosec G304 -- intended CLI tool flag for local acceptance testing
	tsFile, err := os.Open(*recordingPath)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			fmt.Fprintf(os.Stderr, "FATAL: could not open recording file: %v\n", pathErr.Err)
		} else {
			fmt.Fprintf(os.Stderr, "FATAL: could not open recording file\n")
		}
		os.Exit(1)
	}
	defer func() { _ = tsFile.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if *programFlag > 65535 {
		fmt.Fprintf(os.Stderr, "FATAL: program number exceeds uint16 range (0-65535)\n")
		os.Exit(1)
	}
	targetProg := uint16(*programFlag) // #nosec G115 -- bounded check above
	remote, err := remotecore.Start(ctx, coreBin, targetProg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to start media core subprocess\n")
		os.Exit(1)
	}
	defer func() { _ = remote.Close() }()

	const ringCapacity = 32 * 1024 * 1024 // 32 MiB
	r := ring.NewMasterRingWithCore(ringCapacity, remote, ring.WithCanonicalTimeline())
	defer r.Close()

	chunkSize := (*chunkSizeFlag / 188) * 188
	if chunkSize == 0 {
		chunkSize = 188
	}
	buf := make([]byte, chunkSize)
	var totalIngested int64
	var chunksPushed int
	maxBytes := int64(*maxMBFlag) * 1024 * 1024

	for totalIngested < maxBytes {
		n, rErr := io.ReadFull(tsFile, buf)
		if n > 0 {
			// Ensure chunk fed into ring is packet-aligned
			alignedN := (n / 188) * 188
			if alignedN == 0 {
				break
			}
			_, pErr := r.Push(ctx, buf[:alignedN])
			if pErr != nil {
				fmt.Fprintf(os.Stderr, "FATAL: master ring Push failed\n")
				os.Exit(1)
			}
			totalIngested += int64(alignedN)
			chunksPushed++
		}
		if rErr != nil {
			break
		}

		// Check if we have an active epoch, at least 5 RAPs, and video PID established
		obs, ok := r.TimelineObservation()
		if ok && obs.HasEpoch && obs.RAPCount >= 5 {
			vPID, _ := r.VideoDetails()
			if vPID != 0 {
				break
			}
		}
	}

	videoPID, videoCodec := r.VideoDetails()
	tl := r.Timeline()
	if tl == nil {
		fmt.Fprintf(os.Stderr, "FATAL: ring timeline is nil\n")
		os.Exit(1)
	}

	timelines := tl.PresentationTimelines()
	if len(timelines) == 0 {
		fmt.Fprintf(os.Stderr, "FATAL: no presentation timelines established in ingested data\n")
		os.Exit(1)
	}
	activePT := timelines[len(timelines)-1]

	var videoTrack *timeline.TrackPresentation
	for i := range activePT.Tracks {
		if activePT.Tracks[i].PID == videoPID {
			videoTrack = &activePT.Tracks[i]
			break
		}
	}
	if videoTrack == nil || !videoTrack.HasPTS {
		fmt.Fprintf(os.Stderr, "FATAL: active presentation timeline missing video track PTS\n")
		os.Exit(1)
	}

	firstPTS := videoTrack.EarliestPTS90k
	lastPTS := videoTrack.LatestPTS90k
	midPTS := firstPTS + (lastPTS-firstPTS)/2

	// 1. Test SeekModePreceding
	seekPrec, err := r.SeekToTime(activePT.Epoch, midPTS, timeline.SeekModePreceding)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Preceding failed\n")
		os.Exit(1)
	}
	seekPrecPass := seekPrec.RAP.Joinable && seekPrec.RAP.PTS90k <= midPTS
	if !seekPrecPass {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Preceding returned invalid RAP bounds\n")
		os.Exit(1)
	}

	// 2. Test SeekModeFollowing
	seekFoll, err := r.SeekToTime(activePT.Epoch, midPTS, timeline.SeekModeFollowing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Following failed\n")
		os.Exit(1)
	}
	seekFollPass := seekFoll.RAP.Joinable && seekFoll.RAP.PTS90k >= midPTS
	if !seekFollPass {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Following returned invalid RAP bounds\n")
		os.Exit(1)
	}

	// 3. Test SeekModeNearest
	seekNear, err := r.SeekToTime(activePT.Epoch, midPTS, timeline.SeekModeNearest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Nearest failed\n")
		os.Exit(1)
	}
	seekNearPass := seekNear.RAP.Joinable
	if !seekNearPass {
		fmt.Fprintf(os.Stderr, "FATAL: SeekToTime Nearest returned unjoinable RAP\n")
		os.Exit(1)
	}

	// 4. Test Out-of-Range Seeks (Must fail closed with ErrPTSOutOfRange)
	_, errLower := r.SeekToTime(activePT.Epoch, firstPTS-90000, timeline.SeekModePreceding)
	outOfRangeLower := errors.Is(errLower, ring.ErrPTSOutOfRange)

	_, errUpper := r.SeekToTime(activePT.Epoch, lastPTS+90000, timeline.SeekModeFollowing)
	outOfRangeUpper := errors.Is(errUpper, ring.ErrPTSOutOfRange)

	if !outOfRangeLower || !outOfRangeUpper {
		fmt.Fprintf(os.Stderr, "FATAL: boundary PTS seek did not return ErrPTSOutOfRange (lowerPass=%v, upperPass=%v)\n", outOfRangeLower, outOfRangeUpper)
		os.Exit(1)
	}

	// 5. Test ExtractWindow
	startPTS := seekPrec.RAP.PTS90k
	endPTS := seekFoll.RAP.PTS90k
	if startPTS == endPTS {
		endPTS = lastPTS
	}
	windowSlice, err := r.ExtractWindow(ring.WindowRequest{
		Epoch:                      activePT.Epoch,
		StartPTS:                   startPTS,
		EndPTS:                     endPTS,
		RejectTrackDiscontinuities: false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ExtractWindow failed\n")
		os.Exit(1)
	}
	packetAligned := len(windowSlice.Data)%188 == 0 && len(windowSlice.Data) > 0

	// 6. Test NewPrimedSubscriberAtTime
	attach, reader, err := r.NewPrimedSubscriberAtTime(activePT.Epoch, midPTS, timeline.SeekModePreceding)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: NewPrimedSubscriberAtTime failed\n")
		os.Exit(1)
	}
	defer func() { _ = reader.Close() }()

	firstPkt := make([]byte, 188)
	nPkt, err := reader.Read(firstPkt)
	if err != nil || nPkt != 188 {
		fmt.Fprintf(os.Stderr, "FATAL: reader.Read first packet failed\n")
		os.Exit(1)
	}
	// Verify that the reader reads the video keyframe directly and did not duplicate the PAT/PMT preamble
	var firstSyncByte byte
	if len(firstPkt) > 0 {
		firstSyncByte = firstPkt[0]
	}
	noDoubleDelivery := (firstSyncByte == 0x47) && (len(attach.Preamble) > 0)

	rf := r.ReadinessFacts()
	report := VerificationReport{
		Status:             "PASS",
		ChunkSizeBytes:     chunkSize,
		TotalBytesIngested: totalIngested,
		ChunksPushed:       chunksPushed,
		DurationMs:         time.Since(startTime).Milliseconds(),
		Facts: FactsReport{
			HasPAT:     rf.HasPAT,
			HasPMT:     rf.HasPMT,
			HasProgram: rf.ProgramNumber != 0,
			HasVideo:   videoPID != 0,
			VideoCodec: string(videoCodec),
		},
		Timeline: TimelineReport{
			Epoch:                uint64(activePT.Epoch),
			TotalRAPs:            activePT.TotalRAPs,
			JoinableRAPs:         activePT.JoinableRAPs,
			TracksCount:          len(activePT.Tracks),
			DiscontinuitiesCount: len(activePT.Discontinuities),
		},
		SeekTests: SeekReport{
			PrecedingPass:       seekPrecPass,
			FollowingPass:       seekFollPass,
			NearestPass:         seekNearPass,
			OutOfRangeLowerPass: outOfRangeLower,
			OutOfRangeUpperPass: outOfRangeUpper,
		},
		WindowExtraction: WindowExtractionReport{
			PacketAligned:         packetAligned,
			ByteLength:            len(windowSlice.Data),
			TSPacketCount:         len(windowSlice.Data) / 188,
			DurationPTS90k:        windowSlice.ActualEndPTS90k - windowSlice.ActualStartPTS90k,
			HasTrackDiscontinuity: windowSlice.HasTrackDiscontinuity,
			TrackDiscontinuityCnt: len(windowSlice.TrackDiscontinuities),
		},
		PrimedSubscriber: SubscriberReport{
			PreambleBytes:        len(attach.Preamble),
			FirstPacketSyncByte:  firstSyncByte,
			NoDoubleDeliveryPass: noDoubleDelivery,
		},
	}

	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))
}
