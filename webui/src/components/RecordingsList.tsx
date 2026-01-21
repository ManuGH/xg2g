// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Phase 2E: Recordings View refactored to primitives (Card + StatusChip)
// CTO Contract: No custom surfaces/badges, layout-only CSS, tabular technical data

import React, { useState, useEffect, lazy, Suspense, useRef } from 'react';
import { getRecordings, deleteRecording, type RecordingResponse, type RecordingItem } from '../client-ts';
import { useAppContext } from '../context/AppContext';
import { useTranslation } from 'react-i18next';
import RecordingResumeBar, { isResumeEligible } from '../features/resume/RecordingResumeBar';
import { Card, CardBody } from './ui/Card';
import { StatusChip, type ChipState } from './ui/StatusChip';
import './Recordings.css';

const V3Player = lazy(() => import('./V3Player'));

// Simple Icons
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

// mapRecordingToChip - CTO Contract: Deterministic mapping
function mapRecordingToChip(item: RecordingItem): { state: ChipState; label: string } {
  // Priority 1: Resume State (High Value / Orthogonal)
  // "WATCHED" or partial progress overrides "Completed"/"New" status for display utility.
  if (item.resume?.finished) return { state: 'success', label: 'WATCHED' };
  if (item.resume?.posSeconds && item.resume.posSeconds > 0) return { state: 'warning', label: 'RESUME' };

  // Priority 2: Explicit Truth Status (P3-3)
  // Stop-the-line: If backend provides status, WE TRUST IT. Do not fallback to title parsing.
  if (item.status) {
    switch (item.status) {
      case 'recording': return { state: 'recording', label: 'REC' };
      case 'pending': return { state: 'warning', label: 'PENDING' };
      case 'failed': return { state: 'error', label: 'FAILED' };
      case 'deleting': return { state: 'idle', label: 'DELETING' };
      case 'completed': return { state: 'success', label: 'NEW' }; // Completed + Unwatched = NEW
      default:
        // Exhaustiveness check / Safety fallback
        return { state: 'success', label: 'NEW' };
    }
  }

  // Priority 3: Legacy Fallback (Title Tokens)
  // Only reachable if item.status is undefined (legacy backend or partial data)
  if (item.title?.includes('[REC]')) return { state: 'recording', label: 'REC' };
  if (item.title?.includes('[ERROR]')) return { state: 'error', label: 'ERROR' };
  if (item.title?.includes('[WAIT]')) return { state: 'warning', label: 'PENDING' };

  return { state: 'success', label: 'NEW' };
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
  const [playing, setPlaying] = useState<PlayingState | null>(null);

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
      const response = await getRecordings({ query: { root: r, path: p } });
      const res = response.data;
      if (!res) throw new Error('No data received');

      setData(res);

      if (initialLoad.current) {
        initialLoad.current = false;
        if (res.currentRoot && res.currentRoot !== r) {
          setRoot(res.currentRoot);
        }
        if (res.currentPath !== undefined && res.currentPath !== p) {
          setPath(res.currentPath);
        }
      }
    } catch (err: any) {
      console.error(err);
      setError(err.body?.detail || err.statusText || 'Failed to load recordings');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData(root, path);
    setSelectedIds(new Set());
  }, [root, path]);

  // Handlers
  const handleRootChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newRoot = e.target.value;
    setRoot(newRoot);
    setPath('');
  };

  const handleNavigate = (newPath: string) => {
    if (selectionMode) {
      setSelectionMode(false);
    }
    setPath(newPath);
  };

  const handlePlay = async (item: RecordingItem) => {
    if (selectionMode) return;
    if (!item.recordingId) return;

    setPlaying({
      recordingId: item.recordingId,
      title: item.title || 'Recording',
      duration: item.durationSeconds ?? 0
    });
  };

  const toggleSelectionMode = () => {
    setSelectionMode(prev => {
      if (prev) {
        setSelectedIds(new Set());
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
    if (!window.confirm(t('recordings.confirmDelete', { count: selectedIds.size }))) return;

    setDeleteLoading(true);
    try {
      const ids = Array.from(selectedIds);
      await Promise.allSettled(
        ids.map(id => deleteRecording({ path: { recordingId: id } }))
      );
      await fetchData(root, path);
      setSelectionMode(false);
      setSelectedIds(new Set());
    } finally {
      setDeleteLoading(false);
    }
  };

  const formatTime = (ts?: number) => {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleString();
  };

  if (loading && !data) {
    return (
      <div className="recordings-container animate-enter">
        <Card><CardBody>Loading recordings...</CardBody></Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="recordings-container animate-enter">
        <Card>
          <CardBody>
            <div className="alert-error">
              <h3>Error Loading Recordings</h3>
              <StatusChip state="error" label="ERROR" />
              <p>{error}</p>
              <button className="btn-secondary" onClick={() => fetchData(root, path)}>Retry</button>
            </div>
          </CardBody>
        </Card>
      </div>
    );
  }

  return (
    <div className="recordings-container animate-enter">
      {/* Header / Toolbar */}
      <div className="recordings-toolbar">
        <div className="toolbar-group">
          <label className="info-label">Location:</label>
          <select value={root} onChange={handleRootChange} disabled={loading || deleteLoading}>
            {data?.roots?.map(r => (
              <option key={r.id} value={r.id}>{r.name} ({r.id})</option>
            ))}
          </select>
        </div>

        <div className="breadcrumbs">
          <span className="crumb" onClick={() => handleNavigate('')}>Home</span>
          {data?.breadcrumbs?.map((crumb, i) => (
            <React.Fragment key={i}>
              <span className="separator">/</span>
              <span className="crumb" onClick={() => handleNavigate(crumb.path || '')}>{crumb.name}</span>
            </React.Fragment>
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
          <Card
            key={`dir-${i}`}
            interactive
            onClick={() => handleNavigate(dir.path || '')}
          >
            <CardBody className="recording-item-content">
              <div className="icon-wrapper">
                <FolderIcon className="folder-icon" />
              </div>
              <div className="item-details">
                <span className="item-name">{dir.name}</span>
                <StatusChip state="idle" label="Directory" showIcon={false} />
              </div>
            </CardBody>
          </Card>
        ))}

        {/* Recordings */}
        {data?.recordings?.map((rec, i) => {
          const isSelected = rec.recordingId ? selectedIds.has(rec.recordingId) : false;
          const { state, label } = mapRecordingToChip(rec);

          return (
            <Card
              key={`rec-${i}`}
              interactive
              className={isSelected ? 'selected' : ''}
              variant={state === 'recording' || state === 'live' ? 'live' : 'standard'}
              onClick={() => selectionMode && rec.recordingId ? toggleSelect(rec.recordingId) : handlePlay(rec)}
            >
              <CardBody className="recording-item-content">
                <div className="icon-wrapper">
                  <FileIcon className="file-icon" />
                </div>
                <div className="item-details">
                  <div className="item-name">{rec.title}</div>
                  <div className="item-meta-row tabular">
                    <span className="meta-date">{formatTime(rec.begin_unix_seconds)}</span>
                    <span className="meta-length">{rec.length}</span>
                  </div>
                  <p className="item-desc">{rec.description}</p>

                  <div className="badge-group">
                    <StatusChip state={state} label={label} />
                  </div>

                  {/* Resume Bar Integration */}
                  {(rec as RecordingItem).resume && (() => {
                    const r = (rec as RecordingItem).resume!;
                    const safeResume = {
                      ...r,
                      posSeconds: r.posSeconds || 0,
                      durationSeconds: r.durationSeconds || 0,
                      finished: r.finished || false
                    };
                    return isResumeEligible(safeResume, rec.durationSeconds) && (
                      <div className="recording-resume-container">
                        <RecordingResumeBar
                          resume={safeResume}
                          durationSeconds={rec.durationSeconds}
                        />
                      </div>
                    );
                  })()}
                </div>

                {selectionMode && (
                  <div className={`selection-indicator ${isSelected ? 'active' : ''}`}>
                    {isSelected && <CheckCircleIcon className="check-icon" />}
                  </div>
                )}

                {!selectionMode && (
                  <div className="play-overlay">
                    <span className="play-label">Play</span>
                  </div>
                )}
              </CardBody>
            </Card>
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
        <Suspense fallback={
          <div className="player-fallback">
            <Card><CardBody>Loading Player...</CardBody></Card>
          </div>
        }>
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
