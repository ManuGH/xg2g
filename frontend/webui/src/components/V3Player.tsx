import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from 'hls.js';
import { postRecordingPlaybackInfo } from '../client-ts';
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
import styles from './V3Player.module.css';

interface ApiErrorResponse {
  code?: string;
  message?: string;
  requestId?: string;
  details?: unknown;
}

type PreferredCodec = 'av1' | 'hevc' | 'h264';

let cachedPreferredCodecs: PreferredCodec[] | null = null;

function hasTouchInput(): boolean {
  try {
    return typeof navigator !== 'undefined' && Number(navigator.maxTouchPoints || 0) > 0;
  } catch {
    return false;
  }
}

function canUseDesktopWebKitFullscreen(videoEl?: VideoElementRef): boolean {
  if (!videoEl) return false;
  try {
    const webkitVideo = videoEl as unknown as {
      webkitEnterFullscreen?: unknown;
    };
    return typeof webkitVideo.webkitEnterFullscreen === 'function' && !hasTouchInput();
  } catch {
    return false;
  }
}

function shouldForceNativeMobileHls(videoEl?: VideoElementRef): boolean {
  if (!videoEl) return false;
  try {
    const hasNativeHls = videoEl.canPlayType('application/vnd.apple.mpegurl') !== '';
    if (!hasNativeHls) return false;

    // Feature detection for mobile WebKit controls (no UA sniffing).
    const webkitVideo = videoEl as unknown as {
      webkitEnterFullscreen?: unknown;
      webkitSupportsPresentationMode?: unknown;
      webkitSetPresentationMode?: unknown;
    };
    const hasWebKitFullscreen = typeof webkitVideo.webkitEnterFullscreen === 'function';
    const hasWebKitPresentationMode =
      typeof webkitVideo.webkitSupportsPresentationMode === 'function' ||
      typeof webkitVideo.webkitSetPresentationMode === 'function';

    // Desktop Safari can expose some WebKit fullscreen APIs.
    // Restrict native-mobile forcing to touch devices to keep desktop behavior stable.
    return (hasWebKitFullscreen || hasWebKitPresentationMode) && hasTouchInput();
  } catch {
    return false;
  }
}

async function detectPreferredCodecs(videoEl?: HTMLVideoElement | null): Promise<PreferredCodec[]> {
  if (cachedPreferredCodecs) return cachedPreferredCodecs;

  const supports = async (contentType: string): Promise<boolean> => {
    try {
      const mc = (navigator as any)?.mediaCapabilities;
      if (mc?.decodingInfo) {
        const baseVideo = {
          contentType,
          width: 1920,
          height: 1080,
          bitrate: 5_000_000,
          framerate: 30
        };

        try {
          const info = await mc.decodingInfo({ type: 'media-source', video: baseVideo });
          if (info?.supported) return true;
        } catch {
          // Some platforms only accept type='file'; try fallback below.
        }

        try {
          const info = await mc.decodingInfo({ type: 'file', video: baseVideo });
          if (info?.supported) return true;
        } catch {
          // ignore
        }
      }
    } catch {
      // ignore
    }

    try {
      if (typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(contentType)) return true;
    } catch {
      // ignore
    }

    try {
      const v = videoEl || (typeof document !== 'undefined' ? document.createElement('video') : null);
      if (v && v.canPlayType(contentType) !== '') return true;
    } catch {
      // ignore
    }

    return false;
  };

  const supportsAny = async (contentTypes: string[]): Promise<boolean> => {
    const results = await Promise.all(contentTypes.map((ct) => supports(ct)));
    return results.some(Boolean);
  };

  const av1Types = ['video/mp4; codecs="av01.0.05M.08"'];
  const hevcTypes = [
    'video/mp4; codecs="hvc1.1.6.L120.90"',
    'video/mp4; codecs="hev1.1.6.L120.90"'
  ];
  const h264Types = ['video/mp4; codecs="avc1.42E01E"'];

  const out: PreferredCodec[] = [];

  if (await supportsAny(av1Types)) out.push('av1');
  if (await supportsAny(hevcTypes)) out.push('hevc');

  // Always include H.264 as a safe fallback.
  // If the platform surprisingly doesn't report support, keep it anyway: server will still fall back if needed.
  if (out.length === 0) {
    // Still probe H.264 once, but don't block the fallback list on it.
    await supportsAny(h264Types);
  }
  out.push('h264');

  cachedPreferredCodecs = out;
  return out;
}

class PlayerError extends Error {
  details?: unknown;

  constructor(message: string, details?: unknown) {
    super(message);
    this.name = 'PlayerError';
    this.details = details;
    Object.setPrototypeOf(this, PlayerError.prototype);
  }
}

async function readResponseBody(res: Response): Promise<{ json: any | null; text: string | null }> {
  const maybeRes = res as unknown as { text?: () => Promise<string>; json?: () => Promise<any> };
  try {
    if (typeof maybeRes.text === 'function') {
      const text = await maybeRes.text();
      if (!text) return { json: null, text: '' };
      try {
        return { json: JSON.parse(text), text };
      } catch {
        return { json: null, text };
      }
    }
  } catch {
    // fall through to json fallback
  }

  try {
    if (typeof maybeRes.json === 'function') {
      const json = await maybeRes.json();
      return {
        json,
        text: json == null ? '' : JSON.stringify(json)
      };
    }
  } catch {
    // ignore and fall through
  }
  return { json: null, text: null };
}

function extractCapHashFromDecisionToken(token: string): string | null {
  try {
    const parts = token.split('.');
    if (parts.length < 2) return null;
    const payloadB64 = parts[1];
    if (!payloadB64) return null;
    const normalized = payloadB64.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
    const payload = JSON.parse(atob(padded));
    if (payload && typeof payload.capHash === 'string' && payload.capHash) {
      return payload.capHash;
    }
  } catch {
    // Decision token parsing is best-effort; server enforces token validity.
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
  const [error, setError] = useState<string | null>(null);
  const [errorDetails, setErrorDetails] = useState<string | null>(null);
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

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);
  const [] = useState<number | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);

  const sleep = (ms: number): Promise<void> =>
    new Promise(resolve => setTimeout(resolve, ms));

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);

  const {
    sessionId,
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
    setError,
    readResponseBody,
    createPlayerError: (message, details) => new PlayerError(message, details)
  });

  const {
    showStats,
    seekableStart,
    seekableEnd,
    isPip,
    isFullscreen,
    isPlaying,
    isIdle,
    volume,
    isMuted,
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
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen
  });

  // Resume Hook
  useResume({
    recordingId: activeRecordingId || undefined,
    duration: durationSeconds,
    videoElement: videoRef.current,
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
    shouldForceNativeMobileHls,
    setStats,
    setStatus,
    setError,
    setErrorDetails,
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

  const gatherPlaybackCapabilities = useCallback(async () => {
    const video = videoRef.current as HTMLVideoElement | null;
    const preferredCodecs = await detectPreferredCodecs(video);

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

    const forceNativeMobile = shouldForceNativeMobileHls(video as VideoElementRef);

    const hlsEngines: Array<'native' | 'hlsjs'> = [];
    if (supportsNativeHls) {
      hlsEngines.push('native');
    }
    if (Hls.isSupported() && !forceNativeMobile) {
      hlsEngines.push('hlsjs');
    }

    const container = ['mp4', 'ts'];
    if (Hls.isSupported() && !forceNativeMobile) {
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
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    setVodStreamMode(null);
    clearVodRetry();
    clearVodFetch();
    clearSessionLeaseState();
    resetPlaybackEngine();
    setStatus('building');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);
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
        let pInfo: any | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            body: requestCaps
          });

          if (error) {
            if (response.status === 401 || response.status === 403) {
              telemetry.emit('ui.error', { status: response.status, code: 'AUTH_DENIED' });
              setStatus('error');
              setError(t('player.authFailed'));
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
              setError(`${t('player.leaseBusy')}${retryHint}`);
              return;
            }
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                telemetry.emit('ui.error', { status: 503, code: 'UNAVAILABLE', retry_after: seconds });
                setStatus('building');
                setErrorDetails(`${t('player.preparing')} (${seconds}s)`);
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
        mode = pInfo.mode;

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
            reason: pInfo.reason || pInfo.decision?.reasons?.[0] || 'unknown'
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
        const playbackDurationSeconds =
          typeof pInfo.durationSeconds === 'number' && pInfo.durationSeconds > 0
            ? pInfo.durationSeconds
            : typeof pInfo.durationMs === 'number' && pInfo.durationMs > 0
              ? pInfo.durationMs / 1000
              : null;
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
      } catch (e: any) {
        if (activeRecordingRef.current !== id) return;
        setStatus('error');
        setError(e.message || t('player.serverError'));
        return;
      }

      // --- EXECUTION PATHS ---
      if (mode === 'direct_mp4') {
        try {
          isTeardownRef.current = false;
          await waitForDirectStream(streamUrl);
          if (activeRecordingRef.current !== id) return;
          setStatus('buffering');
          playDirectMp4(streamUrl);
          return;
        } catch (err) {
          if (activeRecordingRef.current !== id) return;
          setStatus('error');
          setError(t('player.timeout'));
          return;
        }
      }

      if (mode === 'native_hls' || mode === 'hlsjs' || mode === 'transcode') {
        const forceNativeMobile = shouldForceNativeMobileHls(videoRef.current);
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
          const engine: 'native' | 'hlsjs' = mode === 'native_hls' || forceNativeMobile ? 'native' : 'hlsjs';
          playHls(streamUrl, engine);
        } finally {
          if (vodFetchRef.current === controller) vodFetchRef.current = null;
        }
      }
    } catch (err: any) {
      if (activeRecordingRef.current !== id) return;
      debugError(err);
      setError(err.message);
      setStatus('error');
    } finally {
      if (vodFetchRef.current === abortController) vodFetchRef.current = null;
    }
  }, [apiBase, authHeaders, clearSessionLeaseState, clearVodFetch, clearVodRetry, playDirectMp4, playHls, resetPlaybackEngine, t, waitForDirectStream, ensureSessionCookie, gatherPlaybackCapabilities]);

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
        // Reset state for local/src playback
        activeRecordingRef.current = null;
        setActiveRecordingId(null);
        setVodStreamMode(null);
        clearVodRetry();
        clearVodFetch();
        clearSessionLeaseState();
        setDurationSeconds(duration && duration > 0 ? duration : null);

        setPlaybackMode(duration && duration > 0 ? 'VOD' : 'LIVE');
        setStatus('buffering');
        playHls(src);
        return;
      }

      // Reset state for new live session
      activeRecordingRef.current = null;
      setActiveRecordingId(null);
      setVodStreamMode(null);
      clearVodRetry();
      clearVodFetch();
      clearSessionLeaseState();
      setDurationSeconds(duration && duration > 0 ? duration : null);

      const ref = (refToUse || sRef || '').trim();
      if (!ref) {
        setStatus('error');
        setError(t('player.serviceRefRequired'));
        setErrorDetails(null);
        setShowErrorDetails(false);
        return;
      }
      let newSessionId: string | null = null;
      setStatus('starting');
      setError(null);
      setErrorDetails(null);
      setShowErrorDetails(false);
      setPlaybackMode('LIVE');

      try {
        await ensureSessionCookie();

        // SSOT: live playback mode is decided by backend from measured capabilities.
        let liveMode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';
        let liveEngine: 'native' | 'hlsjs' = 'hlsjs';
        const forceNativeMobile = shouldForceNativeMobileHls(videoRef.current);

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
        const liveInfo = liveInfoJson as any;
        const liveRequestId =
          (typeof liveInfo?.requestId === 'string' ? liveInfo.requestId : undefined) ||
          liveResponse.headers.get('X-Request-ID') ||
          undefined;
        if (liveRequestId) {
          setTraceId(liveRequestId);
        }

        if (!liveResponse.ok) {
          if (liveResponse.status === 401 || liveResponse.status === 403) {
            setStatus('error');
            setError(t('player.authFailed'));
            return;
          }
          const code = typeof liveInfo?.code === 'string' && liveInfo.code ? liveInfo.code : null;
          const title = typeof liveInfo?.title === 'string' && liveInfo.title ? liveInfo.title : null;
          const type = typeof liveInfo?.type === 'string' && liveInfo.type ? liveInfo.type : null;

          const summaryParts = [code, title, type].filter((part): part is string => Boolean(part));
          const message = summaryParts.length > 0
            ? summaryParts.join(' · ')
            : `${t('player.apiError')}: ${liveResponse.status}`;

          const detailParts: string[] = [];
          if (typeof liveInfo?.detail === 'string' && liveInfo.detail) detailParts.push(liveInfo.detail);
          if (liveRequestId) detailParts.push(`requestId=${liveRequestId}`);

          const err = new Error(message);
          if (detailParts.length > 0) {
            (err as any).details = detailParts.join(' · ');
          }
          throw err;
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
            if (forceNativeMobile) {
              liveEngine = 'native';
              liveMode = 'native_hls';
            } else if (Hls.isSupported()) {
              liveEngine = 'hlsjs';
              liveMode = 'hlsjs';
            } else {
              liveEngine = 'native';
              liveMode = 'native_hls';
            }
            break;
          case 'transcode':
            liveMode = 'transcode';
            liveEngine = forceNativeMobile ? 'native' : (Hls.isSupported() ? 'hlsjs' : 'native');
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
          setError(t('player.playbackDenied'));
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
        } else if (liveMode === 'transcode' && forceNativeMobile) {
          liveEngine = 'native';
        } else if (liveMode === 'hlsjs' || liveMode === 'transcode') {
          liveEngine = 'hlsjs';
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
          // [RFC7807] Extract structured problem+json for actionable UI messages
          let errorTitle = res.status === 401 ? t('player.authFailed') : t('player.forbidden');
          let errorDetail: string | null = null;
          try {
            const ct = res.headers.get('content-type') || '';
            if (ct.includes('application/problem+json') || ct.includes('application/json')) {
              const problem = await res.json();
              // RFC7807: title is the human-readable summary, code is machine-readable
              if (problem.title) errorTitle = problem.title;
              const detailParts: string[] = [];
              if (problem.code) detailParts.push(`code=${problem.code}`);
              if (problem.detail) detailParts.push(problem.detail);
              if (problem.requestId) detailParts.push(`requestId=${problem.requestId}`);
              if (detailParts.length > 0) errorDetail = detailParts.join(' · ');

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
          setError(errorTitle);
          setErrorDetails(errorDetail);
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
          setError(`${t('player.leaseBusy')}${retryHint}`);
          if (apiErr?.code || apiErr?.requestId) {
            const parts = [];
            if (apiErr.code) parts.push(`code=${apiErr.code}`);
            if (apiErr.requestId) parts.push(`requestId=${apiErr.requestId}`);
            setErrorDetails(parts.join(' '));
          } else {
            setErrorDetails(null);
          }
          return;
        }
        if (!res.ok) {
          let errorMsg = `${t('player.apiError')}: ${res.status}`;
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
            } else if (text) {
              errorDetails = text;
            }
          } catch (e) {
            debugWarn("Failed to parse error response", e);
          }
          const err = new Error(errorMsg);
          (err as any).details = errorDetails || undefined;
          throw err;
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

      } catch (err) {
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
        clearSessionLeaseState();
        debugError(err);
        const e = err as any;
        setError(e?.message || String(err));
        if (e?.details) {
          try {
            setErrorDetails(typeof e.details === 'string' ? e.details : JSON.stringify(e.details, null, 2));
          } catch {
            setErrorDetails(String(e.details));
          }
        } else {
          setErrorDetails(e?.stack || null);
        }
        setStatus('error');
      }
    } finally {
      startIntentInFlight.current = false;
    }
  }, [src, recordingId, sRef, apiBase, authHeaders, ensureSessionCookie, waitForSessionReady, playHls, sendStopIntent, clearSessionLeaseState, t, duration, startRecordingPlayback, applyAutoplayMute, gatherPlaybackCapabilities, setActiveSessionId]);

  const stopStream = useCallback(async (skipClose: boolean = false): Promise<void> => {
    userPauseIntentRef.current = true;
    if (hlsRef.current) hlsRef.current.destroy();
    if (videoRef.current) {
      videoRef.current.pause();
      videoRef.current.src = '';
    }
    clearVodRetry();
    clearVodFetch();
    activeRecordingRef.current = null;
    setActiveRecordingId(null);
    if (sessionId) {
      await sendStopIntent(sessionId);
    }
    clearSessionLeaseState();
    setPlaybackMode('UNKNOWN');
    resetChromeState();
    setStatus('stopped');
    setVodStreamMode(null);
    if (onClose && !skipClose) onClose();
  }, [clearSessionLeaseState, clearVodFetch, clearVodRetry, onClose, resetChromeState, sendStopIntent, sessionId]);

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
    if (duration && duration > 0) {
      setDurationSeconds(duration);
    }
  }, [duration]);

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
      activeRecordingRef.current = null;
      sendStopIntent(sessionIdRef.current, true);
    };
  }, [clearVodFetch, clearVodRetry, sendStopIntent]);

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
            <span className={styles.errorText}>⚠ {error}</span>
            <Button variant="secondary" size="sm" onClick={handleRetry}>{t('common.retry')}</Button>
          </div>
          {errorDetails && (
            <button
              onClick={() => setShowErrorDetails(!showErrorDetails)}
              className={styles.errorDetailsButton}
            >
              {showErrorDetails ? t('common.hideDetails') : t('common.showDetails')}
            </button>
          )}
          {showErrorDetails && errorDetails && (
            <div className={styles.errorDetailsContent}>
              <pre className={styles.errorDetailsPre}>{errorDetails}</pre>
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
        {showDvrModeButton && (
          <Button onClick={enterDVRMode} title={t('player.dvrMode')}>
            📺 DVR
          </Button>
        )}

        {/* Volume Control */}
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

        <Button
          variant="ghost"
          size="sm"
          active={isPip}
          onClick={togglePiP}
          title={t('player.pipTitle')}
        >
          📺 {t('player.pipLabel')}
        </Button>

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
