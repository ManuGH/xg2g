import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from 'hls.js';
import type { ErrorData, FragLoadedData, ManifestParsedData, LevelLoadedData } from 'hls.js';
import { createSession, getRecordingPlaybackInfo, type PlaybackInfo } from '../client-ts';
import { getApiBaseUrl, setClientAuthToken } from '../client-ts/wrapper';
import { telemetry } from '../services/TelemetryService';
import { resolvePlaybackInfoPolicy } from '../contracts/PolicyEngine';
import { useCapabilities } from '../hooks/useCapabilities';
import type { NormativePlaybackInfo } from '../contracts/consumption';
import type {
  V3PlayerProps,
  PlayerStatus,
  SessionCookieState,
  V3SessionResponse,
  V3SessionStatusResponse,
  HlsInstanceRef,
  VideoElementRef
} from '../types/v3-player';
import { useResume } from '../features/resume/useResume';
import { ResumeState } from '../features/resume/api';
import { Button, Card, StatusChip } from './ui';
import { debugError, debugLog, debugWarn } from '../utils/logging';
import styles from './V3Player.module.css';

interface PlayerStats {
  bandwidth: number;
  resolution: string;
  fps: number;
  droppedFrames: number;
  buffer: number; // Segment duration
  bufferHealth: number; // Buffer ahead in seconds
  latency: number | null; // Live latency
  levelIndex: number;
}

interface ApiErrorResponse {
  code?: string;
  message?: string;
  requestId?: string;
  details?: unknown;
}

type PreferredCodec = 'av1' | 'hevc' | 'h264';

let cachedPreferredCodecs: PreferredCodec[] | null = null;

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
  try {
    const text = await res.text();
    if (!text) return { json: null, text: '' };
    try {
      return { json: JSON.parse(text), text };
    } catch {
      return { json: null, text };
    }
  } catch {
    return { json: null, text: null };
  }
}

function V3Player(props: V3PlayerProps) {
  const { t } = useTranslation();
  const { capabilities } = useCapabilities();
  const { token, autoStart, onClose, duration } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;

  const [sRef, setSRef] = useState<string>(
    channel?.serviceRef || channel?.id || ''
  );

  // Traceability State
  const [traceId, setTraceId] = useState<string>('-');

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [status, setStatus] = useState<PlayerStatus>('idle');
  const [error, setError] = useState<string | null>(null);
  const [errorDetails, setErrorDetails] = useState<string | null>(null);
  const [showErrorDetails, setShowErrorDetails] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const mounted = useRef<boolean>(false);
  const sessionIdRef = useRef<string | null>(null);
  const stopSentRef = useRef<string | null>(null);
  const sessionCookieRef = useRef<SessionCookieState>({ token: null, pending: null });
  const vodRetryRef = useRef<number | null>(null);
  const recordingTimeoutRef = useRef<number | null>(null);
  const vodFetchRef = useRef<AbortController | null>(null);
  const activeRecordingRef = useRef<string | null>(null);
  const [activeRecordingId, setActiveRecordingId] = useState<string | null>(null);
  const startIntentInFlight = useRef<boolean>(false);
  // ADR-00X: Profile-related refs removed (universal policy only)
  const isTeardownRef = useRef<boolean>(false);

  // UX Features State
  const [showStats, setShowStats] = useState(false);
  const [currentPlaybackTime, setCurrentPlaybackTime] = useState(0); // Local tracking for UI updates
  const [seekableStart, setSeekableStart] = useState(0);
  const [seekableEnd, setSeekableEnd] = useState(0);
  const [durationSeconds, setDurationSeconds] = useState<number | null>(
    duration && duration > 0 ? duration : null
  );
  const [playbackMode, setPlaybackMode] = useState<'LIVE' | 'VOD' | 'UNKNOWN'>('UNKNOWN');
  const [vodStreamMode, setVodStreamMode] = useState<'direct_mp4' | 'hls' | null>(null);
  const [isPip, setIsPip] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);
  const [] = useState<number | null>(null);
  // unused: liveEdgeUnix used for calculation but not directly in render yet, keeping state for completeness
  const isSafari = useMemo(() => {
    if (typeof navigator === 'undefined') return false;
    const ua = navigator.userAgent.toLowerCase();
    return ua.includes('safari') && !ua.includes('chrome') && !ua.includes('chromium') && !ua.includes('android');
  }, []);
  // ADR-00X: Profile selection removed (universal policy only)

  // ADR-009: Session Lease Semantics
  const [heartbeatInterval, setHeartbeatInterval] = useState<number | null>(null); // seconds from backend
  // @ts-expect-error - TS6133: leaseExpiresAt used via setter, not directly read
  const [leaseExpiresAt, setLeaseExpiresAt] = useState<string | null>(null); // ISO 8601

  // PREPARING state for VOD remux UX
  const [volume, setVolume] = useState(1); // 0.0 to 1.0
  const [isMuted, setIsMuted] = useState(false);
  const lastNonZeroVolumeRef = useRef<number>(1);
  const lastDecodedRef = useRef<number>(0);
  const [stats, setStats] = useState<PlayerStats>({
    bandwidth: 0,
    resolution: '-',
    fps: 0,
    droppedFrames: 0,
    buffer: 0,
    bufferHealth: 0,
    latency: null,
    levelIndex: -1
  });

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);

  // Resume Hook
  useResume({
    recordingId: activeRecordingId || undefined,
    duration: durationSeconds,
    videoElement: videoRef.current,
    isPlaying
  });

  const sleep = (ms: number): Promise<void> =>
    new Promise(resolve => setTimeout(resolve, ms));

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);


  const formatClock = useCallback((value: number): string => {
    if (!Number.isFinite(value) || value < 0) return '--:--';
    const totalSeconds = Math.floor(value);
    const h = Math.floor(totalSeconds / 3600);
    const m = Math.floor((totalSeconds % 3600) / 60);
    const s = totalSeconds % 60;
    const pad = (n: number) => n.toString().padStart(2, '0');
    return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
  }, []);

  const formatTimeOfDay = useCallback((unixSeconds: number): string => {
    if (!unixSeconds || unixSeconds <= 0) return '--:--:--';
    const date = new Date(unixSeconds * 1000);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  }, []);

  const refreshSeekableState = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    let start = 0;
    let end = 0;
    if (playbackMode === 'VOD' && durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    } else if (video.seekable && video.seekable.length > 0) {
      start = video.seekable.start(0);
      end = video.seekable.end(video.seekable.length - 1);
    } else if (Number.isFinite(video.duration) && video.duration > 0) {
      end = video.duration;
    } else if (durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    }
    setSeekableStart(start);
    setSeekableEnd(end);
    setCurrentPlaybackTime(video.currentTime);
  }, [durationSeconds, playbackMode]);

  const seekTo = useCallback((targetSeconds: number) => {
    const video = videoRef.current;
    if (!video || !Number.isFinite(targetSeconds)) return;
    let clamped = Math.max(0, targetSeconds);
    if (seekableEnd > seekableStart) {
      clamped = Math.min(Math.max(targetSeconds, seekableStart), seekableEnd);
    }
    video.currentTime = clamped;
  }, [seekableEnd, seekableStart]);

  const seekBy = useCallback((deltaSeconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekTo(video.currentTime + deltaSeconds);
  }, [seekTo]);

  const seekWhenReady = useCallback((target: number) => {
    const v = videoRef.current;
    if (!v) return;

    const doSeek = () => {
      seekTo(target);
      v.play().catch(e => debugWarn("Seek play failed", e));
    };

    if (v.readyState >= 1) { // HAVE_METADATA
      doSeek();
    } else {
      v.addEventListener('loadedmetadata', doSeek, { once: true });
    }
  }, [seekTo]);

  // --- UI Helpers (Memoized) ---
  const togglePlayPause = useCallback(() => {
    if (!videoRef.current) return;
    if (videoRef.current.paused) {
      videoRef.current.play().catch(e => debugWarn("Play failed", e));
    } else {
      videoRef.current.pause();
    }
  }, []);

  const toggleFullscreen = useCallback(async () => {
    // Safari DVR Mode (Native Fullscreen)
    const video = videoRef.current;
    if (isSafari && video?.webkitEnterFullscreen) {
      video.webkitEnterFullscreen();
      return;
    }

    if (!document.fullscreenElement) {
      try {
        // Use container fullscreen to preserve custom controls on Safari
        await containerRef.current?.requestFullscreen();
      } catch (err) {
        debugWarn("Fullscreen failed", err);
      }
    } else {
      await document.exitFullscreen();
    }
  }, [isSafari]);

  const enterDVRMode = useCallback(() => {
    const video = videoRef.current;
    if (video && video.webkitEnterFullscreen) {
      video.webkitEnterFullscreen();
    } else {
      // Fallback for non-Safari (just toggle FS)
      toggleFullscreen();
    }
  }, [toggleFullscreen]);

  const togglePiP = useCallback(async () => {
    if (!videoRef.current) return;
    try {
      if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
        setIsPip(false);
      } else {
        await videoRef.current.requestPictureInPicture();
        setIsPip(true);
      }
    } catch (err) {
      debugWarn("PiP failed", err);
    }
  }, []);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (!video.muted) {
      if (video.volume > 0) {
        lastNonZeroVolumeRef.current = video.volume;
      }
      video.muted = true;
      setIsMuted(true);
      return;
    }

    const restoreVolume = lastNonZeroVolumeRef.current > 0 ? lastNonZeroVolumeRef.current : video.volume;
    if (restoreVolume > 0 && video.volume !== restoreVolume) {
      video.volume = restoreVolume;
      setVolume(restoreVolume);
    }
    video.muted = false;
    setIsMuted(false);
  }, []);

  const handleVolumeChange = useCallback((newVolume: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.volume = newVolume;
    setVolume(newVolume);
    if (newVolume > 0) {
      lastNonZeroVolumeRef.current = newVolume;
    }
    const shouldMute = newVolume === 0;
    video.muted = shouldMute;
    setIsMuted(shouldMute);
  }, []);

  const applyAutoplayMute = useCallback(() => {
    if (!autoStart) return;
    const video = videoRef.current;
    if (!video) return;
    video.muted = true;
    setIsMuted(true);
  }, [autoStart]);

  // --- Core Helpers & Wrappers (Memoized) ---

  const authHeaders = useCallback((contentType: boolean = false): HeadersInit => {
    const h: Record<string, string> = {};
    if (contentType) h['Content-Type'] = 'application/json';
    if (token) h.Authorization = `Bearer ${token}`;
    return h;
  }, [token]);

  const reportError = useCallback(async (event: 'error' | 'warning', code: number, msg?: string) => {
    // ADR-00X: Auto-switch logic removed (universal policy only)

    if (!sessionIdRef.current) return;
    try {
      await fetch(`${apiBase}/sessions/${sessionIdRef.current}/feedback`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({
          event,
          code,
          message: msg
        })
      });
    } catch (e) {
      debugWarn('Failed to send feedback', e);
    }
  }, [apiBase, authHeaders]);

  const ensureSessionCookie = useCallback(async (): Promise<void> => {
    if (!token) return;
    if (sessionCookieRef.current.token === token) return;
    if (sessionCookieRef.current.pending) return sessionCookieRef.current.pending;

    const pending = (async () => {
      try {
        setClientAuthToken(token);
        await createSession();
        sessionCookieRef.current.token = token;
      } catch (err) {
        debugWarn('Failed to create session cookie', err);
      } finally {
        sessionCookieRef.current.pending = null;
      }
    })();

    sessionCookieRef.current.pending = pending;
    return pending;
  }, [token]);

  const resetPlaybackEngine = useCallback(() => {
    isTeardownRef.current = true;
    try {
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      if (videoRef.current) {
        videoRef.current.pause();
        videoRef.current.removeAttribute('src'); // Cleanest reset for Safari
        videoRef.current.load(); // Reset media element to initial state
      }
    } finally {
      // Small tick to ensure event loop cleanup clears any pending errors
      // User recommendation: 250-500ms window to catch trailing async errors
      setTimeout(() => { isTeardownRef.current = false; }, 50);
    }
  }, []);

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

  const sendStopIntent = useCallback(async (idToStop: string | null, force: boolean = false): Promise<void> => {
    if (!idToStop) return;
    if (!force && stopSentRef.current === idToStop) return;
    stopSentRef.current = idToStop;
    try {
      await fetch(`${apiBase}/intents`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({
          type: 'stream.stop',
          sessionId: idToStop
        })
      });
    } catch (err) {
      debugWarn('Failed to stop v3 session', err);
    }
  }, [apiBase, authHeaders]);

  const applySessionInfo = useCallback((session: V3SessionStatusResponse) => {
    if (session.mode) {
      setPlaybackMode(session.mode === 'LIVE' ? 'LIVE' : 'VOD');
    }
    if (typeof session.durationSeconds === 'number' && session.durationSeconds > 0) {
      setDurationSeconds(session.durationSeconds);
    }
    // ADR-009: Parse lease fields from session response
    if (typeof session.heartbeat_interval === 'number') {
      setHeartbeatInterval(session.heartbeat_interval);
    }
    if (session.lease_expires_at) {
      setLeaseExpiresAt(session.lease_expires_at);
    }
  }, []);

  const waitForSessionReady = useCallback(async (sid: string, maxAttempts = 180): Promise<V3SessionStatusResponse> => {
    for (let i = 0; i < maxAttempts; i++) {
      try {
        const res = await fetch(`${apiBase}/sessions/${sid}`, {
          headers: authHeaders()
        });

        // Handle Auth failure explicitly
        if (res.status === 401 || res.status === 403) {
          throw new PlayerError(t('player.authFailed'), {
            url: res.url,
            status: res.status,
            requestId: res.headers.get('X-Request-ID') || undefined
          });
        }

        if (res.status === 404) {
          await sleep(100); // Fast retry for session creation
          continue;
        }

        // CTO Contract (Phase 5.3): terminal sessions return 410 Gone with a problem+json body.
        if (res.status === 410) {
          const { json, text } = await readResponseBody(res);
          const requestId =
            (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
            res.headers.get('X-Request-ID') ||
            undefined;

          const reason = (json && typeof json === 'object' ? (json.reason ?? json.state ?? json.code) : undefined) as
            | string
            | undefined;
          const reasonDetail =
            (json && typeof json === 'object' ? (json.reason_detail ?? json.reasonDetail ?? json.detail) : undefined) as
            | string
            | undefined;

          const combined = `${reason ?? 'GONE'}${reasonDetail ? `: ${reasonDetail}` : ''}`;
          const details = {
            url: res.url,
            status: res.status,
            requestId,
            code: json?.code,
            title: json?.title,
            detail: json?.detail,
            session: json?.session,
            state: json?.state,
            reason: json?.reason,
            reason_detail: json?.reason_detail,
            body: json ?? text
          };

          telemetry.emit('ui.error', {
            status: 410,
            code: 'SESSION_GONE',
            reason: reason ?? null,
            reason_detail: reasonDetail ?? null,
            requestId
          });

          if (String(reason).includes('LEASE_BUSY') || String(reasonDetail).includes('LEASE_BUSY')) {
            throw new PlayerError(t('player.leaseBusy'), details);
          }
          throw new PlayerError(`${t('player.sessionFailed')}: ${combined}`, details);
        }

        if (!res.ok) {
          const { json, text } = await readResponseBody(res);
          const requestId =
            (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
            res.headers.get('X-Request-ID') ||
            undefined;
          throw new PlayerError(`${t('player.failedToFetchSession')} (HTTP ${res.status})`, {
            url: res.url,
            status: res.status,
            requestId,
            code: json?.code,
            title: json?.title,
            detail: json?.detail,
            body: json ?? text
          });
        }

        const session: V3SessionStatusResponse = await res.json();
        applySessionInfo(session);
        const sState = session.state;

        if (sState === 'FAILED' || sState === 'STOPPED' || sState === 'CANCELLED' || sState === 'STOPPING') {
          const reason = session.reason || sState;
          const detail = session.reasonDetail ? `: ${session.reasonDetail}` : '';

          // Lease busy -> show the dedicated message (same UX as 409 on /intents)
          if (String(reason).includes('LEASE_BUSY') || String(detail).includes('LEASE_BUSY')) {
            throw new Error(t('player.leaseBusy'));
          }

          throw new Error(`${t('player.sessionFailed')}: ${reason}${detail}`);
        }

        if (sState === 'READY' || sState === 'DRAINING') {
          setStatus('ready');
          return session;
        }

        if (sState === 'PRIMING') {
          setStatus('priming');
        } else {
          setStatus('starting');
        }

        await sleep(100); // Fast polling for low-latency startup
      } catch (err) {
        const e = err as any;
        const msg = e?.message || '';
        const status = e?.details?.status as number | undefined;
        // If it's a terminal, user-facing error, abort immediately
        if (msg === t('player.leaseBusy') || msg.startsWith(t('player.sessionFailed')) || msg === t('player.authFailed')) {
          throw err;
        }

        // Non-retryable client errors (except 404 handled above, and 429 which can be transient).
        if (typeof status === 'number' && status >= 400 && status < 500 && status !== 404 && status !== 429) {
          throw err;
        }

        if (i === maxAttempts - 1) {
          if (err instanceof PlayerError) {
            throw new PlayerError(`${t('player.readinessCheckFailed')}: ${msg}`, e?.details);
          }
          throw new Error(`${t('player.readinessCheckFailed')}: ${msg}`);
        }
        await sleep(500);
      }
    }
    throw new Error(t('player.sessionNotReadyInTime'));
  }, [apiBase, authHeaders, t, applySessionInfo]);

  const updateStats = useCallback((hls: Hls) => {
    if (!hls) return;
    // Handle Auto (-1) or manual level
    const idx = hls.currentLevel === -1 ? 0 : hls.currentLevel; // Fallback to 0 for stats
    const level = hls.levels ? hls.levels[idx] : undefined;

    if (level) {
      setStats(prev => {
        let newBandwidth = prev.bandwidth;
        let newRes = prev.resolution;

        if (level.bitrate) newBandwidth = Math.round(level.bitrate / 1024);
        if (level.width && level.height) {
          newRes = `${level.width}x${level.height}`;
        }

        return {
          ...prev,
          bandwidth: newBandwidth,
          resolution: newRes,
          levelIndex: hls.currentLevel, // Keep actual -1 for display truth
        };
      });
    } else {
      setStats(prev => ({
        ...prev,
        levelIndex: hls.currentLevel,
      }));
    }
  }, []);

  const playHls = useCallback((url: string) => {
    const video = videoRef.current;
    if (!video) return;

    lastDecodedRef.current = 0; // Reset native FPS counter on new stream

    const canPlayNative = !!video.canPlayType('application/vnd.apple.mpegurl');
    const preferNative = isSafari && canPlayNative;
    if (!preferNative && Hls.isSupported()) {
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: false,
        backBufferLength: 300,
        maxBufferLength: 60,
        capLevelToPlayerSize: true // Optimize for windowed mode
      });
      hlsRef.current = hls;

      hls.on(Hls.Events.LEVEL_SWITCHED, () => updateStats(hls));
      hls.on(Hls.Events.MANIFEST_PARSED, (_e, data: ManifestParsedData) => {
        debugLog('[V3Player] HLS Manifest Parsed', { levels: data.levels.length });

        // Phase 12 Fix: Ensure we start on a valid level (Auto)
        if (hls.currentLevel === -1 && data.levels.length > 0) {
          hls.startLevel = -1; // Explicitly set Auto start
        }

        updateStats(hls);
        if (data.levels && data.levels.length > 0) {
          const first = data.levels[0];
          if (first) {
            setStats(prev => ({ ...prev, fps: first.frameRate || 0 }));
          }
        }
        videoRef.current?.play().catch(e => {
          debugWarn("[V3Player] Autoplay failed", e);
          setStatus('ready'); // Force ready so user can click play
        });
      });

      // Phase 5 Fix: Consume backend READY signal to force UI state
      hls.on(Hls.Events.LEVEL_LOADED, (_e, data: LevelLoadedData) => {
        // Check custom header from backend (injected in hls_contract.go)
        // hls.js exposes response headers via stats or network details depending on version/loader
        // For standard fetch loader, we might not get headers easily in event data.
        // Fallback: If we have levels, duration OR valid live fragments, we are effectively ready.
        // For LIVE, totalduration can be 0 or small window, so check fragments length.
        const hasContent = data.details.totalduration > 0 || (data.details.fragments && data.details.fragments.length > 0);

        if (hasContent && status === 'buffering') {
          debugLog('[V3Player] Level Loaded with content, forcing READY state');
          setStatus('ready');
        }
      });

      hls.on(Hls.Events.FRAG_LOADED, (_e, data: FragLoadedData) => {
        debugLog('[V3Player] HLS Frag Loaded', { sn: data.frag.sn, dur: data.frag.duration });

        // Ensure stats are current
        const currentLevel = hls.currentLevel;
        if (currentLevel >= 0) {
          updateStats(hls);
        }

        setStats(prev => ({
          ...prev,
          buffer: Math.round(data.frag.duration * 100) / 100,
          levelIndex: hls.currentLevel // Force sync
        }));
      });

      hls.loadSource(url);
      hls.attachMedia(video);

      hls.on(Hls.Events.ERROR, (_event, data: ErrorData) => {
        if (data.fatal) {
          // Report fatal error to backend
          if (sessionIdRef.current) {
            const code = data.type === Hls.ErrorTypes.MEDIA_ERROR ? 3 : 0;
            void reportError('error', code, `${data.type}: ${data.details}`);
          }

          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              hls.recoverMediaError();
              break;
            default:
              hlsRef.current?.destroy();
              setStatus('error');
              setError(`${t('player.hlsError')}: ${data.type}`);
              setErrorDetails(JSON.stringify(data, null, 2));
              break;
          }
        }
      });
    } else if (canPlayNative) {
      // Safari Native
      video.src = url;
      video.addEventListener('loadedmetadata', () => {
        video.play().catch(e => debugWarn("[V3Player] Native play blocked", e));
      }, { once: true });
    }
  }, [t, updateStats, isSafari, reportError]);

  const playDirectMp4 = useCallback((url: string) => {
    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }
    const video = videoRef.current;
    if (!video) return;

    lastDecodedRef.current = 0; // Reset native FPS counter on new stream

    // Reset stats for direct play
    setStats(prev => ({ ...prev, bandwidth: 0, resolution: 'Original (Direct)', fps: 0, levelIndex: -1 }));

    // Log for verification
    debugLog('[V3Player] Switching to Direct MP4 Mode:', url);

    // Native playback
    video.src = url;
    video.load();
    video.play().catch(e => debugWarn("Autoplay failed", e));
  }, []);

  const waitForDirectStream = useCallback(async (url: string): Promise<void> => {
    const maxRetries = 100; // 5 minutes max wait
    let retries = 0;

    while (retries < maxRetries) {
      if (isTeardownRef.current) throw new Error('Playback cancelled');

      try {
        // Use standard fetch to check status without downloading body
        // Disable cache to avoid false positives/negatives on 503s
        const res = await fetch(url, { method: 'HEAD', cache: 'no-store' });

        if (res.ok || res.status === 206) {
          return; // Ready!
        }

        if (res.status === 503) {
          // Fixed backoff (1s) ignoring Retry-After headers on probes
          await new Promise(r => setTimeout(r, 1000));
          // Continue loop (implicit)
        } else {
          throw new Error(`Unexpected status: ${res.status}`);
        }
      } catch (e) {
        debugWarn('[V3Player] Probe failed', e);
        throw new Error(t('player.networkError'));
      }

      retries++;
    }
    debugWarn('[V3Player] Direct Stream Timeout after', maxRetries, 'attempts');
    throw new Error(t('player.timeout'));
  }, [t]);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    setVodStreamMode(null);
    clearVodRetry();
    clearVodFetch();
    resetPlaybackEngine();
    setStatus('building');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);
    setPlaybackMode('VOD');

    let abortController: AbortController | null = null;

    try {
      await ensureSessionCookie();

      // Determine Playback Mode
      let streamUrl = '';
      let mode: 'hls' | 'direct_mp4' | 'deny' = 'deny';

      try {
        const maxMetaRetries = 20;
        let pInfo: PlaybackInfo | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await getRecordingPlaybackInfo({
            path: { recordingId: id }
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

        const resolution = resolvePlaybackInfoPolicy(capabilities, pInfo);

        if (resolution.mode === 'normative') {
          // Type Assertion: We treat the object as strictly normative here.
          // Any access to pInfo.url or decision outputs will now fail compile-time check (if we used the var).
          const normativePInfo = pInfo as unknown as NormativePlaybackInfo;

          // TS-Guard
          if (!normativePInfo.decision?.selectedOutputUrl) {
            telemetry.emit('ui.failclosed', {
              context: 'V3Player.decision.selectionMissing',
              reason: 'DECISION_SELECTION_MISSING'
            });
            throw new Error('Decision-led playback missing explicit selection');
          }
          streamUrl = normativePInfo.decision.selectedOutputUrl;
          mode = normativePInfo.decision.selectedOutputKind === 'hls' ? 'hls' : 'direct_mp4';
          telemetry.emit('ui.contract.consumed', {
            mode: 'normative',
            fields: ['decision.selectedOutputUrl']
          });
        }
        else if (resolution.mode === 'legacy') {
          if (pInfo.mode === 'deny') {
            throw new Error(t('player.playbackDenied'));
          }
          if (!pInfo.url) {
            throw new Error(t('player.notAvailable'));
          }
          streamUrl = pInfo.url;
          mode = pInfo.mode as any;
          telemetry.emit('ui.contract.consumed', {
            mode: 'legacy',
            fields: ['url', 'mode']
          });
        }
        else {
          // Fail Closed
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.PolicyEngine',
            reason: resolution.reason
          });
          setStatus('error');
          // Contract/Governance failure: keep the user-facing error, but surface the reason explicitly.
          setError(`${t('player.playbackError')} (Policy Violation)`);
          setErrorDetails(`reason=${resolution.reason}`);
          return;
        }

        if (streamUrl.startsWith('/')) {
          streamUrl = `${window.location.origin}${streamUrl}`;
        }

        // Add Cache Busting to prevent sticky 503s
        streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;

        setVodStreamMode(mode as any);

        // Truth Consumption
        if (pInfo.durationSeconds && pInfo.durationSeconds > 0) {
          setDurationSeconds(pInfo.durationSeconds);
        }

        if (pInfo.requestId) setTraceId(pInfo.requestId);
        if (pInfo.isSeekable !== undefined) setCanSeek(pInfo.isSeekable);
        if (pInfo.startUnix) setStartUnix(pInfo.startUnix);

        // Resume State
        if (pInfo.resume && pInfo.resume.posSeconds >= 15 && (!pInfo.resume.finished)) {
          const d = pInfo.resume.durationSeconds || (pInfo.durationSeconds || 0);
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

      if (mode === 'hls') {
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
          playHls(streamUrl);
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
  }, [apiBase, authHeaders, clearVodFetch, clearVodRetry, playDirectMp4, playHls, resetPlaybackEngine, t, waitForDirectStream, ensureSessionCookie]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    if (startIntentInFlight.current) return;
    startIntentInFlight.current = true;
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
        sessionIdRef.current = null;
        stopSentRef.current = null;
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
      sessionIdRef.current = null;
      stopSentRef.current = null;
      setDurationSeconds(duration && duration > 0 ? duration : null);

      const ref = refToUse || sRef;
      let newSessionId: string | null = null;
      setStatus('starting');
      setError(null);
      setErrorDetails(null);
      setShowErrorDetails(false);
      setPlaybackMode('LIVE');

      try {
        await ensureSessionCookie();

        const preferredCodecs = await detectPreferredCodecs(videoRef.current as unknown as HTMLVideoElement | null);

        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            type: 'stream.start',

            serviceRef: ref,
            params: {
              codecs: preferredCodecs.join(',')
            }
          })
        });

        if (res.status === 401 || res.status === 403) {
          setStatus('error');
          setError(t('player.authFailed'));
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
          let errorDetails = null;
          try {
            const isJson = res.headers.get('content-type')?.includes('application/json');
            if (isJson) {
              const apiErr: import('../client-ts').ApiError = await res.json();
              if (apiErr.message) errorMsg = apiErr.message;
              if (apiErr.details) errorDetails = typeof apiErr.details === 'string' ? apiErr.details : JSON.stringify(apiErr.details);
            } else {
              errorDetails = await res.text();
            }
          } catch (e) {
            debugWarn("Failed to parse error response", e);
          }
          const err = new Error(errorMsg);
          err.stack = errorDetails || undefined;
          throw err;
        }

        const data: V3SessionResponse = await res.json();
        newSessionId = data.sessionId;
        if (data.requestId) setTraceId(data.requestId);
        sessionIdRef.current = newSessionId;
        setSessionId(newSessionId);
        const session = await waitForSessionReady(newSessionId);

        setStatus('ready');
        const streamUrl = session.playbackUrl;
        if (!streamUrl) {
          throw new Error(t('player.streamUrlMissing'));
        }
        playHls(streamUrl);

      } catch (err) {
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
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
  }, [src, recordingId, sRef, apiBase, authHeaders, ensureSessionCookie, waitForSessionReady, playHls, sendStopIntent, t, duration, startRecordingPlayback, applyAutoplayMute]);

  const stopStream = useCallback(async (skipClose: boolean = false): Promise<void> => {
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
    sessionIdRef.current = null;
    stopSentRef.current = null;
    setSessionId(null);
    setPlaybackMode('UNKNOWN');
    setSeekableStart(0);
    setSeekableEnd(0);
    setCurrentPlaybackTime(0);
    setStatus('stopped');
    setVodStreamMode(null);
    if (onClose && !skipClose) onClose();
  }, [clearVodFetch, clearVodRetry, onClose, sendStopIntent, sessionId]);

  const handleRetry = useCallback(async () => {
    try {
      await stopStream();
    } finally {
      startIntentInFlight.current = false;
      void startStream();
    }
  }, [stopStream, startStream]);



  // --- Effects ---

  // Keyboard Shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable;
      if (isInput) return;

      switch (e.key.toLowerCase()) {
        case 'f':
          toggleFullscreen();
          break;
        case 'm':
          e.preventDefault();
          toggleMute();
          break;
        case ' ':
        case 'k':
          e.preventDefault();
          togglePlayPause();
          break;
        case 'i':
          setShowStats(prev => !prev);
          break;
        case 'p':
          togglePiP();
          break;
        case 'arrowleft':
          seekBy(-15);
          break;
        case 'arrowright':
          seekBy(15);
          break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [toggleFullscreen, togglePlayPause, togglePiP, toggleMute, seekBy]);

  // ADR-009: Session Heartbeat Loop
  useEffect(() => {
    if (!sessionId || !heartbeatInterval || status !== 'ready') {
      return; // Only run when session is READY
    }

    const intervalMs = heartbeatInterval * 1000;
    debugLog('[V3Player][Heartbeat] Starting heartbeat loop:', { sessionId, intervalMs });

    const timerId = setInterval(async () => {
      try {
        const res = await fetch(`${apiBase}/sessions/${sessionId}/heartbeat`, {
          method: 'POST',
          headers: authHeaders(true)
        });

        if (res.status === 200) {
          const data = await res.json();
          setLeaseExpiresAt(data.lease_expires_at);
          debugLog('[V3Player][Heartbeat] Lease extended:', data.lease_expires_at);
        } else if (res.status === 410) {
          // Terminal: Session lease expired
          debugError('[V3Player][Heartbeat] Session expired (410)');
          clearInterval(timerId);
          setStatus('error');
          setError(t('player.sessionExpired') || 'Session expired. Please restart.');
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 404) {
          debugWarn('[V3Player][Heartbeat] Session not found (404)');
          clearInterval(timerId);
          setStatus('error');
          setError(t('player.sessionNotFound') || 'Session no longer exists.');
          if (videoRef.current) {
            videoRef.current.pause();
          }
        }
      } catch (error) {
        debugError('[V3Player][Heartbeat] Network error:', error);
        // Allow retry on next interval (no infinite loops)
      }
    }, intervalMs);

    return () => {
      debugLog('[V3Player][Heartbeat] Cleanup: Clearing heartbeat timer');
      clearInterval(timerId);
    };
  }, [sessionId, heartbeatInterval, status, apiBase, authHeaders, t]);

  // Video Event Listeners
  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const onWaiting = () => {
      // Phase 15 Fix: Ignore waiting if we have forward buffer (micro-stalls)
      let buffHealth = 0;
      if (videoEl.buffered.length > 0) {
        for (let i = 0; i < videoEl.buffered.length; i++) {
          if (videoEl.currentTime >= videoEl.buffered.start(i) && videoEl.currentTime <= videoEl.buffered.end(i)) {
            buffHealth = videoEl.buffered.end(i) - videoEl.currentTime;
            break;
          }
        }
      }

      if (videoEl.readyState >= 3 && buffHealth > 0.5) {
        debugLog('[V3Player] Event: waiting (ignored, buffer=' + buffHealth.toFixed(1) + 's)');
        return;
      }

      debugLog('[V3Player] Event: waiting -> buffering', { readyState: videoEl.readyState, buff: buffHealth.toFixed(1) });
      setStatus('buffering');
    };

    const onStalled = () => {
      // Stalled means network fetch stopped, but if we have buffer, we are fine.
      let buffHealth = 0;
      if (videoEl.buffered.length > 0) {
        for (let i = 0; i < videoEl.buffered.length; i++) {
          if (videoEl.currentTime >= videoEl.buffered.start(i) && videoEl.currentTime <= videoEl.buffered.end(i)) {
            buffHealth = videoEl.buffered.end(i) - videoEl.currentTime;
            break;
          }
        }
      }

      if (buffHealth > 1.0) {
        debugLog('[V3Player] Event: stalled (ignored, buffer=' + buffHealth.toFixed(1) + 's)');
        return;
      }

      debugLog('[V3Player] Event: stalled -> buffering');
      setStatus('buffering');
    };
    const onSeeking = () => {
      debugLog('[V3Player] Event: seeking -> buffering');
      setStatus('buffering');
    };

    const onPlaying = () => {
      debugLog('[V3Player] Event: playing -> playing');
      setStatus('playing');
      setError(null);
      setErrorDetails(null);
      setShowErrorDetails(false);
    };

    const onPause = () => setStatus(prev => (prev === 'error' ? prev : 'paused'));

    const onSeeked = () => {
      setStatus(prev => (prev === 'error' ? prev : (videoEl.paused ? 'paused' : 'playing')));
    };

    const onError = () => {
      // Ignore errors during teardown or if src is empty (Error 4 noise)
      // Ignore errors during teardown or if src is empty (Error 4 noise)
      if (isTeardownRef.current) return;
      // Strict check: if currentSrc is empty or "about:blank", it's cleanup noise
      if (!videoEl.currentSrc || videoEl.currentSrc === 'about:blank' || !videoEl.getAttribute('src')) return;

      const err = videoEl.error;
      const diagnostics = {
        code: err?.code,
        message: err?.message,
        currentSrc: videoEl.currentSrc,
        readyState: videoEl.readyState,
        networkState: videoEl.networkState,
        buffered: Array.from({ length: videoEl.buffered.length }, (_, i) => ({
          start: videoEl.buffered.start(i),
          end: videoEl.buffered.end(i)
        })),
        videoWidth: videoEl.videoWidth,
        videoHeight: videoEl.videoHeight,
        paused: videoEl.paused,
        hlsJsActive: !!hlsRef.current
      };

      debugError('[V3Player] Video Element Error:', diagnostics);

      // Report to backend (triggers fallback if code=3)
      if (err && sessionIdRef.current) {
        // Run in background
        const safeCode = typeof err.code === 'number' ? err.code : 0;
        void reportError('error', safeCode, err.message || JSON.stringify(diagnostics));
      }

      setStatus('error');
      // Append video error to any existing error for full context
      setError(prev => `Video Error: ${err?.code} (${err?.message}) | State: ${videoEl.readyState}/${videoEl.networkState} | Prev: ${prev}`);
    };

    videoEl.addEventListener('waiting', onWaiting);
    videoEl.addEventListener('stalled', onStalled);
    videoEl.addEventListener('seeking', onSeeking);
    videoEl.addEventListener('seeked', onSeeked);
    videoEl.addEventListener('playing', onPlaying);
    videoEl.addEventListener('pause', onPause);
    videoEl.addEventListener('error', onError);

    return () => {
      videoEl.removeEventListener('waiting', onWaiting);
      videoEl.removeEventListener('stalled', onStalled);
      videoEl.removeEventListener('seeking', onSeeking);
      videoEl.removeEventListener('seeked', onSeeked);
      videoEl.removeEventListener('playing', onPlaying);
      videoEl.removeEventListener('pause', onPause);
      videoEl.removeEventListener('error', onError);
    };
  }, [reportError]);

  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const handleTimeUpdate = () => refreshSeekableState();

    videoEl.addEventListener('timeupdate', handleTimeUpdate);
    videoEl.addEventListener('loadedmetadata', handleTimeUpdate);
    videoEl.addEventListener('durationchange', handleTimeUpdate);
    videoEl.addEventListener('progress', handleTimeUpdate);
    videoEl.addEventListener('seeking', handleTimeUpdate);

    refreshSeekableState();

    return () => {
      videoEl.removeEventListener('timeupdate', handleTimeUpdate);
      videoEl.removeEventListener('loadedmetadata', handleTimeUpdate);
      videoEl.removeEventListener('durationchange', handleTimeUpdate);
      videoEl.removeEventListener('progress', handleTimeUpdate);
      videoEl.removeEventListener('seeking', handleTimeUpdate);
    };
  }, [refreshSeekableState]);

  // Stats Polling
  useEffect(() => {
    if (!showStats) return;
    const interval = setInterval(() => {
      if (!videoRef.current) return;
      const v = videoRef.current;

      let dropped = 0;
      let decoded = lastDecodedRef.current;
      // Webkit non-standard extension
      interface WebkitVideoElement extends HTMLVideoElement {
        webkitDroppedFrameCount?: number;
        webkitDecodedFrameCount?: number;
      }

      if (v.getVideoPlaybackQuality) {
        const q = v.getVideoPlaybackQuality();
        dropped = q.droppedVideoFrames;
        decoded = q.totalVideoFrames;
      } else if ('webkitDroppedFrameCount' in v) {
        dropped = (v as WebkitVideoElement).webkitDroppedFrameCount || 0;
        decoded = (v as WebkitVideoElement).webkitDecodedFrameCount || lastDecodedRef.current;
      }

      const currentFps = Math.max(0, decoded - lastDecodedRef.current);
      lastDecodedRef.current = decoded;

      let buffHealth = 0;
      if (v.buffered.length > 0) {
        for (let i = 0; i < v.buffered.length; i++) {
          const start = v.buffered.start(i);
          const end = v.buffered.end(i);
          if (v.currentTime >= start && v.currentTime <= end) {
            buffHealth = end - v.currentTime;
            break;
          }
        }
        if (buffHealth === 0 && v.buffered.length > 0) {
          const lastEnd = v.buffered.end(v.buffered.length - 1);
          if (lastEnd > v.currentTime) {
            buffHealth = lastEnd - v.currentTime;
          }
        }
      }
      buffHealth = Math.max(0, buffHealth);

      let lat: number | null = null;
      const isLive = playbackMode === 'LIVE';

      if (isLive && hlsRef.current) {
        if (hlsRef.current.latency !== undefined && hlsRef.current.latency !== null) {
          lat = hlsRef.current.latency;
        } else if (hlsRef.current.liveSyncPosition) {
          lat = hlsRef.current.liveSyncPosition - v.currentTime;
        }
        if (lat !== null) lat = Math.max(0, lat);
      }

      setStats(prev => {
        let newRes = prev.resolution;
        let newFps = prev.fps;
        let newBandwidth = prev.bandwidth;
        let newSegDur = prev.buffer;

        // Native fallback or update if video dimensions exist
        if (v.videoWidth && v.videoHeight) {
          const vidRes = `${v.videoWidth}x${v.videoHeight}`;
          if (prev.resolution === '-' || prev.resolution === 'Original (Direct)') {
            newRes = vidRes;
          } else if (prev.resolution !== vidRes && prev.resolution !== '-') {
            newRes = vidRes;
          }
        }

        if (!hlsRef.current && v.src) {
          newFps = currentFps;
        } else if (hlsRef.current) {
          if (currentFps > 0) {
            newFps = currentFps;
          } else if (prev.fps === 0 && hlsRef.current.levels && hlsRef.current.currentLevel >= 0) {
            const lvl = hlsRef.current.levels[hlsRef.current.currentLevel];
            if (lvl && lvl.frameRate) newFps = lvl.frameRate;
          }

          // Aggressively sync bandwidth if zero, desyncs happen on Auto StartLevel
          if (newBandwidth === 0 && hlsRef.current.levels) {
            const idx = hlsRef.current.currentLevel === -1 ? 0 : hlsRef.current.currentLevel;
            const lvl = hlsRef.current.levels[idx];
            if (lvl && lvl.bitrate) {
              newBandwidth = Math.round(lvl.bitrate / 1024);
              if (newRes === '-') newRes = lvl.width ? `${lvl.width}x${lvl.height}` : '-';
            }
          }
        }

        return {
          ...prev,
          resolution: newRes,
          fps: newFps,
          bandwidth: newBandwidth,
          buffer: newSegDur,
          droppedFrames: dropped,
          bufferHealth: parseFloat(buffHealth.toFixed(1)),
          latency: lat !== null ? parseFloat(lat.toFixed(2)) : null
        };
      });

      // Phase 13 Fix: Failsafe state transition
      // If we have data and are playing, but UI says buffering, force it.
      // Use functional update to access fresh state vs closure capture
      setStatus(prevStatus => {
        if (v.readyState >= 3 && !v.paused && (prevStatus === 'buffering' || prevStatus === 'starting' || prevStatus === 'priming')) {
          debugLog('[V3Player] Monitor: readyState=' + v.readyState + ', forcing PLAYING');
          return 'playing';
        }
        if (v.readyState >= 3 && v.paused && (prevStatus === 'buffering' || prevStatus === 'starting')) {
          debugLog('[V3Player] Monitor: readyState=' + v.readyState + ' (paused), forcing READY');
          return 'ready';
        }
        return prevStatus;
      });

    }, 1000);
    return () => clearInterval(interval);
  }, [showStats, playbackMode]);

  // Track play/pause state for VOD controls
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const handlePlay = () => setIsPlaying(true);
    const handlePause = () => setIsPlaying(false);

    video.addEventListener('play', handlePlay);
    video.addEventListener('pause', handlePause);

    // Initialize state
    setIsPlaying(!video.paused);

    return () => {
      video.removeEventListener('play', handlePlay);
      video.removeEventListener('pause', handlePause);
    };
  }, []);

  // Initialize volume from video element
  useEffect(() => {
    if (videoRef.current) {
      setVolume(videoRef.current.volume);
      setIsMuted(videoRef.current.muted);
      if (videoRef.current.volume > 0) {
        lastNonZeroVolumeRef.current = videoRef.current.volume;
      }
    }
  }, []);

  // Fullscreen/PiP Listeners
  useEffect(() => {
    const onFsChange = () => setIsFullscreen(!!document.fullscreenElement);
    const onPipChange = () => setIsPip(!!document.pictureInPictureElement);

    // Safari Native Fullscreen Handler - Switch to DVR profile on enter
    const onWebkitFullscreenChange = () => {
      const video = videoRef.current;
      if (!video || !isSafari) return;

      // ADR-00X: Fullscreen profile switching removed (universal policy only)
      // Safari native fullscreen no longer triggers profile changes
    };

    document.addEventListener('fullscreenchange', onFsChange);
    if (videoRef.current) {
      videoRef.current.addEventListener('enterpictureinpicture', onPipChange);
      videoRef.current.addEventListener('leavepictureinpicture', onPipChange);

      // Safari-specific fullscreen events
      if (isSafari) {
        videoRef.current.addEventListener('webkitbeginfullscreen', onWebkitFullscreenChange);
        videoRef.current.addEventListener('webkitendfullscreen', onWebkitFullscreenChange);
      }
    }

    return () => {
      document.removeEventListener('fullscreenchange', onFsChange);
      if (videoRef.current) {
        videoRef.current.removeEventListener('enterpictureinpicture', onPipChange);
        videoRef.current.removeEventListener('leavepictureinpicture', onPipChange);

        if (isSafari) {
          videoRef.current.removeEventListener('webkitbeginfullscreen', onWebkitFullscreenChange);
          videoRef.current.removeEventListener('webkitendfullscreen', onWebkitFullscreenChange);
        }
      }
    };
  }, [isSafari, stopStream, startStream, src]);

  // Idle Detection (Autohide UI)
  const [isIdle, setIsIdle] = useState(false);
  const idleTimerRef = useRef<number | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    // If no container, we can't listen.
    if (!container) return;

    const resetIdle = () => {
      setIsIdle(false);
      if (idleTimerRef.current) window.clearTimeout(idleTimerRef.current);
      idleTimerRef.current = window.setTimeout(() => setIsIdle(true), 3000);
    };

    // Initial start
    resetIdle();

    const onMove = () => resetIdle();
    const onClick = () => resetIdle();
    const onKey = () => resetIdle();

    container.addEventListener('mousemove', onMove);
    container.addEventListener('click', onClick);
    container.addEventListener('keydown', onKey);
    // Also listen to touch for mobile
    container.addEventListener('touchstart', onClick);

    return () => {
      if (idleTimerRef.current) window.clearTimeout(idleTimerRef.current);
      container.removeEventListener('mousemove', onMove);
      container.removeEventListener('click', onClick);
      container.removeEventListener('keydown', onKey);
      container.removeEventListener('touchstart', onClick);
    };
  }, []);


  // Update sRef on channel change
  useEffect(() => {
    if (channel) {
      const ref = channel.serviceRef || channel.id;
      if (ref) setSRef(ref);
    }
  }, [channel]);

  useEffect(() => {
    if (!autoStart || mounted.current) return;
    // UI-INV-PLAYER-001: Autostart requires an explicit source.
    const hasSource = !!(src || recordingId || sRef);
    if (hasSource) {
      mounted.current = true;
      startStream(sRef);
    }
  }, [autoStart, src, recordingId, sRef, startStream]);

  // Session ID Ref sync
  useEffect(() => {
    sessionIdRef.current = sessionId;
  }, [sessionId]);

  // Token update effect
  useEffect(() => {
    setClientAuthToken(token);
    sessionCookieRef.current.token = null;
    sessionCookieRef.current.pending = null;
  }, [token]);

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

  const windowDuration = Math.max(0, seekableEnd - seekableStart);
  const relativePosition = Math.min(windowDuration, Math.max(0, currentPlaybackTime - seekableStart));
  const hasSeekWindow = canSeek && windowDuration > 0;
  const isLiveMode = playbackMode === 'LIVE';
  const isAtLiveEdge = isLiveMode && windowDuration > 0 && Math.abs(seekableEnd - currentPlaybackTime) < 2;

  // P3-4: Absolute Timeline Formatting
  const startTimeDisplay = startUnix
    ? formatTimeOfDay(startUnix + relativePosition)
    : formatClock(relativePosition);

  const endTimeDisplay = startUnix
    ? formatTimeOfDay(startUnix + windowDuration)
    : formatClock(windowDuration);

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
              {errorDetails}
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
        {isSafari && (
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
          onClick={() => setShowStats(!showStats)}
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
