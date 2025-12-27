import { useState, useEffect, useRef, useCallback } from 'react';
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

function V3Player(props: V3PlayerProps) {
  const { token, autoStart, onClose } = props;
  const channel = props.channel;
  const src = props.src;

  const [sRef, setSRef] = useState<string>(
    channel?.service_ref || channel?.id || '1:0:19:283D:3FB:1:C00000:0:0:0:'
  );
  const profileId = 'auto';
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
  const [isPip, setIsPip] = useState(false);
  const [, setIsFullscreen] = useState(false);
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

  const apiBase = (client.getConfig().baseUrl || '/api/v3').replace(/\/$/, '');

  // --- Keyboard Shortcuts ---
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      // Guard against all input types
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
  }, []);

  // --- Video Event Listeners for Robust State ---
  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const onWaiting = () => setStatus('buffering');
    const onStalled = () => setStatus('buffering');
    const onSeeking = () => setStatus('buffering');

    // Clear error state on successful playback
    const onPlaying = () => {
      setStatus('playing');
      setError(null);
      setErrorDetails(null);
      setShowErrorDetails(false);
    };

    const onPause = () => setStatus(prev => (prev === 'error' ? prev : 'paused'));

    // Handle seeked: Remove spinner, respect paused state
    const onSeeked = () => {
      setStatus(prev => (prev === 'error' ? prev : (videoEl.paused ? 'paused' : 'playing')));
    };

    const onError = () => {
      const err = videoEl.error;
      setStatus('error');
      setError(err?.message || 'Video Playback Error');
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
  }, []);

  // --- Stats Polling ---
  useEffect(() => {
    if (!showStats) return;
    const interval = setInterval(() => {
      if (!videoRef.current) return;
      const v = videoRef.current;

      // Dropped Frames
      let dropped = 0;
      if (v.getVideoPlaybackQuality) {
        dropped = v.getVideoPlaybackQuality().droppedVideoFrames;
      } else if ((v as any).webkitDroppedFrameCount) {
        dropped = (v as any).webkitDroppedFrameCount;
      }

      // Buffer Health
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

      // Latency (Only in Live Mode)
      let lat: number | null = null;
      // Check for live session (sessionIdRef is safer) or hls liveSync
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

    }, 1000);
    return () => clearInterval(interval);
  }, [showStats]);


  // --- Player Controls ---
  const togglePlayPause = () => {
    if (!videoRef.current) return;
    if (videoRef.current.paused) {
      videoRef.current.play().catch(e => console.warn("Play failed", e));
    } else {
      videoRef.current.pause();
    }
  };

  const toggleFullscreen = async () => {
    if (!document.fullscreenElement) {
      try {
        await videoRef.current?.requestFullscreen();
        // State update handled by listener
      } catch (err) {
        console.warn("Fullscreen failed", err);
      }
    } else {
      await document.exitFullscreen();
    }
  };

  const togglePiP = async () => {
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
  };

  // Listen to fullscreen/pip changes
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


  useEffect(() => {
    if (channel) {
      const ref = channel.service_ref || channel.id;
      if (ref) setSRef(ref);
    }
  }, [channel]);

  // Direct Mode Effect
  useEffect(() => {
    if (src && autoStart && !mounted.current) {
      mounted.current = true;
      startStream();
    }
  }, [src, autoStart]);

  useEffect(() => {
    sessionIdRef.current = sessionId;
  }, [sessionId]);

  useEffect(() => {
    if (token) {
      client.setConfig({ headers: { Authorization: `Bearer ${token}` } });
    }
    sessionCookieRef.current.token = null;
    sessionCookieRef.current.pending = null;
  }, [token]);

  const ensureSessionCookie = useCallback(async (): Promise<void> => {
    if (!token) return;
    if (sessionCookieRef.current.token === token) return;
    if (sessionCookieRef.current.pending) return sessionCookieRef.current.pending;

    const pending = (async () => {
      try {
        client.setConfig({ headers: { Authorization: `Bearer ${token}` } });
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

  // Auto-start logic (Live Mode Only)
  useEffect(() => {
    if (autoStart && sRef && !mounted.current && !src) {
      mounted.current = true;
      startStream(sRef);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoStart, sRef, src]);


  const sendStopIntent = useCallback(async (idToStop: string | null, force: boolean = false): Promise<void> => {
    if (!idToStop) return;
    if (!force && stopSentRef.current === idToStop) return;
    stopSentRef.current = idToStop;
    try {
      await fetch(`${apiBase}/intents`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + token
        },
        body: JSON.stringify({
          type: 'stream.stop',
          sessionId: idToStop
        })
      });
    } catch (err) {
      console.warn('Failed to stop v3 session', err);
    }
  }, [apiBase, token]);

  const startStream = async (refToUse?: string): Promise<void> => {
    // Reset refs immediately to prevent stale stats/stops during transition
    sessionIdRef.current = null;
    stopSentRef.current = null;

    // Direct Mode: Bypass Session Logic
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

    try {
      // 0. Auth Preflight (Cookie Handshake for Native HLS/Safari)
      await ensureSessionCookie();

      // 1. Create Session
      const res = await fetch(`${apiBase}/intents`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + token
        },
        body: JSON.stringify({
          type: 'stream.start',
          profile: profileId,
          serviceRef: ref
        })
      });

      if (!res.ok) throw new Error('API Error: ' + res.status);
      const data: V3SessionResponse = await res.json();
      newSessionId = data.sessionId;
      sessionIdRef.current = newSessionId;
      setSessionId(newSessionId);
      setStatus('buffering');

      // 2. Wait for session to be READY
      await waitForSessionReady(newSessionId);

      // 3. Play HLS
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
  };


  const waitForPlaylistReady = async (sessionId: string, maxAttempts = 120): Promise<void> => {
    const playlistUrl = `${apiBase}/sessions/${sessionId}/hls/index.m3u8`;
    for (let i = 0; i < maxAttempts; i++) {
      const playlistRes = await fetch(playlistUrl, {
        headers: { 'Authorization': 'Bearer ' + token }
      });
      if (playlistRes.ok) {
        return;
      }
      await sleep(250);
    }
    throw new Error('Playlist file not ready');
  };

  const waitForSessionReady = async (sessionId: string, maxAttempts = 60): Promise<void> => {
    // First wait for READY state
    for (let i = 0; i < maxAttempts; i++) {
      try {
        const res = await fetch(`${apiBase}/sessions/${sessionId}`, {
          headers: {
            'Authorization': 'Bearer ' + token
          }
        });
        if (res.status === 404) {
          await sleep(500);
          continue;
        }
        if (!res.ok) throw new Error('Failed to fetch session');

        const session: V3SessionStatusResponse = await res.json();

        if (session.state === 'STOPPED') {
          throw new Error('Session failed: ' + (session.error || 'unknown error'));
        }

        if (session.state === 'READY') {
          await waitForPlaylistReady(sessionId);
          return;
        }

        // Wait 500ms before next attempt
        await sleep(500);
      } catch (err) {
        throw new Error('Session readiness check failed: ' + (err as Error).message);
      }
    }
    throw new Error('Session did not become ready in time');
  };

  const updateStats = (hls: Hls) => {
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
  };

  const playHls = (url: string): void => {
    if (Hls.isSupported()) {
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: true,
        liveSyncDurationCount: 2, // Sync close to edge (window is only 3 segs)
        liveMaxLatencyDurationCount: 3,
        maxBufferLength: 10, // Don't try to buffer 30s
        xhrSetup: (xhr) => {
          if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token);
        }
      });
      hlsRef.current = hls;

      // -- Stats Binding --
      hls.on(Hls.Events.LEVEL_SWITCHED, () => updateStats(hls));
      hls.on(Hls.Events.MANIFEST_PARSED, (_e, data: ManifestParsedData) => {
        updateStats(hls);
        // Set initial resolution/fps if available
        if (data.levels && data.levels.length > 0) {
          const first = data.levels[0];
          if (first) {
            setStats(prev => ({ ...prev, fps: first.frameRate || 0 }));
          }
        }
        videoRef.current?.play().catch(e => console.warn("Autoplay failed", e));
        // Note: We don't force 'playing' here anymore, waiting for video event
      });

      hls.on(Hls.Events.FRAG_LOADED, (_e, data: FragLoadedData) => {
        // Just approximate segment duration for stats
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
          // Try to recover
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
              setError('HLS Error: ' + data.type);
              setErrorDetails(JSON.stringify(data, null, 2));
              break;
          }
        }
      });
    } else if (videoRef.current?.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari Native
      videoRef.current.src = url;
      // Use { once: true } to prevent accumulation
      videoRef.current.addEventListener('loadedmetadata', () => {
        videoRef.current?.play();
      }, { once: true });
    }
  };

  const stopStream = async (): Promise<void> => {
    if (hlsRef.current) hlsRef.current.destroy();
    if (videoRef.current) {
      videoRef.current.pause();
      videoRef.current.src = '';
    }
    // Only send stop intent if we have a session (Live Mode)
    if (sessionId) {
      await sendStopIntent(sessionId);
    }
    // Hard reset refs
    sessionIdRef.current = null;
    stopSentRef.current = null;
    setSessionId(null);
    setStatus('stopped');
    if (onClose) onClose();
  };

  // Retry logic
  const handleRetry = () => {
    stopStream().then(() => {
      startStream();
    });
  };


  useEffect(() => {
    const videoEl = videoRef.current;
    return () => {
      if (hlsRef.current) hlsRef.current.destroy();
      if (videoEl) {
        videoEl.pause();
        videoEl.src = '';
      }
      // Force stop on unmount to ensure no zombie sessions
      sendStopIntent(sessionIdRef.current, true);
    };
  }, [sendStopIntent]);

  // Overlay styles if onClose is present
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
  } : {
    // Component mode
  };

  // Inside component logic to show video width
  const videoStyle = {
    width: '100%',
    aspectRatio: '16/9',
    display: 'block'
  };


  return (
    <div className="v3-player-container" style={containerStyle}>
      {onClose && (
        <button
          onClick={stopStream}
          className="close-btn"
          aria-label="Close Player"
        >
          ✕
        </button>
      )}

      {/* Stats Overlay */}
      {showStats && (
        <div className="stats-overlay">
          <div className="stats-row"><span className="stats-label">Status</span> <span className="stats-value" style={{ textTransform: 'capitalize' }}>{status}</span></div>
          <div className="stats-row"><span className="stats-label">Resolution</span> <span className="stats-value">{stats.resolution}</span></div>
          <div className="stats-row"><span className="stats-label">Bandwidth</span> <span className="stats-value">{stats.bandwidth} kbps</span></div>
          <div className="stats-row"><span className="stats-label">Buffer Health</span> <span className="stats-value">{stats.bufferHealth}s</span></div>
          <div className="stats-row"><span className="stats-label">Latency</span> <span className="stats-value">{stats.latency !== null ? stats.latency + 's' : '-'}</span></div>
          <div className="stats-row"><span className="stats-label">FPS</span> <span className="stats-value">{stats.fps}</span></div>
          <div className="stats-row"><span className="stats-label">Dropped</span> <span className="stats-value">{stats.droppedFrames}</span></div>
          <div className="stats-row"><span className="stats-label">HLS Level</span> <span className="stats-value">{stats.levelIndex}</span></div>
          <div className="stats-row"><span className="stats-label">Seg Duration</span> <span className="stats-value">{stats.buffer}s</span></div>
        </div>
      )}

      <div className="video-wrapper">
        {channel && <h3 className="overlay-title">{channel.name}</h3>}

        {/* Buffering Spinner */}
        {(status === 'starting' || status === 'buffering') && (
          <div className="spinner-overlay">
            <div className="spinner"></div>
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
            <button onClick={handleRetry} className="btn-retry">Retry</button>
          </div>
          {errorDetails && (
            <button
              onClick={() => setShowErrorDetails(!showErrorDetails)}
              className="error-details-btn"
            >
              {showErrorDetails ? 'Hide Details' : 'Show Details'}
            </button>
          )}
          {showErrorDetails && errorDetails && (
            <div className="error-details-content">
              {errorDetails}
              <br />
              Session: {sessionIdRef.current || 'N/A'}
            </div>
          )}
        </div>
      )}

      {/* Controls & Status Bar */}
      <div className="v3-player-controls-header">
        {!channel && (
          <input
            type="text"
            className="bg-input"
            value={sRef}
            onChange={(e) => setSRef(e.target.value)}
            placeholder="Service Ref (1:0:1...)"
            style={{ width: '300px' }}
          />
        )}

        {!autoStart && (
          <button
            className="btn-primary"
            onClick={() => startStream()}
            disabled={status === 'starting'}
          >
            ▶ Start Stream
          </button>
        )}

        <button
          className={`btn-icon ${isPip ? 'active' : ''}`}
          onClick={togglePiP}
          title="Picture-in-Picture (p)"
        >
          📺 PiP
        </button>

        <button
          className={`btn-icon ${showStats ? 'active' : ''}`}
          onClick={() => setShowStats(!showStats)}
          title="Stats for Nerds (i)"
        >
          📊 Stats
        </button>

        {/* If Not Overlay, show Stop button explicitly */}
        {!onClose && (
          <button onClick={stopStream} className="btn-danger">
            ⏹ Stop
          </button>
        )}

        <span style={{ fontSize: '12px', color: '#aaa', marginLeft: 'auto' }}>
          Space (Play/Pause) • F (Full) • M (Mute) • P (PiP) • I (Stats)
        </span>
      </div>
    </div>
  );
}

export default V3Player;
