// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.


import React, { useState, useEffect } from 'react';
import { RecordingsService } from '../client/services/RecordingsService';
import { AuthService } from '../client/services/AuthService';
import Player from './Player';
import './Recordings.css';

// SVG Icons
const FolderIcon = ({ className }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M19.5 21a3 3 0 003-3v-4.5a3 3 0 00-3-3h-15a3 3 0 00-3 3V18a3 3 0 003 3h15zM1.5 10.146V6a3 3 0 013-3h5.379a2.25 2.25 0 011.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 013 3v1.146A4.483 4.483 0 0019.5 9h-15a4.483 4.483 0 00-1.89.417z" />
  </svg>
);

const FileIcon = ({ className }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path fillRule="evenodd" d="M5.625 1.5c-1.036 0-1.875.84-1.875 1.875v17.25c0 1.035.84 1.875 1.875 1.875h12.75c1.035 0 1.875-.84 1.875-1.875V12.75A3.75 3.75 0 0016.5 9h-1.875a1.875 1.875 0 01-1.875-1.875V5.25A3.75 3.75 0 009 1.5H5.625zM12.971 5.25a5.23 5.23 0 00-1.276-2.575c-.098-.099-.205-.198-.322-.29C13.315 2.444 14.88 3.5 15.686 5.25H12.972z" clipRule="evenodd" />
    <path d="M9.75 13.5a.75.75 0 000 1.5h4.5a.75.75 0 000-1.5h-4.5zM9.75 16.5a.75.75 0 000 1.5h4.5a.75.75 0 000-1.5h-4.5z" />
  </svg>
);

export default function RecordingsList() {
  // State
  const [root, setRoot] = useState(''); // Selected Root ID
  const [path, setPath] = useState(''); // Current relative path
  const [data, setData] = useState(null); // Full API response
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [playing, setPlaying] = useState(null); // { url, title }
  const initialLoad = React.useRef(true);

  // Fetch Data
  const fetchData = async (r, p) => {
    setLoading(true);
    setError(null);
    try {
      // If root is empty, backend will pick default and return it in 'current_root'
      const res = await RecordingsService.getRecordings(r, p);
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
    } catch (err) {
      console.error(err);
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
  const handleRootChange = (e) => {
    const newRoot = e.target.value;
    setRoot(newRoot);
    setPath(''); // Reset to top of new root
  };

  const handleNavigate = (newPath) => {
    setPath(newPath);
  };

  const handlePlay = async (item) => {
    try {
      // 1. Establish Secure Session (HttpOnly Cookie)
      // We use the generated client to call POST /api/auth/session
      await AuthService.createSession();
    } catch (e) {
      console.warn("Failed to create session cookie, playback might fail on some browsers", e);
    }

    // 2. Construct HLS Playlist URL manually
    // Use Base64 URL Encoding for the ID to be safe in URL segments
    const toBase64Url = (str) => {
      return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    };

    const baseUrl = '/api/v3';
    const encodedId = toBase64Url(item.service_ref);
    const url = `${baseUrl}/recordings/${encodedId}/playlist.m3u8`;

    setPlaying({ url, title: item.title });
  };

  const handleDelete = async (item) => {
    try {
      // Encode ID
      const toBase64Url = (str) => {
        return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
      };
      const encodedId = toBase64Url(item.service_ref);

      const token = localStorage.getItem('XG2G_API_TOKEN'); // Use consistent key
      // Actually RecordingsService likely doesn't have delete method yet.
      // We can use fetch directly or add it to service. 
      // Let's use fetch for MVP to avoid editing another file if possible, or edit service properly.
      // Editing service is better practice. But for now speed:

      const res = await fetch(`/api/v3/recordings/${encodedId}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });

      if (!res.ok) {
        throw new Error('Failed to delete recording');
      }

      // Refresh list
      fetchData(root, path);
    } catch (e) {
      console.error(e);
      alert('Failed to delete recording');
    }
  };

  const formatDuration = (val) => {
    if (!val) return '';
    // Check if seconds or string '90 min'
    if (typeof val === 'number') {
      const h = Math.floor(val / 3600);
      const m = Math.floor((val % 3600) / 60);
      return h > 0 ? `${h}h ${m}m` : `${m}m`;
    }
    return val; // Return string as is logic (e.g. '90 min') 
  };

  const formatDate = (ts) => {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleString();
  };

  // Render Helpers
  const renderBreadcrumbs = () => {
    if (!data?.breadcrumbs || data.breadcrumbs.length === 0) {
      // Show Root Name only
      const rootName = data?.roots?.find(r => r.id === root)?.name || 'Root';
      return <span className="breadcrumb-item active">{rootName}</span>;
    }

    const rootName = data?.roots?.find(r => r.id === root)?.name || 'Root';

    return (
      <>
        <span
          className="breadcrumb-item"
          onClick={() => setPath('')}
        >
          {rootName}
        </span>
        {data.breadcrumbs.map((crumb, idx) => (
          <React.Fragment key={idx}>
            <span className="breadcrumb-separator">/</span>
            <span
              className={`breadcrumb-item ${idx === data.breadcrumbs.length - 1 ? 'active' : ''}`}
              onClick={() => idx !== data.breadcrumbs.length - 1 && handleNavigate(crumb.path)}
            >
              {crumb.name}
            </span>
          </React.Fragment>
        ))}
      </>
    );
  };

  return (
    <div className="recordings-view">
      {/* Header / Toolbar */}
      <div className="recordings-toolbar">
        <div className="breadcrumbs">
          {renderBreadcrumbs()}
        </div>

        <div className="toolbar-actions">
          {data?.roots?.length > 1 && (
            <select
              value={root}
              onChange={handleRootChange}
              className="root-select"
            >
              {data.roots.map(r => (
                <option key={r.id} value={r.id}>{r.name}</option>
              ))}
            </select>
          )}
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="status-indicator error w-full justify-center">
          {error}
        </div>
      )}

      {/* Content */}
      <div className="recordings-list-card">
        {loading && !data && (
          <div className="p-8 text-center text-gray-400">Loading...</div>
        )}

        {data && (
          <table className="rec-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Duration</th>
                <th>Date</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {/* Parent Directory (if not root) */}
              {path && path !== '' && (
                <tr>
                  <td colSpan="4">
                    <div
                      className="rec-name-cell"
                      onClick={() => {
                        // Go up one level
                        // breadcrumbs has full path of parent?
                        // easiest: pop last segment of current path
                        // backend doesn't send '..' explicitly in DTO (removed in my impl), rely on crumbs/logic
                        // Handle manual '..' : path.dirname
                        // BUT breadcrumbs handle navigation.
                        // Let's rely on Breadcrumbs for UP navigation for now?
                        // OR add '..' row? UX usually expects '..' row.
                        // I'll implement '..' logic here:
                        const parts = path.split('/');
                        parts.pop();
                        handleNavigate(parts.join('/'));
                      }}
                    >
                      <FolderIcon className="rec-icon rec-icon-folder" />
                      <span className="rec-name">..</span>
                    </div>
                  </td>
                </tr>
              )}

              {/* Directories */}
              {data.directories?.map((dir, idx) => (
                <tr key={`dir-${idx}`}>
                  <td>
                    <div
                      className="rec-name-cell"
                      onClick={() => handleNavigate(dir.path)}
                    >
                      <FolderIcon className="rec-icon rec-icon-folder" />
                      <span className="rec-name">{dir.name}</span>
                    </div>
                  </td>
                  <td>-</td>
                  <td>-</td>
                  <td>
                    {/* Maybe Open Action? */}
                  </td>
                </tr>
              ))}

              {/* Recordings */}
              {data.recordings?.map((rec, idx) => (
                <tr key={`rec-${idx}`}>
                  <td>
                    <div
                      className="rec-name-cell"
                      onClick={() => handlePlay(rec)}
                    >
                      <FileIcon className="rec-icon rec-icon-file" />
                      <div className="rec-info">
                        <span className="rec-title">{rec.title}</span>
                        {rec.description && <span className="rec-desc">{rec.description}</span>}
                      </div>
                    </div>
                  </td>
                  <td>{formatDuration(rec.length)}</td>
                  <td>{formatDate(rec.begin)}</td>
                  <td className="text-right">
                    <button
                      className="rec-action-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        handlePlay(rec);
                      }}
                    >
                      Play
                    </button>
                    <button
                      className="rec-action-btn rec-delete-btn"
                      style={{ marginLeft: '8px', backgroundColor: '#ef4444' }}
                      onClick={(e) => {
                        e.stopPropagation();
                        // eslint-disable-next-line no-restricted-globals
                        if (confirm(`Delete "${rec.title}"? This cannot be undone.`)) {
                          handleDelete(rec);
                        }
                      }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}

              {/* Empty State */}
              {(!data.directories?.length && !data.recordings?.length) && (
                <tr>
                  <td colSpan="4" className="rec-empty">
                    No recordings found in this folder.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {/* Player Overlay */}
      {playing && (
        <Player
          streamUrl={playing.url}
          title={playing.title}
          onClose={() => setPlaying(null)}
        />
      )}
    </div>
  );
}
