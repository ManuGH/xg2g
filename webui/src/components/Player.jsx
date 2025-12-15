
import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

function derivePreflightUrl(streamUrl) {
  try {
    const url = new URL(streamUrl, window.location.origin);
    const parts = url.pathname.split('/').filter(Boolean);
    if (parts.length !== 3) return null;
    const [prefix, streamId, file] = parts;
    if (prefix !== 'stream' || file !== 'playlist.m3u8') return null;
    url.pathname = `/stream/${streamId}/preflight`;
    return url.pathname + url.search;
  } catch {
    return null;
  }
}

export default function Player({ streamUrl, onClose }) {
  const videoRef = useRef(null);
  const hlsRef = useRef(null);
  const safariNativeFailedRef = useRef(false);
  const safariAutoRetriedRef = useRef(false);
  const attemptStartedAtRef = useRef(0);
  const [isMuted, setIsMuted] = useState(false);
  const [error, setError] = useState(null);
  const [reloadToken, setReloadToken] = useState(0);
  const [isBuffering, setIsBuffering] = useState(true);
  const [showControls, setShowControls] = useState(true);
  const [showSafariStart, setShowSafariStart] = useState(false);
  const hideControlsTimeoutRef = useRef(null);
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent || '' : '';
  const isSafari = /safari/i.test(ua) && !/chrome|crios|fxios|edg/i.test(ua);

  useEffect(() => {
    safariAutoRetriedRef.current = false;
  }, [streamUrl]);

  const clearHideControls = () => {
    if (hideControlsTimeoutRef.current) {
      clearTimeout(hideControlsTimeoutRef.current);
      hideControlsTimeoutRef.current = null;
    }
  };

  const scheduleHideControls = () => {
    clearHideControls();
    if (isBuffering || error) {
      setShowControls(true);
      return;
    }
    hideControlsTimeoutRef.current = setTimeout(() => setShowControls(false), 2000);
  };

  const handleUserActivity = () => {
    setShowControls(true);
    scheduleHideControls();
  };

  useLayoutEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const abortController = new AbortController();
    let isCancelled = false;

    // Disable hls.js fallback on Safari to avoid SourceBuffer errors; native-only
    const safariFallbackEnabled = false;

    setError(null);
    video.setAttribute('playsinline', 'true');
    video.setAttribute('webkit-playsinline', 'true');
    video.setAttribute('preload', 'metadata');
    video.autoplay = false;
    video.crossOrigin = 'anonymous';
    video.muted = false;
    video.defaultMuted = false;

    // setError(null); // Fix lint: Avoid setState in effect
    setIsBuffering(true);
    attemptStartedAtRef.current = Date.now();

    const hasMse = typeof window !== 'undefined' && !!window.MediaSource;
    const canUseNativeHls = !!video.canPlayType('application/vnd.apple.mpegurl');
    // Prefer native HLS on Safari to avoid SourceBuffer quirks (audio buffer removal)
    const preferNativeFirst = isSafari && canUseNativeHls && !safariNativeFailedRef.current;
    // Use hls.js on MSE-capable browsers; Safari fallback is disabled unless explicitly enabled
    const canUseHlsJs = (!isSafari && Hls.isSupported() && hasMse) || (isSafari && safariFallbackEnabled && Hls.isSupported() && hasMse);
    let cleanupNativeError;

    const kickPreflight = () => {
      const preflightUrl = derivePreflightUrl(streamUrl);
      if (!preflightUrl) return;

      fetch(preflightUrl, {
        method: 'GET',
        signal: abortController.signal,
        cache: 'no-store',
        headers: { 'Accept': 'application/json' },
      }).catch((err) => {
        if (isCancelled) return;
        if (err && (err.name === 'AbortError' || err.code === 20)) return;
        console.warn('Stream preflight failed', err);
      });
    };

    const startWithHlsJs = () => {
      if (hlsRef.current) return;
      let hls;
      try {
        hls = new Hls({
          debug: false,
          enableWorker: isSafari ? false : true, // Safari+workers can be flaky
          lowLatencyMode: false,
        });
      } catch (err) {
        console.warn("Failed to init hls.js, falling back to native HLS", err);
        if (canUseNativeHls) startNative(true);
        return;
      }
      hlsRef.current = hls;

      // Ensure previous native src is cleared before attaching MSE
      video.pause();
      video.removeAttribute('src');
      video.load();

      hls.attachMedia(video);
      hls.loadSource(streamUrl);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        hls.startLoad();
        video.play().catch(e => {
          if (e.name !== 'AbortError') console.warn("Play failed after manifest parse:", e);
        });
      });

      hls.on(Hls.Events.ERROR, (event, data) => {
        if (
          isSafari &&
          canUseNativeHls &&
          (data.details === Hls.ErrorDetails.BUFFER_APPEND_ERROR || data.details === Hls.ErrorDetails.BUFFER_APPENDING_ERROR)
        ) {
          console.warn("Safari audio buffer issue detected", data);
          hls.destroy();
          hlsRef.current = null;
          if (!safariNativeFailedRef.current) {
            setError("Safari: Audiopuffer-Problem – wechsle auf nativen Player …");
            startNative(true);
          } else {
            setError("Safari kann den Audiopuffer nicht verarbeiten. Tippe auf Retry (hls.js-Modus).");
          }
          return;
        }

        if (data.fatal) {
          console.error("HLS Fatal Error:", data);
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              setError("Netzwerkproblem – erneuter Verbindungsversuch …");
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              setError("Wiedergabe-Problem – versuche Recovery …");
              hls.recoverMediaError();
              break;
            default:
              setError("Stream konnte nicht gestartet werden. Tippe auf Retry.");
              hls.destroy();
              break;
          }
        }
      });
    };

    const startNative = (skipErrorListener = false, skipAutoplay = false) => {
      video.src = streamUrl;
      video.load();

      if (!skipAutoplay) {
        video.play().catch((e) => {
          if (e.name !== 'AbortError') console.warn("Autoplay failed:", e);
          if (isSafari && e && e.name === "NotAllowedError") {
            setShowSafariStart(true);
            setError(null);
            setIsBuffering(false);
            return;
          }
          setError("Stream konnte nicht gestartet werden. Tippe auf Retry.");
        });
      }

      if (!skipErrorListener) {
        const handleNativeError = () => {
          console.warn("Native HLS failed", video.error);
          safariNativeFailedRef.current = true;
          const code = video.error && typeof video.error.code === 'number' ? video.error.code : null;

          // One gentle auto-retry for Safari: the first request can fail while the backend is still
          // warming up ffmpeg/segments. This avoids the user needing to close+reopen.
          if (isSafari && !safariFallbackEnabled) {
            const sinceStartMs = Date.now() - (attemptStartedAtRef.current || 0);
            if (!safariAutoRetriedRef.current && sinceStartMs < 20_000) {
              safariAutoRetriedRef.current = true;
              setError(null);
              setIsBuffering(true);
              // Force a clean reload of the media element.
              try {
                video.pause();
                video.removeAttribute('src');
                video.load();
              } catch { }
              setTimeout(() => setReloadToken((t) => t + 1), 800);
              return;
            }
          }

          if (isSafari && !safariFallbackEnabled) {
            if (code === 2) {
              setError("Netzwerkproblem – Stream startet noch. Tippe auf Retry.");
            } else if (code === 3) {
              setError("Safari konnte den Stream nicht decodieren (native). Tippe auf Retry.");
            } else {
              setError("Safari konnte den Stream nicht starten (native). Tippe auf Retry.");
            }
            return;
          }
          if (canUseHlsJs) {
            startWithHlsJs();
          } else {
            setError("Stream konnte nicht gestartet werden. Tippe auf Retry.");
          }
        };

        video.addEventListener('error', handleNativeError);
        cleanupNativeError = () => video.removeEventListener('error', handleNativeError);
      }
    };

    // Best-effort warm-up: do NOT await on Safari, otherwise we lose the user gesture and iOS will mute/block audio.
    kickPreflight();

    if (isSafari) {
      // Force native on Safari to avoid hls.js SourceBuffer issues.
      // Do NOT try to autoplay on Safari to avoid "start muted" / race conditions.
      // Instead, show the start overlay immediately and wait for user gesture.
      startNative(false, true); // true = skipAutoplay
      setShowSafariStart(true);
      setError(null);
      setIsBuffering(false);
    } else if (preferNativeFirst) {
      startNative();
    } else if (canUseHlsJs) {
      startWithHlsJs();
    } else if (canUseNativeHls) {
      startNative();
    } else {
      console.warn("HLS not supported in this browser.");
      setError("HLS wird von diesem Browser nicht unterstützt.");
    }

    return () => {
      isCancelled = true;
      abortController.abort();
      if (cleanupNativeError) cleanupNativeError();
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      video.removeAttribute('src');
      video.load();
    };
  }, [streamUrl, reloadToken, isSafari]);

  useEffect(() => {
    scheduleHideControls();
    return clearHideControls;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isBuffering, error]);

  const handleUnmute = () => {
    if (videoRef.current) {
      videoRef.current.muted = false;
      setIsMuted(false);
    }
  };

  const handleUnmuteClick = (e) => {
    e.stopPropagation();
    e.preventDefault();
    handleUnmute();
    if (videoRef.current) {
      videoRef.current.play().catch(() => { });
    }
  };

  const handleReload = () => {
    setError(null);
    setIsBuffering(true);
    setShowSafariStart(false);
    if (isSafari && videoRef.current) {
      videoRef.current.muted = false;
      setIsMuted(false);
    }
    setReloadToken((t) => t + 1);
  };

  const toggleMute = () => {
    if (videoRef.current) {
      const nextMuted = !videoRef.current.muted;
      videoRef.current.muted = nextMuted;
      setIsMuted(nextMuted);
      if (!nextMuted) {
        videoRef.current.play().catch(() => { });
      }
    }
  };

  const handleReady = () => {
    setIsBuffering(false);
    scheduleHideControls();
  };

  const handleSafariStart = () => {
    const video = videoRef.current;
    if (!video) return;

    setShowSafariStart(false);
    setError(null);
    setIsBuffering(true);

    // Use the user gesture to request audio immediately.
    setIsMuted(false);
    video.muted = false;
    video.play().catch((e) => {
      if (e && e.name === 'NotAllowedError') {
        setShowSafariStart(true);
        setIsBuffering(false);
        return;
      }
      console.warn('Safari play failed', e);
      setError('Stream konnte nicht gestartet werden. Tippe auf Retry.');
    });
  };

  const handleWaiting = () => {
    const video = videoRef.current;
    if (!video) return;

    // Only attempt recovery for hls.js/MSE playback.
    if (!hlsRef.current) return;
    if (!video.paused) {
      hlsRef.current.startLoad();
    }
    video.play().catch(() => { });
  };

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.addEventListener('waiting', handleWaiting);
    const canPlayHandler = () => {
      if (video.paused) {
        video.play().catch((e) => {
          if (e.name !== 'AbortError') console.warn('Play on canplay failed', e);
        });
      }
    };
    video.addEventListener('canplay', canPlayHandler);
    video.addEventListener('loadedmetadata', handleReady);
    video.addEventListener('playing', handleReady);
    video.addEventListener('play', handleReady);
    const volumeHandler = () => setIsMuted(!!video.muted);
    video.addEventListener('volumechange', volumeHandler);
    return () => {
      video.removeEventListener('waiting', handleWaiting);
      video.removeEventListener('canplay', canPlayHandler);
      video.removeEventListener('loadedmetadata', handleReady);
      video.removeEventListener('playing', handleReady);
      video.removeEventListener('play', handleReady);
      video.removeEventListener('volumechange', volumeHandler);
    };
  }, []);

  return (
    <div
      className="player-overlay"
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        width: '100vw',
        height: '100vh',
        backgroundColor: 'black',
        zIndex: 99999,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center'
      }}
      onMouseMove={handleUserActivity}
      onTouchStart={handleUserActivity}
    >
      <video
        ref={videoRef}
        style={{ width: '100%', height: '100%', objectFit: 'contain', backgroundColor: 'black' }}
        controls={true}
        playsInline={true}
        webkit-playsinline="true"
        crossOrigin="anonymous"
        preload="metadata"
        muted={isMuted}
        onPlaying={handleReady}
        onCanPlay={handleReady}
        onWaiting={() => setIsBuffering(true)}
      />

      {/* Top Controls */}
      <div
        style={{
          position: 'absolute',
          top: 16,
          right: 16,
          display: 'flex',
          gap: 8,
          zIndex: 100000,
          opacity: showControls ? 1 : 0,
          pointerEvents: showControls ? 'auto' : 'none',
          transition: 'opacity 0.2s ease'
        }}
      >
        <button
          onClick={toggleMute}
          style={{
            background: 'rgba(0,0,0,0.5)',
            color: 'white',
            border: '1px solid rgba(255,255,255,0.2)',
            padding: '8px 12px',
            fontSize: '14px',
            cursor: 'pointer',
            borderRadius: '999px'
          }}
        >
          {isMuted ? '🔇 Unmute' : '🔊 Mute'}
        </button>
        <button
          onClick={onClose}
          style={{
            background: 'rgba(255,255,255,0.1)',
            color: 'white',
            border: '1px solid rgba(255,255,255,0.2)',
            padding: '8px 12px',
            fontSize: '14px',
            cursor: 'pointer',
            borderRadius: '999px'
          }}
        >
          ✕ Close
        </button>
      </div>

      {/* Unmute Overlay (if muted) */}
      {isMuted && (
        <div
          onClick={handleUnmuteClick}
          style={{
            position: 'absolute',
            bottom: '100px',
            background: 'rgba(0,0,0,0.6)',
            color: 'white',
            padding: '10px 20px',
            borderRadius: '20px',
            pointerEvents: 'auto',
            cursor: 'pointer',
            boxShadow: '0 6px 20px rgba(0,0,0,0.35)'
          }}
        >
          Tap to Unmute 🔊
        </div>
      )}

      {/* Safari Start Overlay */}
      {showSafariStart && isSafari && (
        <div
          onClick={handleSafariStart}
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(0,0,0,0.5)',
            color: 'white',
            fontSize: '16px',
            cursor: 'pointer',
            zIndex: 120000,
          }}
        >
          Tippe, um zu starten
        </div>
      )}

      {isBuffering && !error && (
        <div
          style={{
            position: 'absolute',
            top: '50%',
            left: '50%',
            transform: 'translate(-50%, -50%)',
            color: 'white',
            background: 'rgba(0,0,0,0.5)',
            padding: '12px 16px',
            borderRadius: '12px',
            fontSize: '14px',
            display: 'flex',
            alignItems: 'center',
            gap: '10px'
          }}
        >
          <span
            style={{
              width: '14px',
              height: '14px',
              border: '2px solid rgba(255,255,255,0.6)',
              borderTopColor: 'white',
              borderRadius: '50%',
              display: 'inline-block',
              animation: 'spin 1s linear infinite'
            }}
          />
          Buffering …
        </div>
      )}

      {error && (
        <div style={{ position: 'absolute', color: 'red', background: 'rgba(0,0,0,0.8)', padding: '20px', borderRadius: '12px', bottom: '20%', textAlign: 'center' }}>
          <div style={{ marginBottom: '10px' }}>{error}</div>
          <button
            style={{ padding: '8px 16px', borderRadius: '8px', border: 'none', cursor: 'pointer' }}
            onClick={() => {
              setError(null);
              setReloadToken(t => t + 1); // Re-init HLS
            }}
          >
            Retry
          </button>
        </div>
      )}
    </div>
  );
}
