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
  const channel = props.channel;
  const src = props.src;

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
  const [isPip, setIsPip] = useState(false);
  const [, setIsFullscreen] = useState(false);
  const [seekOffset, setSeekOffset] = useState(0); // For VOD seeking offset in ms
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

  const assertPlaylistReady = useCallback(async (sid: string): Promise<void> => {
    const playlistUrl = `${apiBase}/sessions/${sid}/hls/index.m3u8`;
    const playlistRes = await fetch(playlistUrl, {
      headers: authHeaders()
    });
    if (!playlistRes.ok) {
      throw new Error(t('player.readyButNotPlayable'));
    }
  }, [apiBase, authHeaders, t]);

  const waitForSessionReady = useCallback(async (sid: string, maxAttempts = 60): Promise<void> => {
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
        const sState = session.state;

        if (sState === 'FAILED' || sState === 'STOPPED' || sState === 'CANCELLED' || sState === 'DRAINING' || sState === 'STOPPING') {
          const reason = session.reason || sState;
          const detail = session.reasonDetail ? `: ${session.reasonDetail}` : '';
          throw new Error(`${t('player.sessionFailed')}: ${reason}${detail}`);
        }

        if (sState === 'READY') {
          setStatus('ready');
          await assertPlaylistReady(sid);
          return;
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
  }, [apiBase, authHeaders, t, assertPlaylistReady]);

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

  const startStream = useCallback(async (refToUse?: string, startMs: number = 0): Promise<void> => {
    sessionIdRef.current = null;
    stopSentRef.current = null;

    if (src) {
      setStatus('buffering');
      playHls(src);
      return;
    }

    const ref = refToUse || sRef;
    let newSessionId: string | null = null;
    setStatus('starting');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);
    setSeekOffset(startMs); // Update offset

    try {
      await ensureSessionCookie();

      const res = await fetch(`${apiBase}/intents`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({
          type: 'stream.start',
          profile: 'auto',
          serviceRef: ref,
          startMs: startMs > 0 ? startMs : undefined
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
      await waitForSessionReady(newSessionId);

      setStatus('ready');
      const streamUrl = `${apiBase}/sessions/${newSessionId}/hls/index.m3u8`;
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
  }, [src, sRef, apiBase, authHeaders, ensureSessionCookie, waitForSessionReady, playHls, sendStopIntent, t]);

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
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [toggleFullscreen, togglePlayPause, togglePiP]);

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
      const isLive = !!sessionIdRef.current || (hlsRef.current && hlsRef.current.liveSyncPosition);

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

      // Update generic playback time for scrubber
      if (!videoRef.current.paused) {
        setCurrentPlaybackTime(videoRef.current.currentTime);
      }
    }, 1000);
    return () => clearInterval(interval);
  }, [showStats]);

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

  // Direct Mode / AutoStart Effect
  useEffect(() => {
    if (src && autoStart && !mounted.current) {
      mounted.current = true;
      startStream();
    }
  }, [src, autoStart, startStream]);

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

  // Handle sRef auto-start (Live Mode)
  useEffect(() => {
    if (autoStart && sRef && !mounted.current && !src) {
      mounted.current = true;
      startStream(sRef);
    }
  }, [autoStart, sRef, src, startStream]);

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
        {/* VOD Scrubber (If duration is present) */}
        {duration && duration > 0 ? (
          <div className="vod-controls" style={{ flex: 1, display: 'flex', alignItems: 'center', marginRight: '10px' }}>
            <span style={{ fontSize: '12px', color: '#fff', marginRight: '8px', minWidth: '45px' }}>
              {new Date((seekOffset + (currentPlaybackTime * 1000))).toISOString().substr(11, 8)}
            </span>
            <input
              type="range"
              min="0"
              max={duration}
              step="1"
              value={Math.min(duration, (seekOffset / 1000) + currentPlaybackTime)}
              onChange={(e) => {
                const newVal = parseInt(e.target.value, 10);
                // Calculate new startMs: (NewSeconds * 1000)
                // We restart the stream at this absolute offset
                startStream(undefined, newVal * 1000);
              }}
              style={{ flex: 1, cursor: 'pointer' }}
            />
            <span style={{ fontSize: '12px', color: '#aaa', marginLeft: '8px', minWidth: '45px' }}>
              {new Date(duration * 1000).toISOString().substr(11, 8)}
            </span>
          </div>
        ) : (
          !channel && (
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

        {!autoStart && !duration && (
          <button
            className="btn-primary"
            onClick={() => startStream()}
            disabled={status === 'starting' || status === 'priming'}
          >
            ▶ {t('common.startStream')}
          </button>
        )}

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

        {/* Hide Jump button in VOD mode (replaced by scrubber) */}
        {!duration && (
          <span style={{ fontSize: '12px', color: '#aaa', marginLeft: 'auto', marginRight: '10px' }}>
            {t('player.hotkeysHint')}
          </span>
        )}

        {/* Legacy Jump Control (Only for non-VOD / manual ref input) */}
        {!duration && (
          <button
            className="btn-secondary"
            onClick={() => {
              const message = t('player.jumpToPrompt', 'Jump to MM:SS or integer minutes:') || 'Jump to MM:SS or integer minutes:';
              const input = prompt(message);
              if (!input) return;
              let ms = 0;
              if (input.includes(':')) {
                const parts = input.split(':');
                const m = parseInt(parts[0] || '0', 10) || 0;
                const s = parseInt(parts[1] || '0', 10) || 0;
                ms = (m * 60 + s) * 1000;
              } else {
                ms = parseInt(input, 10) * 60 * 1000;
              }
              if (!isNaN(ms) && ms >= 0) {
                startStream(undefined, ms);
              }
            }}
          >
            ⏩ {t('player.jumpLabel', 'Jump')}
          </button>
        )}
      </div>

      {/* Time Overlay - Hide in VOD mode as scrubber has explicit time */}
      {!duration && (
        <div className="time-overlay" style={{
          position: 'absolute',
          top: '10px',
          right: '10px',
          background: 'rgba(0,0,0,0.5)',
          color: 'white',
          padding: '4px 8px',
          borderRadius: '4px',
          fontSize: '14px',
          pointerEvents: 'none'
        }}>
          ⏱ {(() => {
            // We prefer stats.latency if available for live, but for VOD (recordings) we want absolute time.
            // Since we don't distinguish mode easily here yet (unless we check channel vs recording),
            // we'll show just current time if seekOffset is 0 (Live default)
            if (videoRef.current && sessionIdRef.current) {
              const current = videoRef.current.currentTime;
              const totalSeconds = (seekOffset / 1000) + current;
              const h = Math.floor(totalSeconds / 3600);
              const m = Math.floor((totalSeconds % 3600) / 60);
              const s = Math.floor(totalSeconds % 60);
              const pad = (n: number) => n.toString().padStart(2, '0');
              return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
            }
            return '--:--';
          })()}
        </div>
      )}

    </div>
  );
}

export default V3Player;
