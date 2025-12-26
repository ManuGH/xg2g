import { useState, useEffect, useRef, useCallback } from 'react';
import Hls from 'hls.js';
import type { ErrorData } from 'hls.js';
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
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const mounted = useRef<boolean>(false);
  const sessionIdRef = useRef<string | null>(null);
  const stopSentRef = useRef<string | null>(null);
  const sessionCookieRef = useRef<SessionCookieState>({ token: null, pending: null });

  const sleep = (ms: number): Promise<void> =>
    new Promise(resolve => setTimeout(resolve, ms));

  const apiBase = (client.getConfig().baseUrl || '/api/v3').replace(/\/$/, '');

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


  const sendStopIntent = useCallback(async (idToStop: string | null): Promise<void> => {
    if (!idToStop || stopSentRef.current === idToStop) return;
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
      hls.loadSource(url);
      if (videoRef.current) {
        hls.attachMedia(videoRef.current);
      }
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        videoRef.current?.play().catch(e => console.warn("Autoplay failed", e));
        setStatus('playing');
      });
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
              break;
          }
        }
      });
    } else if (videoRef.current?.canPlayType('application/vnd.apple.mpegurl')) {
      videoRef.current.src = url;
      videoRef.current.addEventListener('loadedmetadata', () => {
        videoRef.current?.play();
        setStatus('playing');
      });
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
      setSessionId(null);
    }
    setStatus('idle');
    if (onClose) onClose();
  };


  useEffect(() => {
    const videoEl = videoRef.current;
    return () => {
      if (hlsRef.current) hlsRef.current.destroy();
      if (videoEl) {
        videoEl.pause();
        videoEl.src = '';
      }
      sendStopIntent(sessionIdRef.current);
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
    padding: '20px'
  } : {
    padding: '20px',
    color: '#fff'
  };

  return (
    <div className="v3-player-container" style={containerStyle}>
      {onClose && (
        <button
          onClick={stopStream}
          style={{
            position: 'absolute',
            top: '20px',
            right: '20px',
            background: 'transparent',
            border: 'none',
            color: '#fff',
            fontSize: '2rem',
            cursor: 'pointer',
            zIndex: 10000
          }}
        >
          ✕
        </button>
      )}

      {!onClose && <h2>📺 V3 Stream {channel ? ' - ' + channel.name : ''}</h2>}

      {/* Controls - Hide sRef input if channel provided */}
      <div className="controls" style={{ marginBottom: '20px', display: 'flex', gap: '10px', zIndex: 10000 }}>
        {!channel && (
          <input
            type="text"
            value={sRef}
            onChange={(e) => setSRef(e.target.value)}
            placeholder="Service Ref"
            style={{ padding: '8px', width: '300px', background: '#333', border: '1px solid #555', color: '#fff' }}
          />
        )}


        {!autoStart && (
          <button onClick={() => startStream()} disabled={status === 'starting' || status === 'playing'} style={{ padding: '8px 16px', background: '#2563eb', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}>
            Start
          </button>
        )}

        {/* If Not Overlay, show Stop button explicitly */}
        {!onClose && (
          <button onClick={stopStream} style={{ padding: '8px 16px', background: '#dc2626', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}>
            Stop
          </button>
        )}
      </div>

      {error && <div className="error" style={{ color: '#f87171', marginBottom: '10px', zIndex: 10000 }}>⚠ {error}</div>}
      {status !== 'idle' && status !== 'playing' && <div className="status" style={{ color: '#aaa', marginBottom: '10px', zIndex: 10000 }}>Status: {status}</div>}

      <div className="video-wrapper" style={{ width: '100%', maxWidth: '1280px', background: '#000', position: 'relative' }}>
        {channel && <h3 style={{ position: 'absolute', top: 10, left: 10, textShadow: '0 0 5px black', margin: 0, zIndex: 5, pointerEvents: 'none' }}>{channel.name}</h3>}
        <video
          ref={videoRef}
          controls
          autoPlay={!!autoStart}
          muted={!!autoStart}
          style={{ width: '100%', aspectRatio: '16/9', display: 'block' }}
        />
      </div>
    </div>
  );
}

export default V3Player;
