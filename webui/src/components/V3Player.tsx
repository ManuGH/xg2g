import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from 'hls.js';
import type { ErrorData, FragLoadedData, ManifestParsedData } from 'hls.js';
import { createSession } from '../client-ts';
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

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [status, setStatus] = useState<PlayerStatus>('idle');
  const [error, setError] = useState<string | null>(null);
  const [errorDetails, setErrorDetails] = useState<string | null>(null);
  const [showErrorDetails, setShowErrorDetails] = useState(false);

  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const mounted = useRef<boolean>(false);
  const sessionIdRef = useRef<string | null>(null);
  const stopSentRef = useRef<string | null>(null);
  const sessionCookieRef = useRef<SessionCookieState>({ token: null, pending: null });

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
  const [isPlaying, setIsPlaying] = useState(false); // Track play/pause state
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
    if (!document.fullscreenElement) {
      try {
        await videoRef.current?.requestFullscreen();
      } catch (err) {
        console.warn("Fullscreen failed", err);
      }
    } else {
      await document.exitFullscreen();
    }
  }, []);

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
  }, []);

  const waitForSessionReady = useCallback(async (sid: string, maxAttempts = 60): Promise<V3SessionStatusResponse> => {
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
          await sleep(500);
          continue;
        }
        if (!res.ok) throw new Error(t('player.failedToFetchSession'));

        const session: V3SessionStatusResponse = await res.json();
        applySessionInfo(session);
        const sState = session.state;

        if (sState === 'FAILED' || sState === 'STOPPED' || sState === 'CANCELLED' || sState === 'STOPPING') {
          const reason = session.reason || sState;
          const detail = session.reasonDetail ? `: ${session.reasonDetail}` : '';
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

        await sleep(500);
      } catch (err) {
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
    if (Hls.isSupported()) {
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: false,
        backBufferLength: 300,
        maxBufferLength: 60,
        xhrSetup: (xhr) => {
          if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
        }
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
      if (videoRef.current) {
        hls.attachMedia(videoRef.current);
      }

      hls.on(Hls.Events.ERROR, (_event, data: ErrorData) => {
        if (data.fatal) {
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
    } else if (videoRef.current?.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari Native
      videoRef.current.src = url;
      videoRef.current.addEventListener('loadedmetadata', () => {
        videoRef.current?.play();
      }, { once: true });
    }
  }, [token, t, updateStats]);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    setStatus('buffering');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);
    setPlaybackMode('VOD');

    try {
      await ensureSessionCookie();
      const streamUrl = `${apiBase}/recordings/${id}/playlist.m3u8`;
      playHls(streamUrl);
    } catch (err) {
      console.error(err);
      setError((err as Error).message);
      setErrorDetails((err as Error).stack || null);
      setStatus('error');
    }
  }, [apiBase, ensureSessionCookie, playHls]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    sessionIdRef.current = null;
    stopSentRef.current = null;
    setDurationSeconds(duration && duration > 0 ? duration : null);

    if (src) {
      setPlaybackMode(duration && duration > 0 ? 'VOD' : 'LIVE');
      setStatus('buffering');
      playHls(src);
      return;
    }

    if (recordingId) {
      await startRecordingPlayback(recordingId);
      return;
    }

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
          profileID: 'auto',
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
  }, [src, recordingId, sRef, apiBase, authHeaders, ensureSessionCookie, waitForSessionReady, playHls, sendStopIntent, t, duration, startRecordingPlayback]);

  const stopStream = useCallback(async (): Promise<void> => {
    if (hlsRef.current) hlsRef.current.destroy();
    if (videoRef.current) {
      videoRef.current.pause();
      videoRef.current.src = '';
    }
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
    if (onClose) onClose();
  }, [sessionId, sendStopIntent, onClose]);

  const handleRetry = useCallback(() => {
    stopStream().then(() => {
      startStream();
    });
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
      const err = videoEl.error;
      setStatus('error');
      setError(err?.message || t('player.error'));
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
  }, [t]);

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
      if (v.getVideoPlaybackQuality) {
        dropped = v.getVideoPlaybackQuality().droppedVideoFrames;
      } else if ((v as any).webkitDroppedFrameCount) {
        dropped = (v as any).webkitDroppedFrameCount;
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

    document.addEventListener('fullscreenchange', onFsChange);
    if (videoRef.current) {
      videoRef.current.addEventListener('enterpictureinpicture', onPipChange);
      videoRef.current.addEventListener('leavepictureinpicture', onPipChange);
    }

    return () => {
      document.removeEventListener('fullscreenchange', onFsChange);
      if (videoRef.current) {
        videoRef.current.removeEventListener('enterpictureinpicture', onPipChange);
        videoRef.current.removeEventListener('leavepictureinpicture', onPipChange);
      }
    };
  }, []);

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
      sendStopIntent(sessionIdRef.current, true);
    };
  }, [sendStopIntent]);

  // Overlay styles
  const containerStyle: React.CSSProperties = onClose ? {
    position: 'fixed',
    top: 0,
    left: 0,
    width: '100vw',
    height: '100vh',
    background: 'rgba(0,0,0,0.95)',
    zIndex: 9999,
    display: 'flex',
    flexDirection: 'column',
    justifyContent: 'center',
    alignItems: 'center',
  } : {};

  const videoStyle = {
    width: '100%',
    aspectRatio: '16/9',
    display: 'block'
  };

  const spinnerLabel = (status === 'starting' || status === 'priming' || status === 'buffering')
    ? `${t(`player.statusStates.${status}`, { defaultValue: status })}…`
    : '';

  const windowDuration = Math.max(0, seekableEnd - seekableStart);
  const relativePosition = Math.min(windowDuration, Math.max(0, currentPlaybackTime - seekableStart));
  const hasSeekWindow = windowDuration > 0;
  const isLiveMode = playbackMode === 'LIVE';
  const isAtLiveEdge = isLiveMode && windowDuration > 0 && Math.abs(seekableEnd - currentPlaybackTime) < 2;

  return (
    <div className="v3-player-container" style={containerStyle}>
      {onClose && (
        <button
          onClick={stopStream}
          className="close-btn"
          aria-label={t('player.closePlayer')}
        >
          ✕
        </button>
      )}

      {/* Stats Overlay */}
      {showStats && (
        <div className="stats-overlay">
          <div className="stats-row"><span className="stats-label">{t('player.status')}</span> <span className="stats-value" style={{ textTransform: 'capitalize' }}>{t(`player.statusStates.${status}`, { defaultValue: status })}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.resolution')}</span> <span className="stats-value">{stats.resolution}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.bandwidth')}</span> <span className="stats-value">{stats.bandwidth} kbps</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.bufferHealth')}</span> <span className="stats-value">{stats.bufferHealth}s</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.latency')}</span> <span className="stats-value">{stats.latency !== null ? stats.latency + 's' : '-'}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.fps')}</span> <span className="stats-value">{stats.fps}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.dropped')}</span> <span className="stats-value">{stats.droppedFrames}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.hlsLevel')}</span> <span className="stats-value">{stats.levelIndex}</span></div>
          <div className="stats-row"><span className="stats-label">{t('player.segDuration')}</span> <span className="stats-value">{stats.buffer}s</span></div>
        </div>
      )}

      <div className="video-wrapper">
        {channel && <h3 className="overlay-title">{channel.name}</h3>}

        {/* Buffering Spinner */}
        {(status === 'starting' || status === 'priming' || status === 'buffering') && (
          <div className="spinner-overlay">
            <div className="spinner"></div>
            <div className="spinner-label">{spinnerLabel}</div>
          </div>
        )}

        <video
          ref={videoRef}
          controls
          autoPlay={!!autoStart}
          muted={!!autoStart}
          style={videoStyle}
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
              <span className="vod-time">{formatClock(relativePosition)}</span>
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
              <span className="vod-time-total">{formatClock(windowDuration)}</span>
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
              className="bg-input"
              value={sRef}
              onChange={(e) => setSRef(e.target.value)}
              placeholder={t('player.serviceRefPlaceholder')}
              style={{ width: '300px' }}
            />
          )
        )}

        {!autoStart && !src && !recordingId && (
          <button
            className="btn-primary"
            onClick={() => startStream()}
            disabled={status === 'starting' || status === 'priming'}
          >
            ▶ {t('common.startStream')}
          </button>
        )}

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
          <button onClick={stopStream} className="btn-danger">
            ⏹ {t('common.stop')}
          </button>
        )}
      </div>
    </div>
  );
}

export default V3Player;
