// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useState, useEffect, lazy, Suspense, useRef } from 'react';
import { getRecordings, deleteRecording, type RecordingResponse, type RecordingItem } from '../client-ts';
import { useAppContext } from '../context/AppContext';
import { useTranslation } from 'react-i18next';
import RecordingResumeBar, { isResumeEligible } from '../features/resume/RecordingResumeBar';
import type { ResumeSummary } from '../features/resume/types';
import './Recordings.css';

// TODO: Remove this once client-ts types are regenerated
type RecordingItemWithResume = RecordingItem & {
  resume?: ResumeSummary;
};

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

const TrashIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path fillRule="evenodd" d="M16.5 4.478v.227a48.816 48.816 0 0 1 3.878.512.75.75 0 1 1-.49 1.478 47.4 47.4 0 0 0-3.899-.514H7.991a47.403 47.403 0 0 0-3.899.513.75.75 0 0 1-.492-1.478 48.817 48.817 0 0 1 3.879-.512v-.227c0-1.168.968-2.147 2.135-2.288 1.477-.178 3.013-.178 4.49 0 1.167.14 2.135 1.12 2.135 2.288ZM8.33 12a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75H9.08a.75.75 0 0 1-.75-.75V12Zm3.75 0a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75h-.008a.75.75 0 0 1-.75-.75V12Zm3.75 0a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75h-.008a.75.75 0 0 1-.75-.75V12Z" clipRule="evenodd" />
    <path d="M5 9.75a.75.75 0 0 1 .75-.75h12.5a.75.75 0 0 1 .75.75v7.5a9 9 0 0 1-9 9h-4.5a9 9 0 0 1-9-9v-7.5Z" />
  </svg>

);

const CheckCircleIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path fillRule="evenodd" d="M2.25 12c0-5.385 4.365-9.75 9.75-9.75s9.75 4.365 9.75 9.75-4.365 9.75-9.75 9.75S2.25 17.385 2.25 12Zm13.36-1.814a.75.75 0 1 0-1.22-.872l-3.236 4.53L9.53 12.22a.75.75 0 0 0-1.06 1.06l2.25 2.25a.75.75 0 0 0 1.14-.094l3.75-5.25Z" clipRule="evenodd" />
  </svg>
);

interface PlayingState {
  recordingId: string;
  title: string;
  duration: number;
}

export default function RecordingsList() {
  const { auth } = useAppContext();
  const { t } = useTranslation();

  // State
  const [root, setRoot] = useState<string>(''); // Selected Root ID
  const [path, setPath] = useState<string>(''); // Current relative path
  const [data, setData] = useState<RecordingResponse | null>(null); // Full API response
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [playing, setPlaying] = useState<PlayingState | null>(null); // { url, title, duration }

  // Bulk Delete State
  const [selectionMode, setSelectionMode] = useState<boolean>(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [deleteLoading, setDeleteLoading] = useState<boolean>(false);

  const initialLoad = useRef<boolean>(true);

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
    // Clear selection on navigation
    setSelectedIds(new Set());
  }, [root, path]);

  // Handlers
  const handleRootChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newRoot = e.target.value;
    setRoot(newRoot);
    setPath(''); // Reset to top of new root
  };

  const handleNavigate = (newPath: string) => {
    if (selectionMode) {
      setSelectionMode(false); // Exit selection mode on nav
    }
    setPath(newPath);
  };

  const handlePlay = async (item: RecordingItem) => {
    if (selectionMode) return; // Ignore play if in selection mode

    if (!item.recording_id) {
      console.warn('Missing recording_id for recording item:', item);
      return;
    }

    const durationSec = item.duration_seconds ?? 0;

    setPlaying({
      recordingId: item.recording_id,
      title: item.title || 'Recording',
      duration: durationSec
    });
  };

  // Selection Logic
  const toggleSelectionMode = () => {
    setSelectionMode(prev => {
      if (prev) {
        setSelectedIds(new Set()); // Clear on exit
      }
      return !prev;
    });
  };

  const toggleSelect = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleBulkDelete = async () => {
    if (selectedIds.size === 0) return;

    if (!window.confirm(t('recordings.confirmDelete', { count: selectedIds.size }))) {
      return;
    }

    setDeleteLoading(true);
    try {
      const ids = Array.from(selectedIds);

      // Concurrency limit of 5 to be safe, though allSettled is mostly parallel
      // Simple Promise.allSettled for now as count is usually low
      const results = await Promise.allSettled(
        ids.map(id => deleteRecording({ path: { recordingId: id } }))
      );

      const success = results.filter(r => r.status === 'fulfilled').length;
      const failed = results.filter(r => r.status === 'rejected').length;

      if (failed === 0) {
        // All good
        console.log(`Deleted ${success} recordings`);
      } else {
        console.warn(`Failed to delete ${failed} recordings`);
      }

      // Always refresh and clear state
      await fetchData(root, path);
      setSelectionMode(false);
      setSelectedIds(new Set());
    } finally {
      setDeleteLoading(false);
    }
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
          <select value={root} onChange={handleRootChange} disabled={loading || deleteLoading}>
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

        <div className="toolbar-actions">
          {selectionMode ? (
            <>
              <button
                className="btn-danger"
                disabled={selectedIds.size === 0 || deleteLoading}
                onClick={handleBulkDelete}
              >
                {deleteLoading ? t('common.loading') : t('recordings.deleteSelected', { count: selectedIds.size })}
              </button>
              <button className="btn-secondary" onClick={toggleSelectionMode} disabled={deleteLoading}>
                {t('recordings.cancelSelection')}
              </button>
            </>
          ) : (
            <button
              className="icon-btn"
              title={t('recordings.selectionMode')}
              onClick={toggleSelectionMode}
            >
              <TrashIcon className="icon-sm" />
            </button>
          )}
        </div>
      </div>

      {/* Content Grid */}
      <div className={`recordings-grid ${selectionMode ? 'selection-mode' : ''}`}>

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
        {data?.recordings?.map((rec, i) => {
          const isSelected = rec.recording_id ? selectedIds.has(rec.recording_id) : false;
          return (
            <div
              key={i}
              className={`grid-item recording ${isSelected ? 'selected' : ''}`}
              onClick={() => selectionMode && rec.recording_id ? toggleSelect(rec.recording_id) : handlePlay(rec)}
            >
              <div className="icon-wrapper">
                <FileIcon className="file-icon" />
              </div>
              <div className="item-details">
                <span className="item-name">{rec.title}</span>
                <div className="item-meta-row">
                  <span className="meta-date">{formatTime(rec.begin_unix_seconds)}</span>
                  <span className="meta-length">{rec.length}</span>
                </div>
                <p className="item-desc">{rec.description}</p>
                {/* Resume Bar Integration */}
                {(rec as RecordingItemWithResume).resume && isResumeEligible((rec as RecordingItemWithResume).resume, rec.duration_seconds) && (
                  <RecordingResumeBar
                    resume={(rec as RecordingItemWithResume).resume!}
                    durationSeconds={rec.duration_seconds}
                  />
                )}
              </div>

              {selectionMode && (
                <div className={`selection-indicator ${isSelected ? 'active' : ''}`}>
                  {isSelected && <CheckCircleIcon className="check-icon" />}
                </div>
              )}

              {!selectionMode && (
                <div className="play-overlay">
                  <span>▶ Play</span>
                </div>
              )}
            </div>
          );
        })}

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
            recordingId={playing.recordingId}
            token={auth?.token || undefined}
            autoStart={true}
            onClose={() => setPlaying(null)}
            duration={playing.duration}
          />
        </Suspense>
      )}
    </div>
  );
}
