import { useCallback, useRef } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import { postRecordingPlaybackInfo } from '../../../client-ts';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../../lib/httpProblem';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';
import type { VodStreamMode, PlaybackMachineEvent } from './playbackTypes';
import { debugError, debugLog } from '../../../utils/logging';
import { telemetry } from '../../../services/TelemetryService';
import { normalizePlaybackInfo } from '../contracts/normalizePlaybackInfo';
import {
  buildPlaybackProfileHeaders,
  gatherPlaybackClientContext,
  normalizePlaybackProfileSelection,
  resolvePlaybackProfileForPreflight,
  resolvePlaybackRequestProfile,
} from '../utils/playbackRequestProfile';
import {
  applyPlaybackNetworkProbe,
  measurePlaybackNetwork,
} from '../utils/playbackNetworkProbe';
import { resolvePlaybackLinkProfile } from '../utils/playbackLinkProfile';
import { extractPlaybackTrace, resolvePlaybackObservability } from './observabilityFormatters';
import {
  buildAuthDeniedFailure,
  buildBlockedContractFailure,
  buildContractConsumedTelemetry,
  buildLeaseBusyFailure,
  buildMissingOutputUrlFailure,
  buildRecordingGoneFailure,
  resolveResumeStateFromContract,
} from './startupHelpers';
import { buildContractState } from './contractErrors';
import type { AppError } from '../../../types/errors';
import type { PlaybackFailureReportOptions } from '../semantics/playbackFailureSemantics';

export interface UseVodPlaybackAdmissionProps {
  apiBase: string;
  explicitProfile: string;
  ensureSessionCookie: () => Promise<void>;
  allocatePlaybackEpoch: () => number;
  beginPlaybackAttempt: (
    epoch: number,
    mode: 'VOD' | 'LIVE',
    status: 'building' | 'starting',
    isInitial: boolean,
    isExplicit: boolean,
  ) => void;
  prepareForNextPlaybackAttempt: (keepNativeSession?: boolean) => Promise<void>;
  isLifecycleActive: (gen: number) => boolean;
  isStalePlaybackEpoch: (epoch: number) => boolean;
  lifecycleGenerationRef: MutableRefObject<number>;
  activeRecordingRef: MutableRefObject<string | null>;
  dismissedResumeRecordingIdRef: MutableRefObject<string | null>;
  isTeardownRef: MutableRefObject<boolean>;
  linkProfileRef: MutableRefObject<any>;
  automaticProfileMemoryRef: MutableRefObject<any>;
  vodFetchRef: MutableRefObject<AbortController | null>;
  vodRetryRef: MutableRefObject<number | null>;
  setActiveRecordingId: Dispatch<SetStateAction<string | null>>;
  setCapabilitySnapshot: Dispatch<SetStateAction<CapabilitySnapshot | null>>;
  setTraceId: Dispatch<SetStateAction<string>>;
  setPlaybackObservability: Dispatch<SetStateAction<any>>;
  setVodStreamMode: Dispatch<SetStateAction<VodStreamMode>>;
  setDurationSeconds: Dispatch<SetStateAction<number | null>>;
  setCanSeek: Dispatch<SetStateAction<boolean>>;
  setStartUnix: Dispatch<SetStateAction<number | null>>;
  setAnchorStartSec: Dispatch<SetStateAction<number>>;
  setResumeState: Dispatch<SetStateAction<any>>;
  setShowResumeOverlay: Dispatch<SetStateAction<boolean>>;
  clearPlayerError: () => void;
  dispatchPlayback: Dispatch<PlaybackMachineEvent>;
  reportPlaybackFailure: (error: AppError, options?: PlaybackFailureReportOptions) => void;
  playDirectMp4: (url: string) => void;
  playHls: (url: string, engine: 'native' | 'hlsjs') => void;
  setActiveHlsEngine: Dispatch<SetStateAction<'native' | 'hlsjs' | null>>;
  gatherPlaybackCapabilitiesForPlayer: (surface: 'live' | 'recording') => Promise<CapabilitySnapshot>;
  resolvePreferredHlsEngineForCapabilities: (caps: CapabilitySnapshot) => 'native' | 'hlsjs';
  recordContractAdvisories: (epoch: number, advisories: any[]) => void;
  normalizeRuntimePlaybackError: (val: unknown, fallback: string) => AppError;
  mergeSessionPlaybackTrace: (trace: any) => void;
  sleep: (ms: number, signal?: AbortSignal) => Promise<void>;
  t: TFunction;
}

export interface UseVodPlaybackAdmissionResult {
  startRecordingPlayback: (id: string, profileOverride?: string, startOffsetMs?: number) => Promise<void>;
}

export function useVodPlaybackAdmission({
  apiBase,
  explicitProfile,
  ensureSessionCookie,
  allocatePlaybackEpoch,
  beginPlaybackAttempt,
  prepareForNextPlaybackAttempt,
  isLifecycleActive,
  isStalePlaybackEpoch,
  lifecycleGenerationRef,
  activeRecordingRef,
  dismissedResumeRecordingIdRef,
  isTeardownRef,
  linkProfileRef,
  automaticProfileMemoryRef,
  vodFetchRef,
  vodRetryRef,
  setActiveRecordingId,
  setCapabilitySnapshot,
  setTraceId,
  setPlaybackObservability,
  setVodStreamMode,
  setDurationSeconds,
  setCanSeek,
  setStartUnix,
  setAnchorStartSec,
  setResumeState,
  setShowResumeOverlay,
  clearPlayerError,
  dispatchPlayback,
  reportPlaybackFailure,
  playDirectMp4,
  playHls,
  setActiveHlsEngine,
  gatherPlaybackCapabilitiesForPlayer,
  resolvePreferredHlsEngineForCapabilities,
  recordContractAdvisories,
  normalizeRuntimePlaybackError,
  mergeSessionPlaybackTrace,
  sleep,
  t,
}: UseVodPlaybackAdmissionProps): UseVodPlaybackAdmissionResult {
  const startRecordingPlaybackRef = useRef<((id: string, profileOverride?: string, startOffsetMs?: number) => Promise<void>) | null>(null);

  const startRecordingPlayback = useCallback(async (
    id: string,
    profileOverride?: string,
    startOffsetMs?: number,
  ): Promise<void> => {
    const lifecycleGeneration = lifecycleGenerationRef.current;
    if (!isLifecycleActive(lifecycleGeneration)) return;
    const profileForAttempt = normalizePlaybackProfileSelection(profileOverride ?? explicitProfile);
    const playbackEpoch = allocatePlaybackEpoch();
    await prepareForNextPlaybackAttempt();
    if (!isLifecycleActive(lifecycleGeneration)) return;
    beginPlaybackAttempt(playbackEpoch, 'VOD', 'building', true, profileForAttempt !== 'auto');
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    clearPlayerError();

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null;

    try {
      await ensureSessionCookie();
      if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

      let streamUrl = '';
      let mode: VodStreamMode = null;

      try {
        const maxMetaRetries = 20;
        const [capabilities, networkProbe] = await Promise.all([
          gatherPlaybackCapabilitiesForPlayer('recording'),
          measurePlaybackNetwork(apiBase),
        ]);
        requestCaps = capabilities;
        const requestContext = applyPlaybackNetworkProbe(
          requestCaps,
          gatherPlaybackClientContext(),
          networkProbe,
        );
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        const automaticRequestProfile = resolvePlaybackRequestProfile(
          requestContext,
          requestCaps,
          'recording',
          automaticProfileMemoryRef.current,
        );
        const requestProfile = resolvePlaybackProfileForPreflight(
          profileForAttempt,
          automaticRequestProfile,
        );
        linkProfileRef.current = resolvePlaybackLinkProfile({
          requestProfile,
          probeKind: networkProbe?.kind,
          network: requestContext.network,
        });
        setCapabilitySnapshot(requestCaps);
        let rawContract: unknown = null;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            query: startOffsetMs && startOffsetMs > 0 ? { start_ms: startOffsetMs } : undefined,
            body: requestCaps,
            headers: buildPlaybackProfileHeaders(requestProfile),
          });

          if (error) {
            if (!response) {
              throw new Error(JSON.stringify(error));
            }
            if (notifyAuthRequiredIfUnauthorizedResponse(response, 'V3Player.recordingPlaybackInfo')) {
              const failure = buildAuthDeniedFailure(t, 401);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 403) {
              const failure = buildAuthDeniedFailure(t, 403);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 410) {
              const failure = buildRecordingGoneFailure(t);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 409) {
              const retryAfterHeader = response.headers.get('Retry-After');
              const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
              const failure = buildLeaseBusyFailure(retryAfter, t);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                dispatchPlayback({
                  type: 'normative.session.phase.changed',
                  playbackEpoch,
                  sessionEpoch: 0,
                  phase: 'building',
                });
                recordContractAdvisories(playbackEpoch, [{
                  code: 'recording_retry_after',
                  message: `${t('player.preparing')} (${seconds}s)`,
                  source: 'backend',
                }]);
                await sleep(seconds * 1000);
                continue;
              } else {
                throw new Error('503 Service Unavailable (No Retry-After)');
              }
            }
            throw new Error(JSON.stringify(error));
          }

          if (data) {
            rawContract = data;
            break;
          }
        }

        if (!rawContract) {
          throw new Error("PlaybackInfo timeout");
        }
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

        const preferredHlsEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        const normalizedContract = normalizePlaybackInfo(rawContract, {
          surface: 'recording',
          preferredHlsEngine,
        });

        debugLog('[V3Player] Normalized recording contract:', normalizedContract);
        recordContractAdvisories(playbackEpoch, normalizedContract.advisory.warnings);

        telemetry.emit('ui.contract.consumed', buildContractConsumedTelemetry(normalizedContract, 'recording'));

        if (normalizedContract.observability.requestId) {
          setTraceId(normalizedContract.observability.requestId);
        }
        setPlaybackObservability(resolvePlaybackObservability(
          normalizedContract.observability.decision,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (normalizedContract.kind === 'blocked') {
          const failure = buildBlockedContractFailure(normalizedContract, 'recording', t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        mode = normalizedContract.playback.mode;
        streamUrl = normalizedContract.playback.outputUrl ?? '';
        if (!streamUrl) {
          const failure = buildMissingOutputUrlFailure(t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        dispatchPlayback({
          type: 'normative.playback.contract.resolved',
          epoch: playbackEpoch,
          contract: buildContractState('recording', normalizedContract, streamUrl),
        });

        if (streamUrl.startsWith('/')) {
          streamUrl = `${window.location.origin}${streamUrl}`;
        }

        // Add Cache Busting to prevent sticky 503s
        streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;

        setVodStreamMode(mode);

        const playbackDurationSeconds = normalizedContract.media.durationSeconds;
        if (playbackDurationSeconds && playbackDurationSeconds > 0) {
          setDurationSeconds(playbackDurationSeconds);
        }

        setCanSeek(normalizedContract.playback.seekable);
        if (normalizedContract.media.startUnix) setStartUnix(normalizedContract.media.startUnix);
        setAnchorStartSec(normalizedContract.media.anchorStartSec ?? (startOffsetMs ? startOffsetMs / 1000 : 0));

        const nextResume = resolveResumeStateFromContract(normalizedContract, playbackDurationSeconds);
        if (
          startOffsetMs === undefined &&
          nextResume &&
          dismissedResumeRecordingIdRef.current !== id
        ) {
          setResumeState(nextResume);
          setShowResumeOverlay(true);
          dispatchPlayback({
            type: 'normative.playback.stopped',
            epoch: playbackEpoch,
          });
          return;
        }
      } catch (e: unknown) {
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        mergeSessionPlaybackTrace(extractPlaybackTrace(e));
        reportPlaybackFailure(normalizeRuntimePlaybackError(e, t('player.serverError')), {
          source: 'backend',
        });
        return;
      }

      // --- EXECUTION PATHS ---
      if (mode === 'direct_mp4') {
        // Direct MP4 start stays thin-client: the media element is the source of
        // truth for playability, so we do not gate startup on browser-side probes.
        isTeardownRef.current = false;
        if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        setActiveHlsEngine(null);
        playDirectMp4(streamUrl);
        return;
      }

      if (mode === 'native_hls' || mode === 'hlsjs' || mode === 'transcode') {
        const controller = new AbortController();
        abortController = controller;
        vodFetchRef.current = controller;
        try {
          const res = await fetch(streamUrl, {
            method: 'HEAD',
            signal: controller.signal
          });

          if (res.status === 404) {
            throw new Error(t('player.recordingNotFound'));
          }

          if (res.status === 503) {
            const retryAfter = res.headers.get('Retry-After');
            if (retryAfter) {
              const delay = parseInt(retryAfter, 10) * 1000;
              dispatchPlayback({
                type: 'normative.session.phase.changed',
                playbackEpoch,
                sessionEpoch: 0,
                phase: 'building',
              });
              vodRetryRef.current = window.setTimeout(() => {
                if (isLifecycleActive(lifecycleGeneration) && activeRecordingRef.current === id) {
                  void startRecordingPlaybackRef.current?.(id, profileForAttempt, startOffsetMs);
                }
              }, delay);
              return;
            }
            throw new Error('503 Service Unavailable (No Retry-After)');
          }

          if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
          const engine: 'native' | 'hlsjs' = mode === 'native_hls'
            ? 'native'
            : resolvePreferredHlsEngineForCapabilities(requestCaps);
          playHls(streamUrl, engine);
          setActiveHlsEngine(engine);
        } finally {
          if (vodFetchRef.current === controller) vodFetchRef.current = null;
        }
      }
    } catch (err: unknown) {
      if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
      debugError(err);
      mergeSessionPlaybackTrace(extractPlaybackTrace(err));
      reportPlaybackFailure(normalizeRuntimePlaybackError(err, t('player.serverError')), {
        source: 'backend',
      });
    } finally {
      if (vodFetchRef.current === abortController) vodFetchRef.current = null;
    }
  }, [
    activeRecordingRef,
    allocatePlaybackEpoch,
    apiBase,
    automaticProfileMemoryRef,
    beginPlaybackAttempt,
    clearPlayerError,
    dismissedResumeRecordingIdRef,
    dispatchPlayback,
    ensureSessionCookie,
    explicitProfile,
    gatherPlaybackCapabilitiesForPlayer,
    isLifecycleActive,
    isStalePlaybackEpoch,
    isTeardownRef,
    lifecycleGenerationRef,
    linkProfileRef,
    mergeSessionPlaybackTrace,
    normalizeRuntimePlaybackError,
    playDirectMp4,
    playHls,
    prepareForNextPlaybackAttempt,
    recordContractAdvisories,
    reportPlaybackFailure,
    resolvePreferredHlsEngineForCapabilities,
    setActiveHlsEngine,
    setActiveRecordingId,
    setAnchorStartSec,
    setCanSeek,
    setCapabilitySnapshot,
    setDurationSeconds,
    setPlaybackObservability,
    setResumeState,
    setShowResumeOverlay,
    setStartUnix,
    setTraceId,
    setVodStreamMode,
    sleep,
    t,
    vodFetchRef,
    vodRetryRef,
  ]);

  startRecordingPlaybackRef.current = startRecordingPlayback;

  return { startRecordingPlayback };
}
