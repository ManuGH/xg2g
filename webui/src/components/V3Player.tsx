import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from 'hls.js';
import type { ErrorData, FragLoadedData, ManifestParsedData } from 'hls.js';
import { createSession, getRecordingPlaybackInfo } from '../client-ts/sdk.gen';
import { client } from '../client-ts/client.gen';
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
import { Card, StatusChip } from './ui';
import './V3Player.css';

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
  request_id?: string;
  details?: unknown;
}

function V3Player(props: V3PlayerProps) {
  const { t } = useTranslation();
  const { token, autoStart, onClose, duration } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;

  const [sRef, setSRef] = useState<string>(
    channel?.service_ref || channel?.id || '1:0:19:283D:3FB:1:C00000:0:0:0:'
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
  const [isPip, setIsPip] = useState(false);
  const [, setIsFullscreen] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);
  const [, setLiveEdgeUnix] = useState<number | null>(null);
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
    return (client.getConfig().baseUrl || '/api/v3').replace(/\/$/, '');
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
      v.play().catch(e => console.warn("Seek play failed", e));
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
      videoRef.current.play().catch(e => console.warn("Play failed", e));
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
        console.warn("Fullscreen failed", err);
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
      console.warn("PiP failed", err);
    }
  }, []);

  const toggleMute = useCallback(() => {
    if (!videoRef.current) return;
    if (isMuted) {
      videoRef.current.muted = false;
      setIsMuted(false);
    } else {
      videoRef.current.muted = true;
      setIsMuted(true);
    }
  }, [isMuted]);

  const handleVolumeChange = useCallback((newVolume: number) => {
    if (!videoRef.current) return;
    videoRef.current.volume = newVolume;
    setVolume(newVolume);
    if (newVolume === 0) {
      setIsMuted(true);
    } else if (isMuted) {
      setIsMuted(false);
      videoRef.current.muted = false;
    }
  }, [isMuted]);

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
      console.warn('Failed to send feedback', e);
    }
  }, [apiBase, authHeaders]);

  const ensureSessionCookie = useCallback(async (): Promise<void> => {
    if (!token) return;
    if (sessionCookieRef.current.token === token) return;
    if (sessionCookieRef.current.pending) return sessionCookieRef.current.pending;

    const pending = (async () => {
      try {
        client.setConfig({ headers: token ? { Authorization: `Bearer ${token}` } : {} });
        await createSession();
        sessionCookieRef.current.token = token;
      } catch (err) {
        console.warn('Failed to create session cookie', err);
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
      console.warn('Failed to stop v3 session', err);
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
          throw new Error(t('player.authFailed'));
        }

        if (res.status === 404) {
          await sleep(100); // Fast retry for session creation
          continue;
        }
        if (!res.ok) throw new Error(t('player.failedToFetchSession'));

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
        const msg = (err as Error).message || '';
        // If it's a terminal, user-facing error, abort immediately
        if (msg === t('player.leaseBusy') || msg.startsWith(t('player.sessionFailed')) || msg === t('player.authFailed')) {
          throw err;
        }

        if (i === maxAttempts - 1) {
          throw new Error(`${t('player.readinessCheckFailed')}: ${(err as Error).message}`);
        }
        await sleep(500);
      }
    }
    throw new Error(t('player.sessionNotReadyInTime'));
  }, [apiBase, authHeaders, t, applySessionInfo]);

  const updateStats = useCallback((hls: Hls) => {
    if (!hls) return;
    const level = hls.levels[hls.currentLevel];
    if (level) {
      setStats(prev => ({
        ...prev,
        bandwidth: Math.round(level.bitrate / 1024),
        resolution: level.width ? `${level.width}x${level.height}` : '-',
        levelIndex: hls.currentLevel,
      }));
    }
  }, []);

  const playHls = useCallback((url: string) => {
    const video = videoRef.current;
    if (!video) return;
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
        maxBufferLength: 60
      });
      hlsRef.current = hls;

      hls.on(Hls.Events.LEVEL_SWITCHED, () => updateStats(hls));
      hls.on(Hls.Events.MANIFEST_PARSED, (_e, data: ManifestParsedData) => {
        updateStats(hls);
        if (data.levels && data.levels.length > 0) {
          const first = data.levels[0];
          if (first) {
            setStats(prev => ({ ...prev, fps: first.frameRate || 0 }));
          }
        }
        videoRef.current?.play().catch(e => console.warn("Autoplay failed", e));
      });

      hls.on(Hls.Events.FRAG_LOADED, (_e, data: FragLoadedData) => {
        setStats(prev => ({
          ...prev,
          buffer: Math.round(data.frag.duration * 100) / 100
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
        video.play().catch(e => console.warn("[V3Player] Native play blocked", e));
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

    // Reset stats for direct play
    setStats(prev => ({ ...prev, bandwidth: 0, resolution: 'Original (Direct)', fps: 0, levelIndex: -1 }));

    // Log for verification
    console.debug('[V3Player] Switching to Direct MP4 Mode:', url);

    // Native playback
    video.src = url;
    video.load();
    video.play().catch(e => console.warn("Autoplay failed", e));
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
        console.warn('[V3Player] Probe failed', e);
        throw new Error(t('player.networkError'));
      }

      retries++;
    }
    console.warn('[V3Player] Direct Stream Timeout after', maxRetries, 'attempts');
    throw new Error(t('player.timeout'));
  }, [t]);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    clearVodRetry();
    clearVodFetch();
    resetPlaybackEngine();
    setStatus('building');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);
    setPlaybackMode('VOD');

    try {
      await ensureSessionCookie();

      // Determine Playback Mode
      const hlsUrl = `${apiBase}/recordings/${id}/playlist.m3u8`;
      let streamUrl = hlsUrl;
      let mode = 'hls';


      try {
        // Use generated client with strict typing and contract enforcement
        const maxMetaRetries = 20;
        let pInfo: import('../client-ts/types.gen').PlaybackInfo | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await getRecordingPlaybackInfo({
            path: { recordingId: id }
          });

          if (error) {
            // CONTRACT-FE-001: Strict Retry-After enforcement for PlaybackInfo
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                setStatus('building');
                setErrorDetails(`${t('player.preparing')} (${seconds}s)`);
                await sleep(seconds * 1000);
                continue;
              } else {
                // Strict 503: Fail if no Retry-After
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

        console.debug('[V3Player] Playback Info:', pInfo);

        if (pInfo.mode === 'direct_mp4' && pInfo.url) {
          mode = 'direct_mp4';
          streamUrl = pInfo.url;
          if (streamUrl.startsWith('/')) {
            const origin = window.location.origin;
            streamUrl = `${origin}${streamUrl}`;
          }
          // Add Cache Busting to prevent sticky 503s
          streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;
        }

        // Use Backend-Provided Duration
        if (pInfo.duration_seconds && pInfo.duration_seconds > 0) {
          setDurationSeconds(pInfo.duration_seconds);
          setPlaybackMode('VOD');
        }


        // P3-4: Truth Consumption
        if (pInfo.requestId) setTraceId(pInfo.requestId);
        if (pInfo.is_seekable !== undefined) {
          setCanSeek(pInfo.is_seekable);
        }
        if (pInfo.start_unix) setStartUnix(pInfo.start_unix);
        if (pInfo.live_edge_unix) setLiveEdgeUnix(pInfo.live_edge_unix);
        if (pInfo.dvr_window_seconds) setDurationSeconds(pInfo.dvr_window_seconds);

        // Resume State (Strict Typed)
        if (pInfo.resume && pInfo.resume.pos_seconds >= 15 && (!pInfo.resume.finished)) {
          const d = pInfo.resume.duration_seconds || (pInfo.duration_seconds || 0);
          if (!d || pInfo.resume.pos_seconds < d - 10) {
            // Map to internal ResumeState (nullable vs optional alignment)
            setResumeState({
              pos_seconds: pInfo.resume.pos_seconds,
              duration_seconds: pInfo.resume.duration_seconds || undefined,
              finished: pInfo.resume.finished || undefined
            });
            setShowResumeOverlay(true);
          }
        }
      } catch (e: any) {
        console.warn('[V3Player] Failed to get playback info', e);
        // Fail-closed: Show error, do NOT fallback
        if (activeRecordingRef.current !== id) return;
        setStatus('error');
        setError(e.message || t('player.serverError'));
        return;
      }

      // --- DIRECT MP4 PATH ---
      if (mode === 'direct_mp4') {
        try {
          isTeardownRef.current = false; // Force clear to prevent race condition
          // Probe with no-cache to handle "503 Preparing"
          await waitForDirectStream(streamUrl);

          // If cancelled during wait
          if (activeRecordingRef.current !== id) return;

          setStatus('buffering');
          playDirectMp4(streamUrl);
          return; // EXIT: Success
        } catch (err) {
          console.warn('[V3Player] Direct MP4 Probe Failed:', err);
          // Verify if we should show error or fallback
          if (activeRecordingRef.current !== id) return;

          setStatus('error');
          setError(t('player.timeout'));
          return;
        }
      }

      // --- HLS PATH (FALLBACK) ---
      const controller = new AbortController();
      vodFetchRef.current = controller;
      try {
        // Simple probe for HLS
        const res = await fetch(streamUrl, {
          method: 'HEAD',
          signal: controller.signal
        });

        // HLS logic remains correctly handled by standard players usually, 
        // but we handle basic errors here.
        if (res.status === 404) {
          setError(t('player.recordingNotFound'));
          setStatus('error');
          return;
        }

        // 503 logic for HLS (Rare, usually handled by playlist retry, but good to have)
        if (res.status === 503) {
          // CONTRACT-FE-001: Strict Retry-After enforcement
          const retryAfter = res.headers.get('Retry-After');

          if (!retryAfter) {
            // No header → show error instead of guessing
            setError(t('player.serverBusy'));
            setErrorDetails('Server did not provide retry guidance');
            setStatus('error');
            return;
          }

          const delay = parseInt(retryAfter, 10) * 1000;
          setStatus('building');
          vodRetryRef.current = window.setTimeout(() => {
            if (activeRecordingRef.current === id) startRecordingPlayback(id);
          }, delay);
          return;
        }

        if (activeRecordingRef.current !== id) return;

        setStatus('buffering');
        playHls(streamUrl);

      } finally {
        if (vodFetchRef.current === controller) vodFetchRef.current = null;
      }

    } catch (err) {
      if (activeRecordingRef.current !== id) return;
      console.error(err);
      setError((err as Error).message);
      setStatus('error');
    }
  }, [apiBase, authHeaders, client, clearVodFetch, clearVodRetry, playDirectMp4, playHls, resetPlaybackEngine, t, waitForDirectStream, ensureSessionCookie]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    if (startIntentInFlight.current) return;
    startIntentInFlight.current = true;

    try {
      if (recordingId) {
        console.info('[V3Player] startStream: recordingId path', { recordingId, hasSrc: !!src });
        if (src) {
          console.warn('[V3Player] Both recordingId and src provided; prioritizing recordingId (VOD path).');
        }
        await startRecordingPlayback(recordingId);
        return;
      }

      if (src) {
        console.info('[V3Player] startStream: src path', { hasSrc: true });
        // Reset state for local/src playback
        activeRecordingRef.current = null;
        setActiveRecordingId(null);
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

        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            type: 'stream.start',

            serviceRef: ref
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
          if (apiErr?.code || apiErr?.request_id) {
            const parts = [];
            if (apiErr.code) parts.push(`code=${apiErr.code}`);
            if (apiErr.request_id) parts.push(`request_id=${apiErr.request_id}`);
            setErrorDetails(parts.join(' '));
          } else {
            setErrorDetails(null);
          }
          return;
        }

        if (!res.ok) throw new Error(`${t('player.apiError')}: ${res.status}`);
        const data: V3SessionResponse = await res.json();
        newSessionId = data.sessionId;
        if (data.requestId) setTraceId(data.requestId);
        sessionIdRef.current = newSessionId;
        setSessionId(newSessionId);
        const session = await waitForSessionReady(newSessionId);

        setStatus('ready');
        const streamUrl = session.playbackUrl || `${apiBase}/sessions/${newSessionId}/hls/index.m3u8`;
        playHls(streamUrl);

      } catch (err) {
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
        console.error(err);
        setError((err as Error).message);
        setErrorDetails((err as Error).stack || null);
        setStatus('error');
      }
    } finally {
      startIntentInFlight.current = false;
    }
  }, [src, recordingId, sRef, apiBase, authHeaders, ensureSessionCookie, waitForSessionReady, playHls, sendStopIntent, t, duration, startRecordingPlayback]);

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
          if (videoRef.current) videoRef.current.muted = !videoRef.current.muted;
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
  }, [toggleFullscreen, togglePlayPause, togglePiP, seekBy]);

  // ADR-009: Session Heartbeat Loop
  useEffect(() => {
    if (!sessionId || !heartbeatInterval || status !== 'ready') {
      return; // Only run when session is READY
    }

    const intervalMs = heartbeatInterval * 1000;
    console.debug('[V3Player][Heartbeat] Starting heartbeat loop:', { sessionId, intervalMs });

    const timerId = setInterval(async () => {
      try {
        const res = await fetch(`${apiBase}/sessions/${sessionId}/heartbeat`, {
          method: 'POST',
          headers: authHeaders(true)
        });

        if (res.status === 200) {
          const data = await res.json();
          setLeaseExpiresAt(data.lease_expires_at);
          console.debug('[V3Player][Heartbeat] Lease extended:', data.lease_expires_at);
        } else if (res.status === 410) {
          // Terminal: Session lease expired
          console.error('[V3Player][Heartbeat] Session expired (410)');
          clearInterval(timerId);
          setStatus('error');
          setError(t('player.sessionExpired') || 'Session expired. Please restart.');
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 404) {
          console.warn('[V3Player][Heartbeat] Session not found (404)');
          clearInterval(timerId);
          setStatus('error');
          setError(t('player.sessionNotFound') || 'Session no longer exists.');
          if (videoRef.current) {
            videoRef.current.pause();
          }
        }
      } catch (error) {
        console.error('[V3Player][Heartbeat] Network error:', error);
        // Allow retry on next interval (no infinite loops)
      }
    }, intervalMs);

    return () => {
      console.debug('[V3Player][Heartbeat] Cleanup: Clearing heartbeat timer');
      clearInterval(timerId);
    };
  }, [sessionId, heartbeatInterval, status, apiBase, authHeaders, t]);

  // Video Event Listeners
  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const onWaiting = () => setStatus('buffering');
    const onStalled = () => setStatus('buffering');
    const onSeeking = () => setStatus('buffering');

    const onPlaying = () => {
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

      console.error('[V3Player] Video Element Error:', diagnostics);

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
      // Webkit non-standard extension
      interface WebkitVideoElement extends HTMLVideoElement {
        webkitDroppedFrameCount?: number;
      }

      if (v.getVideoPlaybackQuality) {
        dropped = v.getVideoPlaybackQuality().droppedVideoFrames;
      } else if ('webkitDroppedFrameCount' in v) {
        dropped = (v as WebkitVideoElement).webkitDroppedFrameCount || 0;
      }

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

      setStats(prev => ({
        ...prev,
        droppedFrames: dropped,
        bufferHealth: parseFloat(buffHealth.toFixed(1)),
        latency: lat !== null ? parseFloat(lat.toFixed(2)) : null
      }));

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


  // Update sRef on channel change
  useEffect(() => {
    if (channel) {
      const ref = channel.service_ref || channel.id;
      if (ref) setSRef(ref);
    }
  }, [channel]);

  useEffect(() => {
    if (!autoStart || mounted.current) return;
    if (src || recordingId || sRef) {
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
    if (token) {
      client.setConfig({ headers: { Authorization: `Bearer ${token}` } });
    }
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
  // ADR-00X: Overlay styles moved to .v3-player-overlay in V3Player.css

  // Static styles moved to .video-element in V3Player.css

  const spinnerLabel =
    status === 'starting' || status === 'priming' || status === 'buffering' || status === 'building'
      ? (status === 'buffering' && playbackMode === 'VOD' && activeRecordingRef.current)
        ? t('player.preparingDirectPlay', 'Preparing Direct Play...') // Show explicit preparing for VOD buffering
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
    <div ref={containerRef} className={`v3-player-container animate-enter ${onClose ? 'v3-player-overlay' : ''}`.trim()}
    >
      {onClose && (
        <button
          onClick={() => void stopStream()}
          className="close-btn"
          aria-label={t('player.closePlayer')}
        >
          ✕
        </button>
      )}

      {/* Stats Overlay */}
      {showStats && (
        <div className="stats-overlay">
          <Card variant="standard">
            <Card.Header>
              <Card.Title>{t('player.statsTitle', { defaultValue: 'Technical Stats' })}</Card.Title>
            </Card.Header>
            <Card.Content className="stats-grid">
              <div className="stats-row">
                <span className="stats-label">{t('player.status')}</span>
                <StatusChip
                  state={status === 'ready' ? 'live' : status === 'error' ? 'error' : 'idle'}
                  label={t(`player.statusStates.${status}`, { defaultValue: status })}
                />
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('common.session', { defaultValue: 'Session' })}</span>
                <span className="stats-value">{sessionIdRef.current || '-'}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('common.requestId', { defaultValue: 'Request ID' })}</span>
                <span className="stats-value">{traceId}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.resolution')}</span>
                <span className="stats-value">{stats.resolution}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.bandwidth')}</span>
                <span className="stats-value">{stats.bandwidth} kbps</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.bufferHealth')}</span>
                <span className="stats-value">{stats.bufferHealth}s</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.latency')}</span>
                <span className="stats-value">{stats.latency !== null ? stats.latency + 's' : '-'}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.fps')}</span>
                <span className="stats-value">{stats.fps}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.dropped')}</span>
                <span className="stats-value">{stats.droppedFrames}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.hlsLevel')}</span>
                <span className="stats-value">{stats.levelIndex}</span>
              </div>
              <div className="stats-row">
                <span className="stats-label">{t('player.segDuration')}</span>
                <span className="stats-value">{stats.buffer}s</span>
              </div>
            </Card.Content>
          </Card>
        </div>
      )}

      <div className="video-wrapper">
        {channel && <h3 className="overlay-title">{channel.name}</h3>}

        {/* PREPARING Overlay (VOD Remux) */}
        {(status === 'starting' || status === 'priming' || status === 'buffering' || status === 'building') && (
          <div className="spinner-overlay">
            <div className="spinner spinner-base"></div>
            <div className="spinner-label">{spinnerLabel}</div>
          </div>
        )}

        <video
          ref={videoRef}
          controls={false}
          playsInline
          webkit-playsinline=""
          preload="metadata"
          autoPlay={!!autoStart}
          muted={!!autoStart}
          className="video-element"
        />
      </div>

      {/* Error Toast */}
      {error && (
        <div className="error-toast" aria-live="polite">
          <div className="error-main">
            <span className="error-text">⚠ {error}</span>
            <button onClick={handleRetry} className="btn-retry">{t('common.retry')}</button>
          </div>
          {errorDetails && (
            <button
              onClick={() => setShowErrorDetails(!showErrorDetails)}
              className="error-details-btn"
            >
              {showErrorDetails ? t('common.hideDetails') : t('common.showDetails')}
            </button>
          )}
          {showErrorDetails && errorDetails && (
            <div className="error-details-content">
              {errorDetails}
              <br />
              {t('common.session')}: {sessionIdRef.current || t('common.notAvailable')}
            </div>
          )}
        </div>
      )}

      {/* Controls & Status Bar */}
      <div className="v3-player-controls-header">
        {hasSeekWindow ? (
          <div className="vod-controls seek-controls">
            <div className="seek-buttons">
              <button className="btn-icon" onClick={() => seekBy(-900)} title={t('player.seekBack15m', 'Back 15m')}>
                ↺ 15m
              </button>
              <button className="btn-icon" onClick={() => seekBy(-60)} title={t('player.seekBack60s', 'Back 60s')}>
                ↺ 60s
              </button>
              <button className="btn-icon" onClick={() => seekBy(-15)} title={t('player.seekBack15s', 'Back 15s')}>
                ↺ 15s
              </button>
            </div>

            <button
              className="vod-play-btn"
              onClick={togglePlayPause}
              title={isPlaying ? t('player.pause', 'Pause') : t('player.play', 'Play')}
            >
              {isPlaying ? '⏸' : '▶'}
            </button>

            <div className="seek-slider-group">
              <span className="vod-time">{startTimeDisplay}</span>
              <input
                type="range"
                min="0"
                max={windowDuration}
                step="0.1"
                className="vod-slider"
                value={relativePosition}
                onChange={(e) => {
                  const newVal = parseFloat(e.target.value);
                  seekTo(seekableStart + newVal);
                }}
              />
              <span className="vod-time-total">{endTimeDisplay}</span>
            </div>

            <div className="seek-buttons">
              <button className="btn-icon" onClick={() => seekBy(15)} title={t('player.seekForward15s', 'Forward 15s')}>
                +15s
              </button>
              <button className="btn-icon" onClick={() => seekBy(60)} title={t('player.seekForward60s', 'Forward 60s')}>
                +60s
              </button>
              <button className="btn-icon" onClick={() => seekBy(900)} title={t('player.seekForward15m', 'Forward 15m')}>
                +15m
              </button>
            </div>

            {isLiveMode && (
              <button
                className={`live-btn ${isAtLiveEdge ? 'active' : ''}`}
                onClick={() => seekTo(seekableEnd)}
                title={t('player.goLive', 'Go live')}
              >
                LIVE
              </button>
            )}
          </div>
        ) : (
          !channel && !recordingId && !src && (
            <input
              type="text"
              className="bg-input bg-input-service"
            />
          )
        )}

        {/* ADR-00X: Profile dropdown removed (universal policy only) */}

        {!autoStart && !src && !recordingId && (
          <button
            className="btn-primary"
            onClick={() => startStream()}
            disabled={status === 'starting' || status === 'priming'}
          >
            ▶ {t('common.startStream')}
          </button>
        )}

        {/* DVR Mode Button (Safari Only / Fallback) */}
        <button
          className={`btn-primary btn-dvr ${isSafari ? '' : 'v3-hidden'}`.trim()}
          onClick={enterDVRMode}
          title={t('player.dvrMode', 'DVR Mode (Native)')}
        >
          📺 DVR
        </button>

        {/* Volume Control */}
        <div className="volume-control">
          <button
            className="volume-btn"
            onClick={toggleMute}
            title={isMuted ? t('player.unmute', 'Unmute') : t('player.mute', 'Mute')}
          >
            {isMuted ? '🔇' : volume > 0.5 ? '🔊' : volume > 0 ? '🔉' : '🔈'}
          </button>
          <input
            type="range"
            min="0"
            max="1"
            step="0.05"
            className="volume-slider"
            value={isMuted ? 0 : volume}
            onChange={(e) => handleVolumeChange(parseFloat(e.target.value))}
          />
        </div>

        <button
          className={`btn-icon ${isPip ? 'active' : ''}`}
          onClick={togglePiP}
          title={t('player.pipTitle')}
        >
          📺 {t('player.pipLabel')}
        </button>

        <button
          className={`btn-icon ${showStats ? 'active' : ''}`}
          onClick={() => setShowStats(!showStats)}
          title={t('player.statsTitle')}
        >
          📊 {t('player.statsLabel')}
        </button>

        {!onClose && (
          <button onClick={() => void stopStream()} className="btn-danger">
            ⏹ {t('common.stop')}
          </button>
        )}
      </div>
      {/* Resume Overlay */}
      {showResumeOverlay && resumeState && (
        <div className="v3-player-resume-overlay">
          <div className="v3-player-resume-content">
            <h3>{t('player.resumeTitle', 'Resume Playback?')}</h3>
            <p>{t('player.resumePrompt', { time: formatClock(resumeState.pos_seconds) })}</p>
            <div className="v3-player-resume-actions">
              <button
                className="v3-button primary"
                onClick={() => {
                  seekWhenReady(resumeState.pos_seconds);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.resumeAction', 'Resume')}
              </button>
              <button
                className="v3-button secondary"
                onClick={() => {
                  seekWhenReady(0);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.startOver', 'Start Over')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default V3Player;
// cspell:ignore remux arrowleft arrowright enterpictureinpicture leavepictureinpicture kbps Remux
