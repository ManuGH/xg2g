
import React, { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

export default function Player({ streamUrl, onClose }) {
  const videoRef = useRef(null);
  const hlsRef = useRef(null);
  const [isMuted, setIsMuted] = useState(true);
  const [error, setError] = useState(null);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const forcePlay = () => {
      video.play().catch(() => {});
    };

    // Reset error logic handled by parent key change or initial state
    // setError(null);
    video.setAttribute('playsinline', 'true');
    video.setAttribute('webkit-playsinline', 'true');

    setError(null);

    // iOS / Native HLS Support
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = streamUrl;
      video.play().catch(e => {
        console.warn("Autoplay failed:", e);
        // Usually fails if unmuted, but we start muted.
        // If it fails, show a "Click to Play" overlay?
      });
    }
    // HLS.js Support (Desktop)
    else if (Hls.isSupported()) {
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: false, // Server-side LL-HLS disabled; stick to robust MPEG-TS HLS
      });
      hlsRef.current = hls;

      hls.attachMedia(video);
      hls.loadSource(streamUrl);

      // Ensure playback kicks off once manifest is ready (avoids freeze on first frame)
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        hls.startLoad();
        video.play().catch(err => console.warn("Play failed after manifest parse:", err));
      });
      hls.on(Hls.Events.MEDIA_ATTACHED, () => {
        // Some browsers need a play attempt right after attach
        video.play().catch(() => {});
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
    } else {
      console.warn("HLS not supported in this browser.");
      // setError("HLS not supported in this browser."); // Avoid effect sync update
    }

    video.addEventListener('canplay', forcePlay);

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
      video.removeEventListener('canplay', forcePlay);
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
    handleUnmute();
    if (videoRef.current) {
      videoRef.current.play().catch(() => {});
    }
  };

  // Guard against stalls: if the player fires "waiting", nudge playback/load
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const handleWaiting = () => {
      if (hlsRef.current) {
        hlsRef.current.startLoad();
        hlsRef.current.recoverMediaError();
      }
      video.play().catch(() => {});
    };

    video.addEventListener('waiting', handleWaiting);
    return () => {
      video.removeEventListener('waiting', handleWaiting);
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
    >
      <video
        ref={videoRef}
        style={{ width: '100%', height: '100%', objectFit: 'contain', backgroundColor: 'black' }}
        controls={true}
        playsInline={true}
        webkit-playsinline="true"
        autoPlay
        muted={isMuted}
      />

      {/* Close Button */}
      <button
        onClick={onClose}
        style={{
          position: 'absolute',
          top: '20px',
          right: '20px',
          background: 'rgba(0,0,0,0.5)',
          color: 'white',
          border: 'none',
          padding: '10px 20px',
          fontSize: '18px',
          cursor: 'pointer',
          borderRadius: '5px',
          zIndex: 100000
        }}
      >
        ✕ Close
      </button>

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
            cursor: 'pointer'
          }}
        >
          Tap to Unmute 🔊
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
