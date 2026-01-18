// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { getSystemHealth, postSystemRefresh, type SystemHealth } from '../client-ts';
import './Files.css';

function Files() {
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [regenerating, setRegenerating] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getSystemHealth();
      if (response.data) {
        setHealth(response.data);
        setError(null);
      } else if (response.error) {
        // @ts-ignore - response.error might be generic, status check is valid runtime
        if (response.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          // @ts-ignore
          setError(response.error.message || 'Failed to fetch health');
        }
      }
    } catch (err: any) {
      if (err.status === 401) {
        window.dispatchEvent(new Event('auth-required'));
        setError('Authentication required. Please enter your API token.');
      } else {
        setError(err.message || 'Failed to fetch status');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStatus();
  }, []);

  const handleRegenerate = async () => {
    setError(null);
    setRegenerating(true);
    try {
      await postSystemRefresh();
      setTimeout(fetchStatus, 1000);
    } catch (err: any) {
      if (err.status === 401) {
        window.dispatchEvent(new Event('auth-required'));
        setError('Authentication required. Please enter your API token.');
      } else {
        setError(err.message || 'Failed to regenerate');
      }
    } finally {
      setRegenerating(false);
    }
  };

  if (loading && !health) return <div className="files-loading">Loading...</div>;
  if (error) {
    return (
      <div className="files-alert files-alert-error">
        <div>Error: {error}</div>
        <button
          className="files-btn files-btn-secondary"
          onClick={fetchStatus}
          disabled={loading}
        >
          Retry
        </button>
      </div>
    );
  }

  const hdhrUrl = `${window.location.protocol}//${window.location.host}/device.xml`;
  const m3uUrl = '/files/playlist.m3u';
  const xmltvUrl = `${window.location.protocol}//${window.location.host}/xmltv.xml`;

  return (
    <div className="files-container">
      <div className="files-header">
        <h2>Playlist & EPG</h2>
        <button onClick={handleRegenerate} disabled={regenerating} className="files-btn files-btn-primary">
          {regenerating ? 'Regenerating...' : 'Regenerate Files'}
        </button>
      </div>

      {health?.epg?.status && (
        <div className="files-subtle">
          EPG status: <span className={`files-status ${health.epg.status === 'ok' ? 'is-ok' : 'is-warn'}`}>{health.epg.status}</span>
        </div>
      )}

      <div className="file-list">
        <div className="file-card">
          <h3>M3U Playlist</h3>
          <p className="description">Standard M3U playlist for VLC, Kodi, TiviMate.</p>
          <a href={m3uUrl} className="files-btn files-btn-primary" download>Download M3U</a>
        </div>

        <div className="file-card">
          <h3>XMLTV Guide</h3>
          <p className="description">EPG Data.</p>
          {health?.epg?.status === 'ok' ? (
            <p className="files-alert files-alert-success">EPG Loaded</p>
          ) : (
            <p className="files-alert files-alert-warning">EPG Missing or Partial</p>
          )}
          <div className="code-block" aria-label="XMLTV URL">{xmltvUrl}</div>
          <div className="actions-row">
            <button className="files-btn files-btn-secondary" onClick={() => navigator.clipboard.writeText(xmltvUrl)}>
              Copy URL
            </button>
            <a href="/xmltv.xml" className="files-btn files-btn-secondary" download>
              Download
            </a>
          </div>
        </div>

        <div className="file-card">
          <h3>HDHomeRun (Plex)</h3>
          <p className="description">Use this IP/URL to add xg2g as a DVR in Plex or Jellyfin.</p>
          <div className="code-block" aria-label="HDHomeRun base URL">{hdhrUrl.replace('/device.xml', '')}</div>
          <button
            className="files-btn files-btn-secondary"
            onClick={() => navigator.clipboard.writeText(hdhrUrl.replace('/device.xml', ''))}
          >
            Copy IP
          </button>
        </div>
      </div>
    </div>
  );
}

export default Files;
