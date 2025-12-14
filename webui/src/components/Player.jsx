
import React, { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

export default function Player({ streamUrl, onClose }) {
  const videoRef = useRef(null);
  const hlsRef = useRef(null);
  const [isMuted, setIsMuted] = useState(true);
  const [error, setError] = useState(null);
  const [reloadToken, setReloadToken] = useState(0);
  const [isBuffering, setIsBuffering] = useState(true);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    setError(null);
    video.setAttribute('playsinline', 'true');
    video.setAttribute('webkit-playsinline', 'true');
    video.autoplay = true;
    video.crossOrigin = 'anonymous';

    // setError(null); // Fix lint: Avoid setState in effect
    setIsBuffering(true);

    const canUseHlsJs = Hls.isSupported();
    const canUseNativeHls = !!video.canPlayType('application/vnd.apple.mpegurl');
    let cleanupNativeError;

    const startWithHlsJs = () => {
      if (hlsRef.current) return;
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: false,
      });
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

    const startNative = () => {
      video.src = streamUrl;
      video.load();
      video.play().catch(e => {
        if (e.name !== 'AbortError') console.warn("Autoplay failed:", e);
      });

      const handleNativeError = () => {
        console.warn("Native HLS failed, attempting hls.js fallback", video.error);
        if (canUseHlsJs) {
          startWithHlsJs();
        } else {
          setError("Stream konnte nicht gestartet werden. Tippe auf Retry.");
        }
      };

      video.addEventListener('error', handleNativeError);
      cleanupNativeError = () => video.removeEventListener('error', handleNativeError);
    };

    if (canUseHlsJs) {
      startWithHlsJs();
    } else if (canUseNativeHls) {
      startNative();
    } else {
      console.warn("HLS not supported in this browser.");
      setError("HLS wird von diesem Browser nicht unterstützt.");
    }

    return () => {
      if (cleanupNativeError) cleanupNativeError();
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      video.removeAttribute('src');
      video.load();
    };
  }, [streamUrl, reloadToken]);

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

  const handleWaiting = () => {
    const video = videoRef.current;
    if (!video) return;

    // Only attempt recovery if we are SUPPOSED to be playing
    if (!video.paused && hlsRef.current) {
      hlsRef.current.startLoad();
      // Only recover media error if strictly needed; startLoad is usually enough for stalls
      // hlsRef.current.recoverMediaError(); 
    }
    video.play().catch(() => { });
  };

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.addEventListener('waiting', handleWaiting);
    return () => video.removeEventListener('waiting', handleWaiting);
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
    >
      <video
        ref={videoRef}
        style={{ width: '100%', height: '100%', objectFit: 'contain', backgroundColor: 'black' }}
        controls={true}
        playsInline={true}
        webkit-playsinline="true"
        crossOrigin="anonymous"
        preload="auto"
        autoPlay
        muted={isMuted}
        onPlaying={() => setIsBuffering(false)}
        onCanPlay={() => setIsBuffering(false)}
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
          zIndex: 100000
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
