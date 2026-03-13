import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from 'hls.js';
import {
  postRecordingPlaybackInfo,
  type PlaybackInfo
} from '../client-ts';
import { getApiBaseUrl } from '../lib/clientWrapper';
import { telemetry } from '../services/TelemetryService';
import type {
  V3PlayerProps,
  PlayerStatus,
  V3SessionResponse,
  HlsInstanceRef,
  VideoElementRef
} from '../types/v3-player';
import { useLiveSessionController } from '../features/player/useLiveSessionController';
import { usePlaybackEngine } from '../features/player/usePlaybackEngine';
import { usePlayerChrome } from '../features/player/usePlayerChrome';
import { useResume } from '../features/resume/useResume';
import { ResumeState } from '../features/resume/api';
import { Button, Card, StatusChip } from './ui';
import { debugError, debugLog, debugWarn } from '../utils/logging';
import { detectPreferredCodecs } from '../utils/codecDetection';
import {
  PlayerError,
  readResponseBody,
  extractCapHashFromDecisionToken,
  canUseDesktopWebKitFullscreen,
  shouldForceNativeMobileHls,
  shouldPreferNativeWebKitHls
} from '../utils/playerHelpers';
import { normalizePlayerError } from '../lib/appErrors';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../lib/httpProblem';
import type { AppError } from '../types/errors';
import styles from './V3Player.module.css';

interface ApiErrorResponse {
  code?: string;
  message?: string;
  requestId?: string;
  details?: unknown;
}

function resolvePlaybackDurationSeconds(playbackInfo: PlaybackInfo): number | null {
  if (typeof playbackInfo.durationMs === 'number' && playbackInfo.durationMs > 0) {
    return playbackInfo.durationMs / 1000;
  }
  if (typeof playbackInfo.durationSeconds === 'number' && playbackInfo.durationSeconds > 0) {
    return playbackInfo.durationSeconds;
  }
  return null;
}

function V3Player(props: V3PlayerProps) {
  const { t } = useTranslation();
  const { token, autoStart, onClose, duration } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;

  const [sRef, setSRef] = useState<string>(
    (channel?.serviceRef || channel?.id || '').trim()
  );

  // Traceability State
  const [traceId, setTraceId] = useState<string>('-');

  const [status, setStatus] = useState<PlayerStatus>('idle');
  const [error, setError] = useState<AppError | null>(null);
  const [showErrorDetails, setShowErrorDetails] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const mounted = useRef<boolean>(false);
  const vodRetryRef = useRef<number | null>(null);
  const recordingTimeoutRef = useRef<number | null>(null);
  const vodFetchRef = useRef<AbortController | null>(null);
  const activeRecordingRef = useRef<string | null>(null);
  const [activeRecordingId, setActiveRecordingId] = useState<string | null>(null);
  const startIntentInFlight = useRef<boolean>(false);
  // ADR-00X: Profile-related refs removed (universal policy only)
  const isTeardownRef = useRef<boolean>(false);
  const userPauseIntentRef = useRef<boolean>(false);

  const [durationSeconds, setDurationSeconds] = useState<number | null>(
    duration && duration > 0 ? duration : null
  );
  const [playbackMode, setPlaybackMode] = useState<'LIVE' | 'VOD' | 'UNKNOWN'>('UNKNOWN');
  const [vodStreamMode, setVodStreamMode] = useState<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>(null);
  const [activeHlsEngine, setActiveHlsEngine] = useState<'native' | 'hlsjs' | null>(null);

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);

  const setPlayerError = useCallback((nextError: AppError | null) => {
    setError(nextError);
  }, []);

  const clearPlayerError = useCallback(() => {
    setError(null);
    setShowErrorDetails(false);
  }, []);

  const setLegacyError = useCallback<Dispatch<SetStateAction<string | null>>>((next) => {
    setError((current) => {
      const currentTitle = current?.title ?? null;
      const resolvedTitle = typeof next === 'function' ? next(currentTitle) : next;
      if (!resolvedTitle) {
        return null;
      }
      return {
        title: resolvedTitle,
        detail: current?.detail,
        status: current?.status,
        retryable: current?.retryable ?? true,
      };
    });
  }, []);

  const setLegacyErrorDetails = useCallback<Dispatch<SetStateAction<string | null>>>((next) => {
    setError((current) => {
      if (!current) {
        return current;
      }
      const currentDetail = current.detail ?? null;
      const resolvedDetail = typeof next === 'function' ? next(currentDetail) : next;
      return {
        ...current,
        detail: resolvedDetail ?? undefined,
      };
    });
  }, []);

  useEffect(() => {
    if (!error?.detail) {
      setShowErrorDetails(false);
    }
  }, [error?.detail]);

  const sleep = useCallback((ms: number): Promise<void> => (
    new Promise(resolve => setTimeout(resolve, ms))
  ), []);

  const resolvePreferredHlsEngine = useCallback((): 'native' | 'hlsjs' => {
    const hlsJsSupported = Hls.isSupported();
    if (shouldPreferNativeWebKitHls(videoRef.current, hlsJsSupported)) {
      return 'native';
    }
    return hlsJsSupported ? 'hlsjs' : 'native';
  }, [videoRef]);

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);
  const requestedDuration = useMemo(() => (duration && duration > 0 ? duration : null), [duration]);

  const {
    sessionIdRef,
    authHeaders,
    reportError,
    ensureSessionCookie,
    setActiveSessionId,
    clearSessionLeaseState,
    sendStopIntent,
    waitForSessionReady
  } = useLiveSessionController({
    token,
    apiBase,
    t,
    videoRef,
    setPlaybackMode,
    setDurationSeconds,
    setStatus,
    setError: setLegacyError,
    readResponseBody,
    createPlayerError: (message, details) => new PlayerError(message, details)
  });

  const {
    showStats,
    seekableStart,
    seekableEnd,
    isPip,
    canTogglePiP,
    isFullscreen,
    canToggleFullscreen,
    isPlaying,
    isIdle,
    volume,
    isMuted,
    canAdjustVolume,
    stats,
    setStats,
    windowDuration,
    relativePosition,
    hasSeekWindow,
    isLiveMode,
    isAtLiveEdge,
    showDvrModeButton,
    startTimeDisplay,
    endTimeDisplay,
    formatClock,
    seekTo,
    seekBy,
    seekWhenReady,
    togglePlayPause,
    toggleFullscreen,
    enterDVRMode,
    togglePiP,
    toggleMute,
    handleVolumeChange,
    applyAutoplayMute,
    toggleStats,
    resetChromeState
  } = usePlayerChrome({
    autoStart,
    containerRef,
    videoRef,
    hlsRef,
    userPauseIntentRef,
    lastDecodedRef,
    playbackMode,
    durationSeconds,
    canSeek,
    startUnix,
    setStatus,
    allowNativeFullscreen: activeHlsEngine === 'native',
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen
  });

  // Resume Hook
  useResume({
    recordingId: activeRecordingId || undefined,
    duration: durationSeconds,
    videoRef,
    isPlaying,
    isSeekable: canSeek
  });

  const {
    resetPlaybackEngine,
    playHls,
    playDirectMp4,
    waitForDirectStream
  } = usePlaybackEngine({
    videoRef,
    hlsRef,
    sessionIdRef,
    isTeardownRef,
    lastDecodedRef,
    t,
    reportError,
    waitForSessionReady,
    shouldPreferNativeHls: shouldPreferNativeWebKitHls,
    setStats,
    setStatus,
    setError: setLegacyError,
    setErrorDetails: setLegacyErrorDetails,
    setShowErrorDetails
  });

  // --- Core Helpers & Wrappers (Memoized) ---

  const clearRecordingTimeout = useCallback(() => {
    if (recordingTimeoutRef.current !== null) {
      window.clearTimeout(recordingTimeoutRef.current);
      recordingTimeoutRef.current = null;
    }
  }, []);

  const clearVodRetry = useCallback(() => {
    if (vodRetryRef.current !== null) {
      window.clearTimeout(vodRetryRef.current);
      vodRetryRef.current = null;
    }
    clearRecordingTimeout();
  }, [clearRecordingTimeout]);

  const clearVodFetch = useCallback(() => {
    if (vodFetchRef.current) {
      vodFetchRef.current.abort();
      vodFetchRef.current = null;
    }
  }, []);

  const clearPlaybackSelection = useCallback(() => {
    activeRecordingRef.current = null;
    setActiveRecordingId(null);
    setVodStreamMode(null);
    setActiveHlsEngine(null);
  }, []);

  const clearPlaybackState = useCallback(() => {
    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
    clearSessionLeaseState();
    resetChromeState();
  }, [clearPlaybackSelection, clearSessionLeaseState, clearVodFetch, clearVodRetry, resetChromeState]);

  const hasActivePlayback = useCallback((): boolean => {
    const videoEl = videoRef.current;
    return Boolean(
      sessionIdRef.current ||
      activeRecordingRef.current ||
      hlsRef.current ||
      videoEl?.currentSrc ||
      videoEl?.getAttribute('src')
    );
  }, [hlsRef, sessionIdRef, videoRef]);

  const teardownActivePlayback = useCallback(async (): Promise<void> => {
    const activeSessionId = sessionIdRef.current;
    const hadActivePlayback = hasActivePlayback();

    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
    if (hadActivePlayback) {
      resetPlaybackEngine();
      await sleep(75);
    }
    if (activeSessionId) {
      await sendStopIntent(activeSessionId);
    }
    clearSessionLeaseState();
    resetChromeState();
  }, [
    clearPlaybackSelection,
    clearSessionLeaseState,
    clearVodFetch,
    clearVodRetry,
    hasActivePlayback,
    resetChromeState,
    resetPlaybackEngine,
    sendStopIntent,
    sessionIdRef,
    sleep,
  ]);

  const prepareFreshPlayback = useCallback((mode: 'LIVE' | 'VOD') => {
    setDurationSeconds(requestedDuration);
    setPlaybackMode(mode);
  }, [requestedDuration]);

  const gatherPlaybackCapabilities = useCallback(async () => {
    const video = videoRef.current as HTMLVideoElement | null;
    const preferredCodecs = await detectPreferredCodecs(video);
    const hlsJsSupported = Hls.isSupported();

    const supportsNativeHls = (() => {
      if (!video) return false;
      try {
        return video.canPlayType('application/vnd.apple.mpegurl') !== '';
      } catch {
        return false;
      }
    })();

    const supportsAc3 = (() => {
      if (!video) return false;
      try {
        return video.canPlayType('audio/mp4; codecs="ac-3"') !== '';
      } catch {
        return false;
      }
    })();

    const preferNativeHls = shouldPreferNativeWebKitHls(video as VideoElementRef, hlsJsSupported);

    const hlsEngines: Array<'native' | 'hlsjs'> = [];
    if (supportsNativeHls) {
      hlsEngines.push('native');
    }
    if (hlsJsSupported && !preferNativeHls) {
      hlsEngines.push('hlsjs');
    }

    const container = ['mp4', 'ts'];
    if (hlsJsSupported && !preferNativeHls) {
      container.push('fmp4');
    }

    const audioCodecs = ['aac', 'mp3'];
    if (supportsAc3) {
      audioCodecs.push('ac3');
    }

    return {
      capabilitiesVersion: 1,
      container: Array.from(new Set(container)),
      videoCodecs: Array.from(new Set(preferredCodecs)),
      audioCodecs: Array.from(new Set(audioCodecs)),
      supportsHls: hlsEngines.length > 0,
      supportsRange: true,
      allowTranscode: true,
      deviceType: 'web'
    };
  }, []);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    if (hasActivePlayback()) {
      await teardownActivePlayback();
    } else {
      clearPlaybackState();
    }
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    setStatus('building');
    clearPlayerError();
    setTraceId('-');
    setPlaybackMode('VOD');

    let abortController: AbortController | null = null;

    try {
      await ensureSessionCookie();

      // Determine Playback Mode from backend PlaybackInfo (single source of truth).
      let streamUrl = '';
      let mode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';

      try {
        const maxMetaRetries = 20;
        const requestCaps = await gatherPlaybackCapabilities();
        let pInfo: PlaybackInfo | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            body: requestCaps
          });

          if (error) {
            if (notifyAuthRequiredIfUnauthorizedResponse(response, 'V3Player.recordingPlaybackInfo')) {
              telemetry.emit('ui.error', { status: 401, code: 'AUTH_DENIED' });
              setStatus('error');
              setPlayerError({
                title: t('player.authFailed'),
                status: 401,
                retryable: false,
              });
              return;
            }
            if (response.status === 403) {
              telemetry.emit('ui.error', { status: response.status, code: 'AUTH_DENIED' });
              setStatus('error');
              setPlayerError({
                title: t('player.forbidden'),
                status: 403,
                retryable: false,
              });
              return;
            }
            if (response.status === 410) {
              telemetry.emit('ui.error', { status: 410, code: 'GONE' });
              throw new Error(t('player.notAvailable'));
            }
            if (response.status === 409) {
              const retryAfterHeader = response.headers.get('Retry-After');
              const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
              const retryHint = retryAfter > 0 ? ` ${t('player.retryAfter', { seconds: retryAfter })}` : '';
              telemetry.emit('ui.error', { status: 409, code: 'LEASE_BUSY', retry_after: retryAfter });
              setStatus('error');
              setPlayerError({
                title: `${t('player.leaseBusy')}${retryHint}`,
                status: 409,
                retryable: true,
              });
              return;
            }
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                telemetry.emit('ui.error', { status: 503, code: 'UNAVAILABLE', retry_after: seconds });
                setStatus('building');
                setLegacyErrorDetails(`${t('player.preparing')} (${seconds}s)`);
                await sleep(seconds * 1000);
                continue;
              } else {
                throw new Error('503 Service Unavailable (No Retry-After)');
              }
            }
            throw new Error(JSON.stringify(error));
          }

          if (data) {
            pInfo = data;
            break;
          }
        }

        if (!pInfo) {
          throw new Error("PlaybackInfo timeout");
        }

        debugLog('[V3Player] Playback Info:', pInfo);

        if (!pInfo.mode) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.missing',
            reason: 'MODE_MISSING'
          });
          throw new Error('Backend decision missing mode');
        }
        // Map backend 'hls' to local preferred HLS engine if needed
        const rawMode = pInfo.mode;
        if (rawMode === 'hls') {
          mode = resolvePreferredHlsEngine() === 'native' ? 'native_hls' : 'hlsjs';
        } else {
          mode = rawMode as any; // fall back to other modes or deny
        }

        if (!['native_hls', 'hlsjs', 'direct_mp4', 'transcode', 'deny'].includes(mode)) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.invalid',
            reason: String(mode)
          });
          throw new Error(`Unsupported backend playback mode: ${mode}`);
        }
        if (mode === 'deny') {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.deny',
            reason: pInfo.decisionReason || pInfo.decision?.reasons?.[0] || 'unknown'
          });
          throw new Error(t('player.playbackDenied'));
        }
        if (!pInfo.decision?.selectedOutputUrl) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.decision.selectionMissing',
            reason: 'DECISION_SELECTION_MISSING'
          });
          throw new Error('Backend decision missing selectedOutputUrl');
        }

        streamUrl = pInfo.decision.selectedOutputUrl;

        telemetry.emit('ui.contract.consumed', {
          mode: 'backend',
          fields: ['mode', 'decision.selectedOutputUrl']
        });

        if (streamUrl.startsWith('/')) {
          streamUrl = `${window.location.origin}${streamUrl}`;
        }

        // Add Cache Busting to prevent sticky 503s
        streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;

        setVodStreamMode(mode as any);

        // Truth Consumption
        const playbackDurationSeconds = resolvePlaybackDurationSeconds(pInfo);
        if (playbackDurationSeconds && playbackDurationSeconds > 0) {
          setDurationSeconds(playbackDurationSeconds);
        }

        if (pInfo.requestId) setTraceId(pInfo.requestId);
        const recordingIsSeekable = pInfo.isSeekable !== undefined ? Boolean(pInfo.isSeekable) : true;
        setCanSeek(recordingIsSeekable);
        if (pInfo.startUnix) setStartUnix(pInfo.startUnix);

        // Resume State
        if (recordingIsSeekable && pInfo.resume && pInfo.resume.posSeconds >= 15 && (!pInfo.resume.finished)) {
          const d = pInfo.resume.durationSeconds || (playbackDurationSeconds || 0);
          if (!d || pInfo.resume.posSeconds < d - 10) {
            setResumeState({
              posSeconds: pInfo.resume.posSeconds,
              durationSeconds: pInfo.resume.durationSeconds || undefined,
              finished: pInfo.resume.finished || undefined
            });
            setShowResumeOverlay(true);
          }
        }
      } catch (e: unknown) {
        if (activeRecordingRef.current !== id) return;
        setStatus('error');
        setPlayerError(normalizePlayerError(e, {
          fallbackTitle: t('player.serverError'),
        }));
        return;
      }

      // --- EXECUTION PATHS ---
      if (mode === 'direct_mp4') {
        try {
          isTeardownRef.current = false;
          await waitForDirectStream(streamUrl);
          if (activeRecordingRef.current !== id) return;
          setStatus('buffering');
          setActiveHlsEngine(null);
          playDirectMp4(streamUrl);
          return;
        } catch (err) {
          if (activeRecordingRef.current !== id) return;
          setStatus('error');
          setPlayerError({
            title: t('player.timeout'),
            retryable: true,
          });
          return;
        }
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
              setStatus('building');
              vodRetryRef.current = window.setTimeout(() => {
                if (activeRecordingRef.current === id) startRecordingPlayback(id);
              }, delay);
              return;
            }
            throw new Error('503 Service Unavailable (No Retry-After)');
          }

          if (activeRecordingRef.current !== id) return;
          setStatus('buffering');
          const engine: 'native' | 'hlsjs' = mode === 'native_hls' ? 'native' : resolvePreferredHlsEngine();
          playHls(streamUrl, engine);
          setActiveHlsEngine(engine);
        } finally {
          if (vodFetchRef.current === controller) vodFetchRef.current = null;
        }
      }
    } catch (err: unknown) {
      if (activeRecordingRef.current !== id) return;
      debugError(err);
      setPlayerError(normalizePlayerError(err, {
        fallbackTitle: t('player.serverError'),
      }));
      setStatus('error');
    } finally {
      if (vodFetchRef.current === abortController) vodFetchRef.current = null;
    }
  }, [apiBase, authHeaders, clearPlaybackState, clearPlayerError, ensureSessionCookie, gatherPlaybackCapabilities, hasActivePlayback, playDirectMp4, playHls, resolvePreferredHlsEngine, setLegacyErrorDetails, setPlayerError, sleep, t, teardownActivePlayback, waitForDirectStream]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    if (startIntentInFlight.current) return;
    startIntentInFlight.current = true;
    userPauseIntentRef.current = false;
    applyAutoplayMute();

    try {
      if (recordingId) {
        debugLog('[V3Player] startStream: recordingId path', { recordingId, hasSrc: !!src });
        if (src) {
          debugWarn('[V3Player] Both recordingId and src provided; prioritizing recordingId (VOD path).');
        }
        await startRecordingPlayback(recordingId);
        return;
      }

      if (src) {
        debugLog('[V3Player] startStream: src path', { hasSrc: true });
        if (hasActivePlayback()) {
          await teardownActivePlayback();
        } else {
          clearPlaybackState();
        }
        prepareFreshPlayback(requestedDuration ? 'VOD' : 'LIVE');
        setStatus('buffering');
        setTraceId('-');
        const srcEngine = resolvePreferredHlsEngine();
        playHls(src, srcEngine);
        setActiveHlsEngine(srcEngine);
        return;
      }

      const ref = (refToUse || sRef || '').trim();
      if (!ref) {
        setStatus('error');
        setPlayerError({
          title: t('player.serviceRefRequired'),
          retryable: false,
        });
        return;
      }
      if (hasActivePlayback()) {
        await teardownActivePlayback();
      } else {
        clearPlaybackState();
      }
      prepareFreshPlayback('LIVE');
      let newSessionId: string | null = null;
      setStatus('starting');
      clearPlayerError();
      setTraceId('-');

      try {
        await ensureSessionCookie();

        // SSOT: live playback mode is decided by backend from measured capabilities.
        let liveMode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';
        let liveEngine: 'native' | 'hlsjs' = 'hlsjs';
        const preferredHlsEngine = resolvePreferredHlsEngine();

        const requestCaps = await gatherPlaybackCapabilities();
        // raw-fetch-justified: live decision request posts dynamic capability payload not covered by generated wrapper flow.
        const liveResponse = await fetch(`${apiBase}/live/stream-info`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            serviceRef: ref,
            capabilities: requestCaps
          })
        });
        const { json: liveInfoJson } = await readResponseBody(liveResponse);
        const liveInfo = liveInfoJson as PlaybackInfo;
        const liveError = (!liveResponse.ok) ? liveInfoJson as any : null;
        const liveRequestId =
          (typeof liveInfo?.requestId === 'string' ? liveInfo.requestId : undefined) ||
          liveResponse.headers.get('X-Request-ID') ||
          undefined;
        if (liveRequestId) {
          setTraceId(liveRequestId);
        }

        if (!liveResponse.ok) {
          if (notifyAuthRequiredIfUnauthorizedResponse(liveResponse, 'V3Player.liveStreamInfo')) {
            setStatus('error');
            setPlayerError(normalizePlayerError(liveError ?? {
              status: 401,
              title: t('player.authFailed'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.authFailed'),
              status: 401,
              retryable: false,
            }));
            return;
          }
          if (liveResponse.status === 403) {
            setStatus('error');
            setPlayerError(normalizePlayerError(liveError ?? {
              status: 403,
              title: t('player.forbidden'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.forbidden'),
              status: 403,
              retryable: false,
            }));
            return;
          }
          throw normalizePlayerError(liveError ?? {
            status: liveResponse.status,
            title: `${t('player.apiError')}: ${liveResponse.status}`,
            requestId: liveRequestId,
          }, {
            fallbackTitle: `${t('player.apiError')}: ${liveResponse.status}`,
            status: liveResponse.status,
          });
        }

        if (!liveInfo?.mode) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.missing',
            reason: 'MODE_MISSING'
          });
          throw new Error('Backend live decision missing mode');
        }

        const beMode = String(liveInfo.mode);
        switch (beMode) {
          case 'native_hls':
            liveMode = 'native_hls';
            liveEngine = 'native';
            break;
          case 'hlsjs':
          case 'hls':
          case 'direct_stream':
            liveEngine = preferredHlsEngine;
            liveMode = liveEngine === 'native' ? 'native_hls' : 'hlsjs';
            break;
          case 'transcode':
            liveMode = 'transcode';
            liveEngine = preferredHlsEngine;
            break;
          case 'direct_mp4':
            liveMode = 'direct_mp4';
            break;
          case 'deny':
            liveMode = 'deny';
            break;
          default:
            telemetry.emit('ui.failclosed', {
              context: 'V3Player.live.mode.invalid',
              reason: beMode || String(liveMode)
            });
            throw new Error(`Unsupported backend live playback mode: ${beMode}`);
        }

        if (!liveInfo.requestId) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.request_id.missing',
            reason: 'REQUEST_ID_MISSING'
          });
          throw new Error('Backend live decision missing requestId');
        }
        setTraceId(liveInfo.requestId);

        if (liveMode === 'deny') {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.deny',
            reason: liveInfo.reason || liveInfo.decision?.reasons?.[0] || 'unknown'
          });
          setStatus('error');
          setPlayerError({
            title: t('player.playbackDenied'),
            retryable: false,
          });
          return;
        }

        const liveDecisionToken = liveInfo.playbackDecisionToken;
        if (!liveDecisionToken) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.playback_decision_token.missing',
            reason: 'PLAYBACK_DECISION_TOKEN_MISSING'
          });
          throw new Error('Backend live decision missing playbackDecisionToken');
        }

        if (liveMode === 'native_hls') {
          liveEngine = 'native';
        } else if (liveMode === 'hlsjs') {
          liveEngine = 'hlsjs';
        } else if (liveMode === 'transcode') {
          liveEngine = resolvePreferredHlsEngine();
        } else {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.unsupported',
            reason: liveMode
          });
          throw new Error(`Unsupported live playback mode: ${liveMode}`);
        }

        telemetry.emit('ui.contract.consumed', {
          mode: 'backend',
          fields: ['mode']
        });

        const intentParams: Record<string, string> = {
          playback_mode: liveMode,
          // Keep canonical contract key for downstream compatibility checks.
          playback_decision_token: liveDecisionToken
        };
        const capHash = extractCapHashFromDecisionToken(liveDecisionToken);
        if (capHash) {
          intentParams.capHash = capHash;
        }

        // raw-fetch-justified: stream.start intent needs explicit payload shaping and immediate RFC7807 handling.
        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            type: 'stream.start',
            serviceRef: ref,
            playbackDecisionToken: liveDecisionToken,
            ...(Object.keys(intentParams).length > 0 ? { params: intentParams } : {})
          })
        });

        if (res.status === 401 || res.status === 403) {
          const isUnauthorized = notifyAuthRequiredIfUnauthorizedResponse(res, 'V3Player.startIntent');
          let errorTitle = isUnauthorized ? t('player.authFailed') : t('player.forbidden');
          let problemBody: unknown = null;
          try {
            const ct = res.headers.get('content-type') || '';
            if (ct.includes('application/problem+json') || ct.includes('application/json')) {
              const problem = await res.json();
              if (problem.title) errorTitle = problem.title;
              problemBody = problem;

              telemetry.emit('ui.auth_error', {
                status: res.status,
                code: problem.code || null,
                title: problem.title || null,
                detail: problem.detail || null
              });
            }
          } catch {
            // Body parse failed – fall through with generic message
          }
          setStatus('error');
          setPlayerError(normalizePlayerError(problemBody ?? {
            status: res.status,
            title: errorTitle,
          }, {
            fallbackTitle: errorTitle,
            status: res.status,
            retryable: false,
          }));
          return;
        }

        if (res.status === 409) {
          const retryAfterHeader = res.headers.get('Retry-After');
          const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
          const retryHint = retryAfter > 0 ? ` ${t('player.retryAfter', { seconds: retryAfter })}` : '';
          let apiErr: ApiErrorResponse | null = null;
          try {
            apiErr = await res.json();
          } catch {
            apiErr = null;
          }
          setStatus('error');
          setPlayerError(normalizePlayerError(apiErr ?? {
            status: 409,
            title: `${t('player.leaseBusy')}${retryHint}`,
          }, {
            fallbackTitle: `${t('player.leaseBusy')}${retryHint}`,
            status: 409,
            retryable: true,
          }));
          return;
        }
        if (!res.ok) {
          let errorMsg = `${t('player.apiError')}: ${res.status}`;
          let errorPayload: unknown = null;
          let errorDetails: string | null = null;
          try {
            const { json, text } = await readResponseBody(res);
            const responseRequestId =
              (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
              res.headers.get('X-Request-ID') ||
              undefined;

            if (json && typeof json === 'object') {
              const title = typeof json.title === 'string' && json.title ? json.title : null;
              const message = typeof json.message === 'string' && json.message ? json.message : null;
              if (title) {
                errorMsg = title;
              } else if (message) {
                errorMsg = message;
              }

              const detailParts: string[] = [];
              if (typeof json.code === 'string' && json.code) detailParts.push(`code=${json.code}`);
              if (typeof json.detail === 'string' && json.detail) detailParts.push(json.detail);
              if (json.details) {
                detailParts.push(typeof json.details === 'string' ? json.details : JSON.stringify(json.details));
              }
              if (responseRequestId) detailParts.push(`requestId=${responseRequestId}`);
              if (detailParts.length > 0) {
                errorDetails = detailParts.join(' · ');
              }
              errorPayload = {
                ...json,
                status: res.status,
                requestId: responseRequestId,
              };
            } else if (text) {
              errorDetails = text;
            }
          } catch (e) {
            debugWarn("Failed to parse error response", e);
          }
          throw normalizePlayerError(errorPayload ?? {
            status: res.status,
            title: errorMsg,
            details: errorDetails,
          }, {
            fallbackTitle: errorMsg,
            fallbackDetail: errorDetails ?? undefined,
            status: res.status,
          });
        }

        const data: V3SessionResponse = await res.json();
        newSessionId = data.sessionId;
        if (data.requestId) setTraceId(data.requestId);
        setActiveSessionId(newSessionId);
        const session = await waitForSessionReady(newSessionId);

        setStatus('ready');
        const streamUrl = session.playbackUrl;
        if (!streamUrl) {
          throw new Error(t('player.streamUrlMissing'));
        }
        playHls(streamUrl, liveEngine);
        setActiveHlsEngine(liveEngine);

      } catch (err) {
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
        clearSessionLeaseState();
        debugError(err);
        setPlayerError(normalizePlayerError(err, {
          fallbackTitle: t('player.serverError'),
        }));
        setStatus('error');
      }
    } finally {
      startIntentInFlight.current = false;
    }
  }, [src, recordingId, sRef, apiBase, authHeaders, clearPlaybackState, clearPlayerError, ensureSessionCookie, waitForSessionReady, hasActivePlayback, playHls, sendStopIntent, clearSessionLeaseState, t, startRecordingPlayback, applyAutoplayMute, gatherPlaybackCapabilities, resolvePreferredHlsEngine, setActiveSessionId, setPlayerError, prepareFreshPlayback, requestedDuration, teardownActivePlayback]);

  const stopStream = useCallback(async (skipClose: boolean = false): Promise<void> => {
    userPauseIntentRef.current = true;
    await teardownActivePlayback();
    setPlaybackMode('UNKNOWN');
    setStatus('stopped');
    setTraceId('-');
    if (onClose && !skipClose) onClose();
  }, [onClose, setPlaybackMode, teardownActivePlayback]);

  const handleRetry = useCallback(async () => {
    try {
      await stopStream();
    } finally {
      startIntentInFlight.current = false;
      void startStream();
    }
  }, [stopStream, startStream]);
  // --- Effects ---
  // Update sRef on channel change
  useEffect(() => {
    if (channel) {
      const ref = (channel.serviceRef || channel.id || '').trim();
      if (ref) setSRef(ref);
    }
  }, [channel]);

  useEffect(() => {
    if (!autoStart || mounted.current) return;
    // UI-INV-PLAYER-001: Autostart requires an explicit source.
    const normalizedRef = sRef.trim();
    const hasSource = !!(src || recordingId || normalizedRef);
    if (hasSource) {
      mounted.current = true;
      startStream(normalizedRef || undefined);
    }
  }, [autoStart, src, recordingId, sRef, startStream]);

  useEffect(() => {
    if (requestedDuration) {
      setDurationSeconds(requestedDuration);
    }
  }, [requestedDuration]);

  // Cleanup effect
  useEffect(() => {
    const videoEl = videoRef.current;
    return () => {
      if (hlsRef.current) hlsRef.current.destroy();
      if (videoEl) {
        videoEl.pause();
        videoEl.src = '';
      }
      clearVodRetry();
      clearVodFetch();
      clearPlaybackSelection();
      sendStopIntent(sessionIdRef.current, true);
    };
  }, [clearPlaybackSelection, clearVodFetch, clearVodRetry, sendStopIntent]);

  // Overlay styles
  // ADR-00X: Overlay styles are controlled via styles.overlay in V3Player.module.css
  // Static layout styles are in V3Player.module.css (scoped)

  const spinnerLabel =
    status === 'starting' || status === 'priming' || status === 'buffering' || status === 'building'
      ? (status === 'buffering' && playbackMode === 'VOD' && activeRecordingRef.current && vodStreamMode === 'direct_mp4')
        ? t('player.preparingDirectPlay') // Show explicit preparing for VOD buffering
        : `${t(`player.statusStates.${status}`, { defaultValue: status })}…`
      : '';

  return (
    <div
      ref={containerRef}
      className={[
        styles.container,
        'animate-enter',
        onClose ? styles.overlay : null,
        isIdle ? styles.userIdle : null,
      ].filter(Boolean).join(' ')}
    >
      {onClose && (
        <button
          onClick={() => void stopStream()}
          className={styles.closeButton}
          aria-label={t('player.closePlayer')}
        >
          ✕
        </button>
      )}

      {/* Stats Overlay */}
      {showStats && (
        <div className={styles.statsOverlay}>
          <Card variant="standard">
            <Card.Header>
              <Card.Title>{t('player.statsTitle', { defaultValue: 'Technical Stats' })}</Card.Title>
            </Card.Header>
            <Card.Content className={styles.statsGrid}>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.status')}</span>
                <StatusChip
                  state={status === 'ready' ? 'live' : status === 'error' ? 'error' : 'idle'}
                  label={t(`player.statusStates.${status}`, { defaultValue: status })}
                />
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('common.session', { defaultValue: 'Session' })}</span>
                <span className={styles.statsValue}>{sessionIdRef.current || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('common.requestId', { defaultValue: 'Request ID' })}</span>
                <span className={styles.statsValue}>{traceId}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.resolution')}</span>
                <span className={styles.statsValue}>{stats.resolution}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.bandwidth')}</span>
                <span className={styles.statsValue}>{stats.bandwidth > 0 ? `${stats.bandwidth} kbps` : '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.bufferHealth')}</span>
                <span className={styles.statsValue}>{stats.bufferHealth}s</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.latency')}</span>
                <span className={styles.statsValue}>{stats.latency !== null ? stats.latency + 's' : '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.fps')}</span>
                <span className={styles.statsValue}>{stats.fps}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.dropped')}</span>
                <span className={styles.statsValue}>{stats.droppedFrames}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.hlsLevel')}</span>
                <span className={styles.statsValue}>{
                  hlsRef.current ? (stats.levelIndex === -1 ? 'Auto' : stats.levelIndex) : 'Native / Direct'
                }</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.segDuration')}</span>
                <span className={styles.statsValue}>{stats.buffer > 0 ? `${stats.buffer}s` : '-'}</span>
              </div>
            </Card.Content>
          </Card>
        </div>
      )}

      <div className={styles.videoWrapper}>
        {channel && <h3 className={styles.overlayTitle}>{channel.name}</h3>}

        {/* PREPARING Overlay (VOD Remux) */}
        {(status === 'starting' || status === 'priming' || status === 'buffering' || status === 'building') && (
          <div className={styles.spinnerOverlay} ref={() => debugLog('[V3Player] Spinner Rendered', { status, fullscreen: isFullscreen })}>
            <div className={`${styles.spinner} spinner-base`}></div>
            <div className={styles.spinnerLabel}>{spinnerLabel}</div>
          </div>
        )}

        <video
          ref={videoRef}
          controls={false}
          playsInline
          webkit-playsinline=""
          preload="metadata"
          autoPlay={!!autoStart}
          className={styles.videoElement}
        />
      </div>

      {/* Error Toast */}
      {error && (
        <div className={styles.errorToast} aria-live="polite" role="alert">
          <div className={styles.errorMain}>
            <span className={styles.errorText}>⚠ {error.title}</span>
            {error.retryable ? (
              <Button variant="secondary" size="sm" onClick={handleRetry}>{t('common.retry')}</Button>
            ) : null}
          </div>
          {error.detail && (
            <button
              onClick={() => setShowErrorDetails(!showErrorDetails)}
              className={styles.errorDetailsButton}
            >
              {showErrorDetails ? t('common.hideDetails') : t('common.showDetails')}
            </button>
          )}
          {showErrorDetails && error.detail && (
            <div className={styles.errorDetailsContent}>
              <pre className={styles.errorDetailsPre}>{error.detail}</pre>
              <br />
              {t('common.session')}: {sessionIdRef.current || t('common.notAvailable')}
            </div>
          )}
        </div>
      )}

      {/* Controls & Status Bar */}
      <div className={styles.controlsHeader}>
        {hasSeekWindow ? (
          <div className={[styles.vodControls, styles.seekControls].join(' ')}>
            <div className={styles.seekButtons}>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-900)} title={t('player.seekBack15m')} aria-label={t('player.seekBack15m')}>
                ↺ 15m
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-60)} title={t('player.seekBack60s')} aria-label={t('player.seekBack60s')}>
                ↺ 60s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-15)} title={t('player.seekBack15s')} aria-label={t('player.seekBack15s')}>
                ↺ 15s
              </Button>
            </div>

            <Button
              variant="primary"
              size="icon"
              className={styles.playPauseButton}
              onClick={togglePlayPause}
              title={isPlaying ? t('player.pause') : t('player.play')}
              aria-label={isPlaying ? t('player.pause') : t('player.play')}
            >
              {isPlaying ? '⏸' : '▶'}
            </Button>

            <div className={styles.seekSliderGroup}>
              <span className={styles.vodTime}>{startTimeDisplay}</span>
              <input
                type="range"
                min="0"
                max={windowDuration}
                step="0.1"
                className={styles.vodSlider}
                value={relativePosition}
                onChange={(e) => {
                  const newVal = parseFloat(e.target.value);
                  seekTo(seekableStart + newVal);
                }}
              />
              <span className={styles.vodTimeTotal}>{endTimeDisplay}</span>
            </div>

            <div className={styles.seekButtons}>
              <Button variant="ghost" size="sm" onClick={() => seekBy(15)} title={t('player.seekForward15s')} aria-label={t('player.seekForward15s')}>
                +15s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(60)} title={t('player.seekForward60s')} aria-label={t('player.seekForward60s')}>
                +60s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(900)} title={t('player.seekForward15m')} aria-label={t('player.seekForward15m')}>
                +15m
              </Button>
            </div>

            {isLiveMode && (
              <button
                className={[styles.liveButton, isAtLiveEdge ? styles.liveButtonActive : null].filter(Boolean).join(' ')}
                onClick={() => seekTo(seekableEnd)}
                title={t('player.goLive')}
              >
                LIVE
              </button>
            )}
          </div>
        ) : (
          !channel && !recordingId && !src && (
            <input
              type="text"
              className={styles.serviceInput}
              value={sRef}
              onChange={(e) => setSRef(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  const nextRef = e.currentTarget.value;
                  void startStream(nextRef);
                }
              }}
            />
          )
        )}

        {/* ADR-00X: Profile dropdown removed (universal policy only) */}

        {!autoStart && !src && !recordingId && (
          <Button
            onClick={() => startStream()}
            disabled={status === 'starting' || status === 'priming'}
          >
            ▶ {t('common.startStream')}
          </Button>
        )}

        {/* DVR Mode Button (Safari Only / Fallback) */}
        {showDvrModeButton && !canToggleFullscreen && (
          <Button onClick={enterDVRMode} title={t('player.dvrMode')}>
            📺 DVR
          </Button>
        )}

        <div className={styles.utilityControls}>
          {canToggleFullscreen && (
            <Button
              variant="ghost"
              size="sm"
              active={isFullscreen}
              onClick={() => void toggleFullscreen()}
              title={isFullscreen
                ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
            >
              ⛶ {isFullscreen
                ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
            </Button>
          )}

          {canAdjustVolume && (
            <div className={styles.volumeControl}>
              <button
                className={styles.volumeButton}
                onClick={toggleMute}
                title={isMuted ? t('player.unmute') : t('player.mute')}
              >
                {isMuted ? '🔇' : volume > 0.5 ? '🔊' : volume > 0 ? '🔉' : '🔈'}
              </button>
              <input
                type="range"
                min="0"
                max="1"
                step="0.05"
                className={styles.volumeSlider}
                value={isMuted ? 0 : volume}
                onChange={(e) => handleVolumeChange(parseFloat(e.target.value))}
              />
            </div>
          )}

          {canTogglePiP && (
            <Button
              variant="ghost"
              size="sm"
              active={isPip}
              onClick={() => void togglePiP()}
              title={t('player.pipTitle')}
            >
              📺 {t('player.pipLabel')}
            </Button>
          )}

          <Button
            variant="ghost"
            size="sm"
            active={showStats}
            onClick={toggleStats}
            title={t('player.statsTitle')}
          >
            📊 {t('player.statsLabel')}
          </Button>

          {!onClose && (
            <Button variant="danger" onClick={() => void stopStream()}>
              ⏹ {t('common.stop')}
            </Button>
          )}
        </div>
      </div>
      {/* Resume Overlay */}
      {showResumeOverlay && resumeState && (
        <div className={styles.resumeOverlay}>
          <div className={styles.resumeContent}>
            <h3>{t('player.resumeTitle')}</h3>
            <p>{t('player.resumePrompt', { time: formatClock(resumeState.posSeconds) })}</p>
            <div className={styles.resumeActions}>
              <Button
                onClick={() => {
                  seekWhenReady(resumeState.posSeconds);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.resumeAction')}
              </Button>
              <Button
                variant="secondary"
                onClick={() => {
                  seekWhenReady(0);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.startOver')}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default V3Player;
// cspell:ignore remux arrowleft arrowright enterpictureinpicture leavepictureinpicture kbps Remux
