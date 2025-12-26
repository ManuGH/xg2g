// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useState, useEffect, lazy, Suspense, useRef } from 'react';
import { getRecordings, type RecordingResponse, type RecordingItem } from '../client-ts';
import { client } from '../client-ts/client.gen';
import { useAppContext } from '../context/AppContext';
import './Recordings.css';

const V3Player = lazy(() => import('./V3Player'));

// SVG Icons
const FolderIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M19.5 21a3 3 0 003-3v-4.5a3 3 0 00-3-3h-15a3 3 0 00-3 3V18a3 3 0 003 3h15zM1.5 10.146V6a3 3 0 013-3h5.379a2.25 2.25 0 011.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 013 3v1.146A4.483 4.483 0 0019.5 9h-15a4.483 4.483 0 00-1.89.417z" />
  </svg>
);

const FileIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path fillRule="evenodd" d="M5.625 1.5c-1.036 0-1.875.84-1.875 1.875v17.25c0 1.035.84 1.875 1.875 1.875h12.75c1.035 0 1.875-.84 1.875-1.875V12.75A3.75 3.75 0 0016.5 9h-1.875a1.875 1.875 0 01-1.875-1.875V5.25A3.75 3.75 0 009 1.5H5.625zM12.971 5.25a5.23 5.23 0 00-1.276-2.575c-.098-.099-.205-.198-.322-.29C13.315 2.444 14.88 3.5 15.686 5.25H12.972z" clipRule="evenodd" />
    <path d="M9.75 13.5a.75.75 0 000 1.5h4.5a.75.75 0 000-1.5h-4.5zM9.75 16.5a.75.75 0 000 1.5h4.5a.75.75 0 000-1.5h-4.5z" />
  </svg>
);

interface PlayingState {
  url: string;
  title: string;
}

export default function RecordingsList() {
  // Context for Auth Token
  const { auth } = useAppContext();

  // State
  const [root, setRoot] = useState<string>(''); // Selected Root ID
  const [path, setPath] = useState<string>(''); // Current relative path
  const [data, setData] = useState<RecordingResponse | null>(null); // Full API response
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [playing, setPlaying] = useState<PlayingState | null>(null); // { url, title }
  const initialLoad = useRef<boolean>(true);

  // Safe cast for baseUrl which exists in broader context
  const apiBase = (client.getConfig().baseUrl || '/api/v3').replace(/\/$/, '');

  // Fetch Data
  const fetchData = async (r: string, p: string) => {
    setLoading(true);
    setError(null);
    try {
      // If root is empty, backend will pick default and return it in 'current_root'
      const response = await getRecordings({ query: { root: r, path: p } });
      const res = response.data;
      if (!res) throw new Error('No data received');

      setData(res);

      // Only update local state from backend on the very first load
      // This prevents double-fetching when backend normalizes keys
      if (initialLoad.current) {
        initialLoad.current = false;
        if (res.current_root && res.current_root !== r) {
          setRoot(res.current_root);
        }
        if (res.current_path !== undefined && res.current_path !== p) {
          setPath(res.current_path);
        }
      }
    } catch (err: any) {
      console.error(err);
      // @ts-ignore
      setError(err.body?.detail || err.statusText || 'Failed to load recordings');
    } finally {
      setLoading(false);
    }
  };

  // Effect: Fetch on mount or state change
  useEffect(() => {
    fetchData(root, path);
  }, [root, path]);

  // Handlers
  const handleRootChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newRoot = e.target.value;
    setRoot(newRoot);
    setPath(''); // Reset to top of new root
  };

  const handleNavigate = (newPath: string) => {
    setPath(newPath);
  };

  const handlePlay = async (item: RecordingItem) => {
    if (!item.service_ref) return;

    // Use HLS playlist URL for V3Player direct mode
    const toBase64Url = (str: string) => {
      return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    };

    const baseUrl = apiBase;
    const encodedId = toBase64Url(item.service_ref);
    const url = `${baseUrl}/recordings/${encodedId}/playlist.m3u8`;

    setPlaying({
      url,
      title: item.title || 'Recording'
    });
  };

  // Helper to format UNIX timestamp
  const formatTime = (ts?: number) => {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleString();
  };

  if (loading && !data) {
    return (
      <div className="recordings-container">
        <div className="loading-spinner"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="recordings-container">
        <div className="alert-error">
          <h3>Error Loading Recordings</h3>
          <p>{error}</p>
          <button onClick={() => fetchData(root, path)}>Retry</button>
        </div>
      </div>
    );
  }

  return (
    <div className="recordings-container animate-fade-in">
      {/* Header / Toolbar */}
      <div className="recordings-toolbar">
        <div className="toolbar-group">
          <label>Location:</label>
          <select value={root} onChange={handleRootChange} disabled={loading}>
            {data?.roots?.map(r => (
              <option key={r.id} value={r.id}>{r.name} ({r.id})</option>
            ))}
          </select>
        </div>

        <div className="breadcrumbs">
          <span className="crumb" onClick={() => handleNavigate('')}>Home</span>
          {data?.breadcrumbs?.map((crumb, i) => (
            <span key={i}>
              <span className="separator">/</span>
              <span className="crumb" onClick={() => handleNavigate(crumb.path || '')}>{crumb.name}</span>
            </span>
          ))}
        </div>
      </div>

      {/* Content Grid */}
      <div className="recordings-grid">

        {/* Directories */}
        {data?.directories?.map((dir, i) => (
          <div key={i} className="grid-item directory" onClick={() => handleNavigate(dir.path || '')}>
            <div className="icon-wrapper">
              <FolderIcon className="folder-icon" />
            </div>
            <div className="item-details">
              <span className="item-name">{dir.name}</span>
              <span className="item-meta">Directory</span>
            </div>
          </div>
        ))}

        {/* Recordings */}
        {data?.recordings?.map((rec, i) => (
          <div key={i} className="grid-item recording" onClick={() => handlePlay(rec)}>
            <div className="icon-wrapper">
              <FileIcon className="file-icon" />
            </div>
            <div className="item-details">
              <span className="item-name">{rec.title}</span>
              <div className="item-meta-row">
                <span className="meta-date">{formatTime(rec.begin)}</span>
                <span className="meta-length">{rec.length} min</span>
              </div>
              <p className="item-desc">{rec.description}</p>
            </div>
            <div className="play-overlay">
              <span>▶ Play</span>
            </div>
          </div>
        ))}

        {/* Empty State */}
        {(!data?.directories?.length && !data?.recordings?.length) && (
          <div className="empty-state">
            <p>No recordings found in this location.</p>
          </div>
        )}
      </div>

      {/* V3Player Overlay */}
      {playing && (
        <Suspense fallback={<div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.9)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'white' }}>Loading Player...</div>}>
          <V3Player
            src={playing.url}
            token={auth?.token || undefined}
            autoStart={true}
            onClose={() => setPlaying(null)}
          />
        </Suspense>
      )}
    </div>
  );
}
