package v3

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/vod"
)

// prepareVODRequest gathers all necessary information to start a VOD build.
// It performs probing and keyframe analysis synchronously.
func (s *Server) prepareVODRequest(ctx context.Context, recordingID, serviceRef, localPath, cachePath string) (vod.RemuxRequest, error) {
	logger := log.L().With().
		Str("component", "vod-prep").
		Str("recording", recordingID).
		Logger()

	// Binaries
	ffprobeBin := "ffprobe"

	// Input selection: support multi-part recordings via concat list.
	inputPath := localPath
	probePath := localPath
	concatPath := ""
	cleanupPaths := []string{}

	if parts, partErr := RecordingParts(localPath); partErr == nil && len(parts) > 0 {
		probePath = parts[0]
		if len(parts) > 1 {
			concatPath = cachePath + ".concat.txt"
			if err := WriteConcatList(concatPath, parts); err != nil {
				return vod.RemuxRequest{}, fmt.Errorf("concat list: %w", err)
			}
			inputPath = concatPath
			cleanupPaths = append(cleanupPaths, concatPath)
			logger.Info().Int("parts", len(parts)).Msg("using concat input")
		} else {
			inputPath = parts[0]
		}
	} else if partErr != nil && !errors.Is(partErr, ErrRecordingNotFound) {
		return vod.RemuxRequest{}, fmt.Errorf("recording parts: %w", partErr)
	}

	// Step 1: Probe streams
	var videoCodec, videoPixFmt, audioCodec string
	var videoBitDepth, audioTracks, audioDelayMs int

	streamInfo, err := ProbeStreams(ctx, ffprobeBin, probePath)
	if err != nil {
		logger.Warn().Err(err).Msg("stream probe failed - using default DVB assumptions")
		videoCodec = "h264"
		videoPixFmt = "yuv420p"
		videoBitDepth = 8
		audioCodec = "ac3"
		audioTracks = 1
	} else {
		videoCodec = streamInfo.Video.CodecName
		videoPixFmt = streamInfo.Video.PixFmt
		videoBitDepth = streamInfo.Video.BitDepth
		audioCodec = streamInfo.Audio.CodecName
		audioTracks = streamInfo.Audio.TrackCount
		audioDelayMs = ComputeAudioDelayMs(streamInfo)
	}

	// Step 2: Precision Start Time
	startTime, err := FindKeyframeStart(ctx, ffprobeBin, probePath)
	if err != nil {
		logger.Warn().Err(err).Msg("keyframe analysis failed - defaulting to 1")
		startTime = "1"
	}

	return vod.RemuxRequest{
		ID:         cachePath, // Unique build ID (using output path)
		InputPath:  inputPath,
		OutputPath: cachePath,
		StartTime:  startTime,
		StreamInfo: vod.StreamInfo{
			VideoCodec:    videoCodec,
			VideoPixFmt:   videoPixFmt,
			VideoBitDepth: videoBitDepth,
			AudioCodec:    audioCodec,
			AudioTracks:   audioTracks,
			AudioDelayMs:  audioDelayMs,
		},
		CleanupPaths: cleanupPaths,
	}, nil
}

// executeVODWork performs the actual VOD remux (MP4) using the VOD package helpers.
// It is designed to be passed as a WorkFuncSpec to vod.Manager.
func (s *Server) executeVODWork(req vod.RemuxRequest) vod.WorkFuncSpec {
	return func(ctx context.Context, spec vod.JobSpec) error {
		logger := log.L().With().
			Str("component", "vod-worker").
			Str("recording", spec.ID).
			Str("serviceRef", spec.ServiceRef).
			Logger()

		defer func() {
			// Cleanup temporary files
			for _, path := range req.CleanupPaths {
				_ = os.Remove(path)
			}
		}()

		// Build Arguments
		buildInput := vod.BuildArgsInput{
			InputPath:     req.InputPath,
			OutputPath:    req.OutputPath,
			StartTime:     req.StartTime,
			VideoCodec:    req.StreamInfo.VideoCodec,
			VideoPixFmt:   req.StreamInfo.VideoPixFmt,
			VideoBitDepth: req.StreamInfo.VideoBitDepth,
			AudioCodec:    req.StreamInfo.AudioCodec,
			AudioTracks:   req.StreamInfo.AudioTracks,
			AudioDelayMs:  req.StreamInfo.AudioDelayMs,
		}

		decision := vod.BuildRemuxArgs(buildInput)

		if decision.Strategy == "unsupported" {
			return fmt.Errorf("unsupported codec: %s", decision.Reason)
		}

		// Execute
		exec := &vod.DefaultExecutor{Logger: logger}
		return exec.Run(ctx, "ffmpeg", decision.Args)
	}
}
