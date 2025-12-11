
import React, { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

export default function Player({ streamUrl, onClose }) {
  const videoRef = useRef(null);
  const hlsRef = useRef(null);
  const [isMuted, setIsMuted] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    // Reset error logic handled by parent key change or initial state
    // setError(null);

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
        lowLatencyMode: true,
      });
      hlsRef.current = hls;

      hls.loadSource(streamUrl);
      hls.attachMedia(video);

      hls.on(Hls.Events.ERROR, (event, data) => {
        if (data.fatal) {
          console.error("HLS Fatal Error:", data);
          // setError("Stream Connection Failed"); // Avoid direct layout shift
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              hls.recoverMediaError();
              break;
            default:
              hls.destroy();
              break;
          }
        }
      });
    } else {
      console.warn("HLS not supported in this browser.");
      // setError("HLS not supported in this browser."); // Avoid effect sync update
    }

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
    };
  }, [streamUrl]);

  const handleUnmute = () => {
    if (videoRef.current) {
      videoRef.current.muted = false;
      setIsMuted(false);
    }
  };

  return (
    <div
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
        style={{ width: '100%', height: '100%', objectFit: 'contain' }}
        controls
        playsInline
        autoPlay
        muted
        onClick={handleUnmute}
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
          onClick={handleUnmute}
          style={{
            position: 'absolute',
            bottom: '100px',
            background: 'rgba(0,0,0,0.6)',
            color: 'white',
            padding: '10px 20px',
            borderRadius: '20px',
            pointerEvents: 'none' // Let clicks pass to video
          }}
        >
          Tap to Unmute 🔊
        </div>
      )}

      {error && (
        <div style={{ position: 'absolute', color: 'red', background: 'rgba(0,0,0,0.8)', padding: '20px' }}>
          {error}
        </div>
      )}
    </div>
  );
}
