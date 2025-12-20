// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';
import Plyr from 'plyr';
import 'plyr/dist/plyr.css';

// Styling for the Close Button overlay since custom controls in Plyr can be tricky
const closeButtonStyle = {
  position: 'absolute',
  top: '20px',
  right: '20px',
  zIndex: 10000,
  background: 'rgba(255, 255, 255, 0.2)',
  backdropFilter: 'blur(10px)',
  border: '1px solid rgba(255, 255, 255, 0.3)',
  color: 'white',
  padding: '8px 16px',
  borderRadius: '20px',
  cursor: 'pointer',
  fontWeight: '600',
  fontSize: '14px',
  transition: 'all 0.2s ease',
  boxShadow: '0 4px 6px rgba(0, 0, 0, 0.1)',
};

export default function Player({ streamUrl, preflightUrl, onClose }) {
  const videoRef = useRef(null);
  const playerRef = useRef(null);
  const hlsRef = useRef(null);
  const [error, setError] = useState(null);

  // Helper to get Auth Token
  const getAuthToken = () => localStorage.getItem('XG2G_API_TOKEN');

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    setError(null);
    let hls = null;
    let player = null;

    const initPlayer = async () => {
      // 1. Kick Preflight (Auth Cookie/Session Warmup)
      if (preflightUrl) {
        try {
          // Wait for preflight to ensure Cookie is set
          await fetch(preflightUrl, { method: 'GET', cache: 'no-store', credentials: 'omit' });
        } catch (err) {
          console.warn('Preflight failed', err);
        }
      }

      const source = streamUrl;

      // 2. Initialize HLS.js if supported and not Safari (native rules)
      const isSafari = /^((?!chrome|android).)*safari/i.test(navigator.userAgent);

      if (Hls.isSupported() && !isSafari) {
        hls = new Hls({
          debug: false,
          enableWorker: true,
          lowLatencyMode: true,
          xhrSetup: (xhr) => {
            const token = getAuthToken();
            if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
          }
        });
        hls.loadSource(source);
        hls.attachMedia(video);
        hlsRef.current = hls;

        hls.on(Hls.Events.ERROR, (event, data) => {
          if (data.fatal) {
            console.error("HLS Fatal Error:", data);
            switch (data.type) {
              case Hls.ErrorTypes.NETWORK_ERROR:
                hls.startLoad();
                break;
              case Hls.ErrorTypes.MEDIA_ERROR:
                hls.recoverMediaError();
                break;
              default:
                hls.destroy();
                setError(`Playback Error: ${data.details}`);
                break;
            }
          }
        });
      } else {
        // Native HLS (Safari/iOS)
        video.src = source;
      }

      // 3. Initialize Plyr
      player = new Plyr(video, {
        controls: [
          'play-large', 'play', 'live', 'progress', 'mute', 'volume', 'pip', 'airplay', 'fullscreen',
        ],
        autoplay: true,
        muted: true,
        hideControls: true,
        keyboard: { focused: true, global: true },
      });

      playerRef.current = player;

      player.on('ready', () => {
        var playPromise = player.play();
        if (playPromise !== undefined) {
          playPromise.catch(e => {
            console.warn("Autoplay blocked", e);
          });
        }
      });

      player.on('error', (e) => {
        console.warn("Plyr warning/error:", e);
      });
    };

    initPlayer();

    // Cleanup
    return () => {
      if (hls) {
        hls.destroy();
      }
      if (player) {
        player.destroy();
      }
    };
  }, [streamUrl, preflightUrl]);

  return (
    <div className="player-wrapper" style={{
      position: 'fixed',
      top: 0,
      left: 0,
      width: '100vw',
      height: '100vh',
      backgroundColor: '#000',
      zIndex: 9999,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center'
    }}>
      {/* Close Button */}
      <button
        onClick={onClose}
        style={closeButtonStyle}
        onMouseEnter={(e) => {
          e.target.style.background = 'rgba(255, 255, 255, 0.3)';
          e.target.style.transform = 'scale(1.05)';
        }}
        onMouseLeave={(e) => {
          e.target.style.background = 'rgba(255, 255, 255, 0.2)';
          e.target.style.transform = 'scale(1)';
        }}
      >
        ✕ Close
      </button>

      {/* Error Overlay */}
      {error && (
        <div style={{
          position: 'absolute',
          zIndex: 10001,
          background: 'rgba(255, 0, 0, 0.8)',
          color: 'white',
          padding: '20px',
          borderRadius: '12px',
          backdropFilter: 'blur(5px)',
          maxWidth: '80%',
          textAlign: 'center'
        }}>
          <h3>Error</h3>
          <p>{error}</p>
          <button onClick={onClose} style={{
            marginTop: '10px',
            padding: '8px 16px',
            background: 'white',
            color: 'red',
            border: 'none',
            borderRadius: '6px',
            cursor: 'pointer',
            fontWeight: 'bold'
          }}>Close Player</button>
        </div>
      )}

      {/* Video Element */}
      <div style={{ width: '100%', height: '100%' }}>
        <video
          ref={videoRef}
          className="plyr-video"
          playsInline
          crossOrigin="anonymous"
          style={{ width: '100%', height: '100%' }}
        />
      </div>

      {/* Custom Styles for Plyr to match "Premium" Look */}
      <style>{`
        :root {
            --plyr-color-main: #3b82f6; /* Modern Blue */
            --plyr-video-background: #000;
            --plyr-menu-background: rgba(20, 20, 20, 0.9);
            --plyr-menu-color: #fff;
        }
        .plyr--full-ui {
            height: 100vh !important;
        }
        .plyr__control--overlaid {
            background: rgba(59, 130, 246, 0.8) !important;
        }
        .plyr--video .plyr__controls {
            background: linear-gradient(rgba(0,0,0,0), rgba(0,0,0,0.7));
            padding-bottom: 30px;
        }
      `}</style>
    </div>
  );
}
