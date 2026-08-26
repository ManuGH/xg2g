package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/hls"
	"github.com/ManuGH/xg2g/internal/hls/cmaf"
	"github.com/ManuGH/xg2g/internal/infra/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Start initiates the media process.
func (a *LocalAdapter) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	// Everything up to the spawn runs on prepCtx, which carries the caller's
	// PrepareDeadline. The process itself is started from ctx further down, so
	// the preparation bound can never reach the stream it prepared: tuning,
	// resolution, preflight and the plan probes are bounded, the pipeline is not.
	//
	// Without this bound the pre-spawn work sat outside the caller's startup
	// budget entirely — a slow receiver could consume all of it before ffmpeg
	// existed, and the session then failed on a packager timeout for a packager
	// that had never been given a chance to run.
	prepCtx := ctx
	if !spec.PrepareDeadline.IsZero() {
		var cancelPrepare context.CancelFunc
		prepCtx, cancelPrepare = context.WithDeadline(ctx, spec.PrepareDeadline)
		defer cancelPrepare()
	}

	if spec.Source.Type == ports.SourceTuner && a.E2 != nil {
		if spec.Source.TunerSlot < 0 {
			return "", fmt.Errorf("invalid tuner slot: %d", spec.Source.TunerSlot)
		}
		tuner := enigma2.NewTuner(a.E2, spec.Source.TunerSlot, 10*time.Second)
		if err := tuner.Tune(prepCtx, spec.Source.ID); err != nil {
			return "", fmt.Errorf("tuning failed: %w", err)
		}
		telemetry.GetStartupTracer(spec.SessionID).MarkOnce("E_LOCK", "tuner_locked")
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "tuner_tuned").
			Int("tuner_slot", spec.Source.TunerSlot).
			Str("service_ref", spec.Source.ID).
			Msg("tuner tune completed")
	}

	inputURL := ""

	// ingestInput holds the shared-ingest attachment for a tuner source. It owns
	// the claim that keeps the upstream alive, so it is released only once the
	// process that reads it has exited - handed to the monitor below, or released
	// here if the spawn never happens.
	var ingestInput *sharedIngestInput
	ingestHandedOff := false
	defer func() {
		if ingestInput != nil && !ingestHandedOff {
			ingestInput.Release()
		}
	}()

	switch spec.Source.Type {
	case ports.SourceTuner:
		// The receiver URL is gone from this path. Bytes come from the one
		// connection shared ingest already holds for this service, and the startup
		// probes read a snapshot of its head from a file rather than opening
		// connections of their own. There is deliberately no fallback that
		// resolves a stream URL: a tuner transcode that cannot reach shared ingest
		// must fail, or the path this replaced would live on as an error handler.
		in, err := a.acquireSharedIngestInput(prepCtx, spec)
		if err != nil {
			return "", err
		}
		ingestInput = in
		inputURL = in.ProbePath()
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "input_source_selected").
			Str("probe_source", inputURL).
			Msg("live input taken from shared ingest")
	case ports.SourceURL:
		inputURL = spec.Source.ID
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "input_url_selected").
			Str("input_url", sanitizeURLForLog(inputURL)).
			Msg("direct input url selected")
	}
	sourceKey := fpsCacheKey(spec.Source, inputURL)

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_args_build_started").
		Str("input_url", sanitizeURLForLog(inputURL)).
		Msg("ffmpeg args build started")
	// Span covering the playback decision + ffprobe probes (the untraced gap
	// between request and spawn). No-op when tracing is off. planCtx lets any
	// probe spans added later nest under this one.
	planCtx, planSpan := telemetry.Tracer("xg2g.ffmpeg").Start(prepCtx, "playback.plan",
		trace.WithAttributes(
			attribute.String("xg2g.session_id", spec.SessionID),
			attribute.String("xg2g.source_type", fmt.Sprintf("%v", spec.Source.Type)),
		),
	)
	plan, err := a.buildArgsWithPlan(planCtx, spec, inputURL)
	if err != nil {
		planSpan.RecordError(err)
		planSpan.SetStatus(codes.Error, "plan build failed")
		planSpan.End()
		return "", fmt.Errorf("failed to build args: %w", err)
	}
	planSpan.SetAttributes(attribute.String("xg2g.path_id", plan.pathID))
	planSpan.End()
	args := plan.args

	// Live A/V-sync (flag-gated, default off): re-route the HTTP relay through
	// a Go peek that measures the source's audio-before-keyframe "orphan" and
	// corrects it via a stdin pipe — audio atrim for -c:v copy (constant
	// audio-leads desync in iOS AVPlayer), input -ss for video transcode
	// (startup segment with a leading video hole while audio runs on). Any
	// peek/measure failure falls back to the unchanged direct-URL path.
	var avsyncStdin io.Reader
	// The spec as it actually runs: the plan may have finalized a different
	// profile than the one requested, and both the avsync decision and the
	// startup watchdog have to reason about what runs, not what was asked for.
	effectiveSpec := spec
	effectiveSpec.Profile = plan.effectiveProfile
	var usingPipe bool

	// A tuner source is always a pipe now, and its bytes always come from the
	// spool. The two paths below that used to build their own HTTP connection to
	// the receiver only ever ran for this source type, so they are skipped
	// entirely rather than being given a second byte source to choose from.
	if ingestInput != nil {
		args = transformArgsForTelemetryPipeMode(args)
		avsyncStdin = ingestInput.Stdin()
		usingPipe = true
	}

	if !usingPipe && a.shouldAvsyncAtrim(effectiveSpec) {
		if orphan, stdin, useAtrim := a.prepareAvsyncPipe(ctx, inputURL, spec.SessionID); stdin != nil {
			args = transformArgsForAvsyncPipeMode(args, orphan, useAtrim && !a.LiveAvsyncPipeNoTrim, effectiveSpec.Profile.TranscodeVideo)
			avsyncStdin = stdin
			if !useAtrim {
				a.Logger.Warn().
					Str("session_id", spec.SessionID).
					Str("startup_phase", "avsync_pipe_diagnostic").
					Msg("live-copy avsync fallback: stdin pipe active without atrim correction")
			} else if a.LiveAvsyncPipeNoTrim {
				a.Logger.Warn().
					Str("session_id", spec.SessionID).
					Str("startup_phase", "avsync_pipe_diagnostic").
					Msg("live-copy avsync diagnostic: stdin pipe active without audio trim")
			}
		}
	}

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_args_built").
		Int("arg_count", len(args)).
		Msg("ffmpeg args build finished")

	// #nosec G204 - BinPath is trusted from config; args are generated by strict internal logic (buildArgs)
	var cmd *exec.Cmd
	systemdRunPath, err := exec.LookPath("systemd-run")
	_, statErr := os.Stat("/run/systemd/system")
	// systemd-run --scope requires root or sufficient polkit permissions.
	// Non-root users will get a permission error even when the binary and
	// /run/systemd/system exist, so skip systemd-run when not root.
	useSystemd := os.Geteuid() == 0 && err == nil && statErr == nil

	if useSystemd {
		scopeName := fmt.Sprintf("xg2g-media-%s", spec.SessionID)
		systemdArgs := []string{"--scope", "--unit=" + scopeName, a.BinPath}
		systemdArgs = append(systemdArgs, args...)
		cmd = exec.CommandContext(ctx, systemdRunPath, systemdArgs...) // #nosec G204
	} else {
		cmd = exec.CommandContext(ctx, a.BinPath, args...) // #nosec G204
	}

	if avsyncStdin != nil {
		cmd.Stdin = avsyncStdin
	}
	procgroup.Set(cmd) // Mandatory for tree reaping
	// On ctx cancellation, gracefully SIGTERM the whole process group so ffmpeg
	// can flush its final segment + ENDLIST, instead of the stdlib default which
	// SIGKILLs the leader PID only. WaitDelay then bounds how long Wait blocks
	// after cancellation before the runtime force-kills the leader and closes I/O.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return procgroup.TerminateGroup(cmd.Process.Pid)
	}
	if a.KillTimeout > 0 {
		cmd.WaitDelay = a.KillTimeout
	} else {
		cmd.WaitDelay = 5 * time.Second
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to pipe stderr: %w", err)
	}
	cmd.Stdout = nil

	// LL-HLS pipe mode: ffmpeg streams one fragmented MP4 on stdout and the
	// in-process segmenter produces the session artifacts. A manual os.Pipe
	// (instead of StdoutPipe) keeps the read end out of cmd.Wait's lifecycle
	// so the segmenter can drain the tail after process exit.
	var cmafRead, cmafWrite *os.File
	if plan.cmafSegment {
		cmafRead, cmafWrite, err = os.Pipe()
		if err != nil {
			return "", fmt.Errorf("cmaf pipe: %w", err)
		}
		cmd.Stdout = cmafWrite
	}

	// Span covering ffmpeg spawn -> first HLS segment (the user-perceived startup
	// latency). No-op when tracing is disabled; ended exactly once in the monitor.
	spawnedAt := time.Now()
	_, startupSpan := telemetry.Tracer("xg2g.ffmpeg").Start(ctx, "ffmpeg.startup",
		trace.WithAttributes(
			attribute.String("xg2g.session_id", spec.SessionID),
			attribute.String("xg2g.source_type", fmt.Sprintf("%v", spec.Source.Type)),
			attribute.String("xg2g.hw_backend", fmt.Sprintf("%v", argsHardwareBackend(args))),
			attribute.String("xg2g.path_id", plan.pathID),
		),
	)

	if a.inMemoryIngest && a.ingestServer != nil {
		a.ingestServer.Registry().GetOrCreate(spec.SessionID, nil)
	}

	if err := cmd.Start(); err != nil {
		if cmafRead != nil {
			_ = cmafRead.Close()
			_ = cmafWrite.Close()
		}
		startupSpan.RecordError(err)
		startupSpan.SetStatus(codes.Error, "ffmpeg start failed")
		startupSpan.End()
		return "", fmt.Errorf("ffmpeg start failed: %w", err)
	}
	metrics.IncActiveFFmpegProcesses()
	isDirectHTTP := avsyncStdin == nil && isHTTPInputURL(inputURL)
	if cmafRead != nil {
		// The child holds its own copy of the write end; closing ours makes
		// EOF reach the segmenter when ffmpeg exits.
		_ = cmafWrite.Close()
		segLogger := a.Logger.With().
			Str("component", "cmaf_segmenter").
			Str("session_id", spec.SessionID).
			Logger()
		segCfg := cmaf.Config{
			StreamID:          store.StreamID(spec.SessionID),
			Dir:               ports.SessionHLSDirForPolicy(a.HLSRoot, spec.SessionID, spec.Profile.DVRWindowSec),
			TargetDurationSec: plan.cmafTargetDurSec,
			ListSize:          plan.listSize,
			Logger:            segLogger,
			ShadowPublisher:   nil, // Shadow runtime is managed natively below
		}
		go func(r *os.File) {
			defer func() {
				_ = r.Close()
				if a.StoreRegistry != nil {
					a.StoreRegistry.Unregister(spec.SessionID)
				}
				hls.EvictRAPCache(spec.SessionID)
			}()
			_ = cmaf.Run(ctx, r, segCfg)
		}(cmafRead)
	}

	pid := cmd.Process.Pid
	_ = WriteWorkerState(spec.SessionID, spec.Source.ID, spec.Profile.Name, pid)
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_started").
		Int("pid", pid).
		Msg("ffmpeg process started")
	handle := ports.RunHandle(fmt.Sprintf("%s-%d", spec.SessionID, pid))
	a.mu.Lock()
	a.activeProcs[handle] = cmd
	a.handleSessions[handle] = spec.SessionID
	a.finalizedProfiles[handle] = plan.effectiveProfile
	// Execution truth: capture the plan parsed from the REAL argv we just handed
	// to the process, so observers report what ffmpeg runs, not a prediction.
	executedPlan := executedFFmpegPlanFromArgs(args)
	a.executedPlans[handle] = executedPlan
	delete(a.processDetails, handle)
	a.mu.Unlock()

	sessionDir := ports.SessionHLSDirForPolicy(a.HLSRoot, spec.SessionID, spec.Profile.DVRWindowSec)
	shadowRuntime, err := a.attachShadowStore(ctx, spec.SessionID, executedPlan, sessionDir)
	if err != nil {
		a.Logger.Warn().Err(err).Str("session_id", spec.SessionID).Msg("failed to attach shadow store, proceeding with disk only")
	}

	// The shared-ingest claim outlives Start and belongs to the process, not to
	// this call: the upstream stays alive only while a holder is attached, so
	// releasing it here would pull the stream out from under an ffmpeg that is
	// still reading. The monitor blocks on cmd.Wait, which makes its return the
	// first moment nothing is reading any more.
	ingestHandedOff = true
	releaseIngest := ingestInput
	go func() {
		defer releaseIngest.Release()                                                                                                                                                                                                                                                  // nil-safe; a non-tuner source has none
		a.monitorProcessWithStartTimeout(ctx, handle, cmd, stderr, spec.SessionID, spec.Profile.DVRWindowSec, argsHardwareBackend(args), plan.pathID, a.startTimeoutForSpec(effectiveSpec), startupSpan, spawnedAt, shadowRuntime, plan.effectiveProfile.TranscodeVideo, isDirectHTTP) // #nosec G118 -- goroutine receives the request-scoped ctx (first arg), not context.Background/TODO
	}()
	if sourceKey != "" {
		go a.learnFPSFromOutput(ctx, sourceKey, spec.SessionID, spec.Profile.DVRWindowSec)
	}

	metrics.RecordPipelineSpawn("ffmpeg", "admitted")
	return handle, nil
}

func (a *LocalAdapter) FinalizedProfile(handle ports.RunHandle) (ports.ProfileSpec, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	profile, ok := a.finalizedProfiles[handle]
	if !ok {
		return ports.ProfileSpec{}, false
	}
	return profile, true
}

// ExecutedFFmpegPlan returns the execution-truth plan parsed from the real argv
// of the process launched for the handle. It is the un-lie-able source for
// "what ffmpeg runs", as opposed to any profile-derived prediction.
func (a *LocalAdapter) ExecutedFFmpegPlan(handle ports.RunHandle) (ports.ExecutedFFmpegPlan, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	plan, ok := a.executedPlans[handle]
	if !ok {
		return ports.ExecutedFFmpegPlan{}, false
	}
	return plan, true
}

// shouldRecordHWRuntimeFailure reports whether a finished ffmpeg process should
// count as a hardware-encoder runtime failure for the sticky GPU->CPU demotion
// counter. Only a process that exited on its own (naturalExit) with a non-zero
// status AND that emitted a recognized encoder failure line qualifies. A
// deliberate stop (parentCtx cancel) or a watchdog stall termination kills the
// process too — procErr is non-nil in those cases — but that is not evidence
// the encoder failed, so it must not feed the demotion counter.
func shouldRecordHWRuntimeFailure(naturalExit bool, procErr error, runtimeFailureLine string) bool {
	if runtimeFailureLine == "" {
		return false
	}
	return naturalExit && procErr != nil
}

// processExitClassification captures how an ffmpeg process finished.
type processExitClassification struct {
	procErr          error
	resultErr        error
	naturalExit      bool
	watchdogConsumed bool
}

// awaitProcessExit blocks until the ffmpeg process finishes and classifies HOW
// it ended. naturalExit is true ONLY when the process exited on its own (the
// procErrCh case fired first). A watchdog stall and a context cancellation are
// both deliberate terminations and leave naturalExit false.
//
// Subtlety that this function exists to get right: the watchdog context is
// derived from parentCtx, so a parentCtx cancellation (user stop) ALSO cancels
// the watchdog, which then returns nil — i.e. wdErrCh receives nil at the same
// time parentCtx.Done() becomes ready. The select may pick either case, so the
// wdErr==nil branch is a context cancellation too and must NOT be treated as a
// natural exit (otherwise ~half of user stops would be misclassified).
func awaitProcessExit(
	parentCtx context.Context,
	procErrCh <-chan error,
	wdErrCh <-chan error,
	onWatchdogStall func(stallErr error),
	onContextCanceled func(),
) processExitClassification {
	var out processExitClassification
	select {
	case procErr := <-procErrCh:
		out.procErr = procErr
		// When the parent context is canceled (user stop), the proc may also
		// finish at the same instant, making both this case and parentCtx.Done()
		// ready. The select picks whichever is ready first, so we must check
		// parentCtx here too — a user stop is never a natural exit.
		if parentCtx.Err() != nil {
			out.resultErr = parentCtx.Err()
		} else {
			out.resultErr = procErr
			out.naturalExit = true
		}
	case wdErr := <-wdErrCh:
		out.watchdogConsumed = true
		if wdErr != nil {
			onWatchdogStall(wdErr)
			out.procErr = <-procErrCh
			out.resultErr = wdErr
			return out
		}
		// wdErr == nil: can happen either because parentCtx was canceled (user stop)
		// or because the watchdog finished naturally (e.g. progress=end).
		if parentCtx.Err() != nil {
			onContextCanceled()
			out.procErr = <-procErrCh
			out.resultErr = parentCtx.Err()
		} else {
			out.procErr = <-procErrCh
			out.resultErr = out.procErr
			out.naturalExit = true
		}
	case <-parentCtx.Done():
		onContextCanceled()
		out.procErr = <-procErrCh
		out.resultErr = parentCtx.Err()
	}
	return out
}

func (a *LocalAdapter) monitorProcessWithStartTimeout(parentCtx context.Context, handle ports.RunHandle, cmd *exec.Cmd, stderr io.ReadCloser, sessionID string, dvrWindowSec int, hwBackend profiles.GPUBackend, pathID string, startTimeout time.Duration, startupSpan trace.Span, spawnedAt time.Time, shadowRuntime *ShadowRuntime, transcodeVideo bool, _ bool) {
	defer func() {
		a.mu.Lock()
		a.removeActiveProcessLocked(handle, true)
		a.mu.Unlock()
		if shadowRuntime != nil {
			shadowRuntime.Close()
		}
		hls.EvictRAPCache(sessionID)
		if a.HLSRoot != "" && sessionID != "" {
			hls.EvictRAPCache(ports.SessionHLSDirForPolicy(a.HLSRoot, sessionID, dvrWindowSec))
		}
		dc := a.GetDiagnosticContext(sessionID)
		a.Logger.Info().
			Str("session_id", dc.SessionID).
			Str("generation_id", dc.GenerationID).
			Str("reason", dc.Reason).
			Int64("elapsed_since_stop_ms", dc.ElapsedSinceStopMs).
			Msg("ffmpeg_process_exited")
		metrics.DecActiveFFmpegProcesses()
	}()

	// End the startup span exactly once: success is recorded on first segment;
	// any other exit path (start timeout, early ffmpeg exit, ctx cancel) is an error.
	var sawFirstSegment atomic.Bool
	endStartupSpan := sync.OnceFunc(func() { startupSpan.End() })
	defer func() {
		if !sawFirstSegment.Load() {
			startupSpan.SetStatus(codes.Error, "ffmpeg exited before first HLS segment")
		}
		endStartupSpan()
	}()

	wd := watchdog.New(startTimeout, a.StallTimeout)

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	wdCtx, wdCancel := context.WithCancel(parentCtx)
	defer wdCancel()
	observerCtx, observerCancel := context.WithCancel(parentCtx)
	defer observerCancel()

	wdErrCh := make(chan error, 1)
	go func() {
		wdErrCh <- wd.Run(wdCtx)
	}()

	runtimeFailureLine := ""
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)

		// Forward lines to RingBuffer/Log and Watchdog.
		scanner := bufio.NewScanner(stderr)
		scanner.Split(scanFFmpegLogTokens)
		firstFrameLogged := false
		firstSegmentLogged := false
		outputObserverStarted := false

		logFFmpegLine := func(level zerolog.Level, line string, repeats int) {
			evt := a.Logger.WithLevel(level).Str("sessionId", sessionID).Str("ffmpeg_log", line)
			if repeats > 0 {
				evt = evt.Int("repeated", repeats)
			}
			evt.Msg("ffmpeg output")
		}
		dedup := newFFmpegLogDeduper(10 * time.Second)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Opening '") && strings.Contains(line, "' for reading") {
				telemetry.GetStartupTracer(sessionID).MarkOnce(telemetry.MilestoneT1, "input_opened")
			}
			if strings.Contains(line, "Stream mapping:") {
				telemetry.GetStartupTracer(sessionID).MarkOnce(telemetry.MilestoneT3, "probe_completed")
			}
			if !firstFrameLogged {
				if frame, ok := parseFFmpegFrameCount(line); ok && frame > 0 {
					firstFrameLogged = true
					if transcodeVideo {
						telemetry.GetStartupTracer(sessionID).MarkOnce(telemetry.MilestoneT4, "first_usable_video_au")
					}
					wd.ObserveProgress()
					a.writeFirstFrameMarker(sessionID, dvrWindowSec)
					a.Logger.Info().
						Str("session_id", sessionID).
						Str("startup_phase", "first_frame").
						Int("frame", frame).
						Msg("ffmpeg first frame observed")
				}
			}
			if !firstSegmentLogged {
				if segmentPath, ok := extractStartupSegmentPath(line); ok {
					firstSegmentLogged = true
					sawFirstSegment.Store(true)
					startupSpan.SetAttributes(attribute.Int64("xg2g.time_to_first_segment_ms", time.Since(spawnedAt).Milliseconds()))
					startupSpan.SetStatus(codes.Ok, "")
					endStartupSpan()
					wd.ObserveProgress()
					a.Logger.Info().
						Str("session_id", sessionID).
						Str("startup_phase", "first_segment_write").
						Str("segment_path", segmentPath).
						Msg("ffmpeg first segment write observed")
					if pathID != "" && !outputObserverStarted {
						outputObserverStarted = true
						go a.detector.observeRuntimePathCorrectness(observerCtx, handle, cmd, sessionID, pathID)
					}
				}
			}
			sanitizedLine := sanitizeFFmpegLogLine(line)
			a.recordRuntimeDiagnostics(handle, line, sanitizedLine)
			if detail := summarizeFFmpegFailureLine(sanitizedLine); detail != "" {
				a.recordProcessDetail(handle, detail)
			}
			if runtimeFailureLine == "" {
				switch hwBackend {
				case profiles.GPUBackendVAAPI:
					if isVAAPIRuntimeFailureLine(sanitizedLine) {
						runtimeFailureLine = sanitizedLine
					}
				case profiles.GPUBackendNVENC:
					if isNVENCRuntimeFailureLine(sanitizedLine) {
						runtimeFailureLine = sanitizedLine
					}
				}
			}
			dedup.observe(sanitizedLine, ffmpegLogLevel(sanitizedLine), logFFmpegLine)
			wd.ParseLine(line)
		}
		dedup.flush(logFFmpegLine)
		if scanErr := scanner.Err(); scanErr != nil {
			a.Logger.Warn().Err(scanErr).Str("sessionId", sessionID).Msg("ffmpeg stderr scan failed")
		}
	}()

	procErrCh := make(chan error, 1)
	go func() {
		// os/exec closes StderrPipe from Wait. Draining stderr first avoids
		// dropping the final FFmpeg failure lines that Health surfaces later.
		<-scanDone
		procErrCh <- cmd.Wait()
	}()

	classification := awaitProcessExit(
		parentCtx, procErrCh, wdErrCh,
		func(stallErr error) {
			metrics.IncLiveFFmpegStall("watchdog_timeout")
			a.recordProcessDetail(handle, ports.DetailTranscodeStalled)
			a.Logger.Error().Err(stallErr).Str("sessionId", sessionID).Msg("watchdog triggered process termination")
			a.terminateProcessGroup(cmd, sessionID)
		},
		func() {
			a.terminateProcessGroup(cmd, sessionID)
		},
	)
	procErr := classification.procErr
	resultErr := classification.resultErr
	watchdogConsumed := classification.watchdogConsumed
	naturalExit := classification.naturalExit

	wdCancel()
	if !watchdogConsumed {
		if wdErr := <-wdErrCh; wdErr != nil {
			metrics.IncLiveFFmpegStall("watchdog_timeout")
			a.recordProcessDetail(handle, ports.DetailTranscodeStalled)
			a.Logger.Error().Err(wdErr).Str("sessionId", sessionID).Msg("watchdog reported failure")
			if resultErr == nil {
				resultErr = wdErr
			}
		}
	}

	<-scanDone

	if shouldRecordHWRuntimeFailure(naturalExit, procErr, runtimeFailureLine) {
		switch hwBackend {
		case profiles.GPUBackendVAAPI:
			a.recordVAAPIRuntimeFailure(sessionID, runtimeFailureLine)
		case profiles.GPUBackendNVENC:
			a.recordNVENCRuntimeFailure(sessionID, runtimeFailureLine)
		}
	}

	if procErr != nil {
		a.recordProcessDetail(handle, summarizeProcessExit(procErr))
		if sig, crashed := isProcessCrash(procErr); crashed {
			// Loud and greppable on purpose: a fault is a defect in the encoder or
			// the driver, not a stream problem, and it must not be readable as one.
			a.Logger.Error().
				Err(procErr).
				Str("event", "ffmpeg_crashed").
				Str("sessionId", sessionID).
				Str("signal", sig.String()).
				Str("hw_backend", string(hwBackend)).
				Msg("ffmpeg terminated on a fault signal")
			metrics.RecordFFmpegCrash(sig.String(), string(hwBackend))
		} else {
			a.Logger.Debug().Err(procErr).Str("sessionId", sessionID).Msg("ffmpeg process exited")
		}
		return
	}
	if resultErr != nil {
		return
	}
	a.clearProcessDetail(handle)
}

// startTimeoutForSpec is how long the startup watchdog gives the media process
// to produce its first progress: the profile's own allowance, clamped so it
// always lands before the caller stops waiting.
//
// The clamp is the whole point. The profile allowance reaches 60s for an HQ50
// CPU transcode and is capped at 120s, while a live caller stops waiting after
// its startup budget — 45s by default. Unclamped, the watchdog could only ever
// fire after the session had already been failed by someone else, which made it
// unreachable code for exactly the sessions that needed it. What is lost with it
// is the diagnosis: the watchdog reports DetailTranscodeStalled, which the
// recovery policy classifies as recoverable by a lighter profile, whereas the
// caller's own timeout can only report that no playlist appeared.
func (a *LocalAdapter) startTimeoutForSpec(spec ports.StreamSpec) time.Duration {
	timeout := a.startTimeoutForProfile(spec.Source.Type, spec.Profile)
	if timeout <= 0 || spec.ReadyDeadline.IsZero() {
		return timeout
	}
	clamped := time.Until(spec.ReadyDeadline) - watchdogStartLead
	// Never turn a configured timeout into a non-positive one: zero and below
	// mean "fire on the first tick" to the watchdog, which is a different
	// contract than "fire early".
	if clamped < minWatchdogStartTimeout {
		clamped = minWatchdogStartTimeout
	}
	if clamped < timeout {
		return clamped
	}
	return timeout
}

const (
	// watchdogStartLead is how far ahead of the caller's deadline the watchdog is
	// allowed to fire. It covers terminating the process group plus one of the
	// caller's ready-poll intervals, so the caller observes an unhealthy process
	// carrying a real diagnosis instead of hitting its own generic timeout first.
	watchdogStartLead = 2 * time.Second
	// minWatchdogStartTimeout keeps a clamped allowance positive.
	minWatchdogStartTimeout = 1 * time.Second
)

// startTimeoutForProfile is the profile's own startup allowance, before any
// caller deadline is taken into account.
func (a *LocalAdapter) startTimeoutForProfile(sourceType ports.SourceType, profile ports.ProfileSpec) time.Duration {
	timeout := a.StartTimeout
	if timeout <= 0 {
		return timeout
	}
	if sourceType == ports.SourceFile || !profile.TranscodeVideo {
		return timeout
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return timeout
	}

	overrideFloor := 30 * time.Second
	if profile.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		overrideFloor = 60 * time.Second
	} else {
		normalizedProfile := profiles.NormalizeRequestedProfileID(profile.Name)
		if normalizedProfile != profiles.ProfileSafari && normalizedProfile != profiles.ProfileSafariRuntimeHQ {
			return timeout
		}
	}

	override := a.Config.SafariCPUStartTimeoutOverride
	if override <= 0 {
		override = overrideFloor
	}
	if override < timeout {
		override = timeout
	}
	if override > 120*time.Second {
		override = 120 * time.Second
	}
	if override > timeout {
		return override
	}
	return timeout
}

func argsHardwareBackend(args []string) profiles.GPUBackend {
	for i := range args {
		if args[i] == "-vaapi_device" {
			return profiles.GPUBackendVAAPI
		}
		switch args[i] {
		case "h264_nvenc", "hevc_nvenc", "av1_nvenc":
			return profiles.GPUBackendNVENC
		}
	}
	return profiles.GPUBackendNone
}

func (a *LocalAdapter) writeFirstFrameMarker(sessionID string, dvrWindowSec int) {
	if !ports.IsSafeSessionID(sessionID) {
		return
	}
	markerPath := ports.SessionFirstFrameMarkerPathForPolicy(a.HLSRoot, sessionID, dvrWindowSec)
	if markerPath != "" {
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o750); err != nil {
			a.Logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to prep marker dir")
		} else {
			_ = os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600)
		}
	}

}

// Stop terminates the process.
func (a *LocalAdapter) Stop(ctx context.Context, handle ports.RunHandle) error {
	a.mu.Lock()
	cmd, exists := a.activeProcs[handle]
	sessionID := a.handleSessions[handle]
	if exists {
		a.removeActiveProcessLocked(handle, false)
	}
	a.mu.Unlock()

	if !exists {
		return nil // Idempotent
	}

	if cmd.Process != nil {
		a.terminateProcessGroup(cmd, sessionID)
		return nil
	}

	return nil
}

func (a *LocalAdapter) removeActiveProcessLocked(handle ports.RunHandle, archiveDetail bool) {
	delete(a.activeProcs, handle)
	delete(a.handleSessions, handle)
	delete(a.finalizedProfiles, handle)
	delete(a.executedPlans, handle)
	delete(a.runtimeDiagnostics, handle)
	if archiveDetail {
		a.archiveProcessDetailLocked(handle)
	}
	delete(a.processDetails, handle)
}

func (a *LocalAdapter) archiveProcessDetailLocked(handle ports.RunHandle) {
	detail := strings.TrimSpace(a.processDetails[handle])
	if detail == "" {
		return
	}
	if a.completedProcessDetails == nil {
		a.completedProcessDetails = make(map[ports.RunHandle]string)
	}
	if _, exists := a.completedProcessDetails[handle]; !exists {
		a.completedProcessDetailOrder = append(a.completedProcessDetailOrder, handle)
	}
	a.completedProcessDetails[handle] = detail
	for len(a.completedProcessDetailOrder) > maxCompletedProcessDetails {
		evict := a.completedProcessDetailOrder[0]
		a.completedProcessDetailOrder = a.completedProcessDetailOrder[1:]
		delete(a.completedProcessDetails, evict)
	}
}

func (a *LocalAdapter) terminateProcessGroup(cmd *exec.Cmd, sessionID string) {
	if cmd != nil {
		dc := a.GetDiagnosticContext(sessionID)
		a.Logger.Info().
			Str("session_id", dc.SessionID).
			Str("generation_id", dc.GenerationID).
			Str("reason", dc.Reason).
			Int64("elapsed_since_stop_ms", dc.ElapsedSinceStopMs).
			Msg("ffmpeg_termination_requested")
	}
	err := a.killProcessGroup(cmd)
	if err != nil {
		a.Logger.Warn().Err(err).Str("sessionId", sessionID).Msg("failed to terminate ffmpeg process group")
	}
}

func (a *LocalAdapter) killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Non-reaping group ladder: the monitor goroutine's cmd.Wait is the sole
	// reaper, so this must not call Wait or it would race the exit status
	// (corrupting exit classification, e.g. a spurious GPU->CPU demotion).
	return procgroup.KillGroupGraceful(cmd.Process.Pid, 2*time.Second, a.KillTimeout)
}

func (a *LocalAdapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	_, exists := a.activeProcs[handle]
	diagnostics := a.runtimeDiagnostics[handle]
	a.mu.Unlock()
	if !exists {
		// monitorProcess has finished — scanner drained, detail is final.
		return ports.HealthStatus{
			Healthy:     false,
			Message:     a.processStatusMessage(handle, "process not found"),
			LastCheck:   time.Now(),
			Diagnostics: diagnostics,
		}
	}

	return ports.HealthStatus{
		Healthy:     true,
		Message:     "process active",
		LastCheck:   time.Now(),
		Diagnostics: diagnostics,
	}
}

func (a *LocalAdapter) recordProcessDetail(handle ports.RunHandle, detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.processDetails[handle]
	if processDetailPriority(detail) >= processDetailPriority(current) {
		a.processDetails[handle] = detail
	}
}

func (a *LocalAdapter) clearProcessDetail(handle ports.RunHandle) {
	a.mu.Lock()
	delete(a.processDetails, handle)
	a.mu.Unlock()
}

func (a *LocalAdapter) processStatusMessage(handle ports.RunHandle, fallback string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if detail := strings.TrimSpace(a.processDetails[handle]); detail != "" {
		delete(a.processDetails, handle)
		return detail
	}
	if detail := strings.TrimSpace(a.completedProcessDetails[handle]); detail != "" {
		delete(a.completedProcessDetails, handle)
		return detail
	}
	return fallback
}

// processDetailPriority ranks details via the shared taxonomy, so the adapter and
// the session domain cannot disagree about which failure outranks which.
func processDetailPriority(detail string) int {
	return ports.ClassifyProcessFailure(detail).Priority()
}
func transformArgsForTelemetryPipeMode(args []string) []string {
	stripValueFlag := map[string]bool{
		"-headers":                    true,
		"-user_agent":                 true,
		"-protocol_whitelist":         true,
		"-reconnect":                  true,
		"-reconnect_at_eof":           true,
		"-reconnect_streamed":         true,
		"-reconnect_delay_max":        true,
		"-reconnect_on_network_error": true,
		"-reconnect_on_http_error":    true,
	}
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "-i" && i+1 < len(args) {
			out = append(out, "-i", "pipe:0")
			i++ // skip the URL value
			continue
		}
		if stripValueFlag[tok] && i+1 < len(args) {
			i++ // skip the option value
			continue
		}
		out = append(out, tok)
	}
	return out
}
