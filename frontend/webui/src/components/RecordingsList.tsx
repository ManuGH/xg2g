// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Phase 2E: Recordings View refactored to primitives (Card + StatusChip)
// CTO Contract: No custom surfaces/badges, layout-only CSS, tabular technical data

import React, { useState, useEffect, lazy, Suspense, useRef } from 'react';
import { type RecordingItem } from '../client-ts';
import { useAppContext } from '../context/AppContext';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import { filterRecordingsForProfile } from '../features/household/model';
import { useTranslation } from 'react-i18next';
import RecordingResumeBar, { isResumeEligible } from '../features/resume/RecordingResumeBar';
import { usePlayerHistoryBridge } from '../features/player/usePlayerHistoryBridge';
import { useUiOverlay } from '../context/UiOverlayContext';
import { useDeleteRecordingsMutation, useRecordings } from '../hooks/useServerQueries';
import { toAppError } from '../lib/appErrors';
import { Button, Card, CardBody, StatusChip, type ChipState } from './ui';
import ErrorPanel from './ErrorPanel';
import LoadingSkeleton from './LoadingSkeleton';
import styles from './Recordings.module.css';

const V3Player = lazy(() => import('../features/player/components/V3Player'));

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
  durationSeconds: number;
}

type RecordingsFilter = 'all' | 'active' | 'resume' | 'unwatched';
type RecordingsSort = 'newest' | 'oldest';
type RecordingChipLabel = 'watched' | 'resume' | 'rec' | 'scheduled' | 'failed' | 'unknown' | 'new';

// mapRecordingToChip - CTO Contract: Deterministic mapping
function mapRecordingToChip(item: RecordingItem): { state: ChipState; labelKey: RecordingChipLabel } {
  // Priority 1: Resume State (High Value / Orthogonal)
  // "WATCHED" or partial progress overrides "Completed"/"New" status for display utility.
  if (item.resume?.finished) return { state: 'success', labelKey: 'watched' };
  if (item.resume?.posSeconds && item.resume.posSeconds > 0) return { state: 'warning', labelKey: 'resume' };

  // Priority 2: Explicit Truth Status (P3-3)
  // Stop-the-line: If backend provides status, WE TRUST IT. Missing status fails closed to unknown.
  switch (item.status) {
    case 'recording': return { state: 'recording', labelKey: 'rec' };
    case 'scheduled': return { state: 'warning', labelKey: 'scheduled' };
    case 'failed': return { state: 'error', labelKey: 'failed' };
    case 'completed': return { state: 'success', labelKey: 'new' }; // Completed + Unwatched = NEW
    case 'unknown':
    case undefined:
      return { state: 'idle', labelKey: 'unknown' };
    default:
      return { state: 'idle', labelKey: 'unknown' };
  }
}

export default function RecordingsList() {
  const { t } = useTranslation();
  const { auth } = useAppContext();
  const { confirm, toast } = useUiOverlay();
  const { selectedProfile, canAccessDvrPlayback, canManageDvr } = useHouseholdProfiles();

  // State
  const [root, setRoot] = useState<string>(''); // Selected Root ID
  const [path, setPath] = useState<string>(''); // Current relative path
  const [playing, setPlaying] = useState<PlayingState | null>(null);
  const handlePlayerClose = usePlayerHistoryBridge(playing !== null, () => setPlaying(null));
  const [filterMode, setFilterMode] = useState<RecordingsFilter>('all');
  const [sortMode, setSortMode] = useState<RecordingsSort>('newest');

  // Bulk Delete State
  const [selectionMode, setSelectionMode] = useState<boolean>(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  const initialLoad = useRef<boolean>(true);
  const {
    data = null,
    error,
    isPending,
    isFetching,
    refetch: refetchRecordings
  } = useRecordings(root, path);
  const deleteRecordingsMutation = useDeleteRecordingsMutation();
  const deleteLoading = deleteRecordingsMutation.isPending;
  const loading = isPending || (isFetching && !data);
  const pageError = error
    ? toAppError(error, {
      fallbackTitle: 'Unable to load recordings',
      fallbackDetail: 'Try again to refresh the current recordings listing.',
    })
    : null;
  const profileRecordings = filterRecordingsForProfile(selectedProfile, [...(data?.recordings || [])]);

  useEffect(() => {
    setSelectedIds(new Set());
  }, [root, path]);

  useEffect(() => {
    if (canManageDvr) {
      return;
    }

    setSelectionMode(false);
    setSelectedIds(new Set());
  }, [canManageDvr]);

  useEffect(() => {
    if (!data || !initialLoad.current) return;

    initialLoad.current = false;

    if (data.currentRoot && data.currentRoot !== root) {
      setRoot(data.currentRoot);
    }
    if (data.currentPath !== undefined && data.currentPath !== path) {
      setPath(data.currentPath);
    }
  }, [data, path, root]);

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
    if (!canAccessDvrPlayback) return;
    if (selectionMode) return;
    if (!item.recordingId) return;

    setPlaying({
      recordingId: item.recordingId,
      title: item.title || 'Recording',
      durationSeconds: item.durationSeconds ?? 0
    });
  };

  const toggleSelectionMode = () => {
    if (!canManageDvr) return;
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
    if (!canManageDvr) return;
    if (selectedIds.size === 0) return;
    const ok = await confirm({
      title: 'Delete recordings',
      message: t('recordings.confirmDelete', { count: selectedIds.size }),
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
      tone: 'danger',
    });
    if (!ok) return;

    const ids = Array.from(selectedIds);
    await deleteRecordingsMutation.mutateAsync(ids);
    setSelectionMode(false);
    setSelectedIds(new Set());
    toast({ kind: 'success', message: `Deleted ${ids.length} recording(s)` });
  };

  const handleRefresh = () => {
    void refetchRecordings();
  };

  const formatTime = (ts?: number) => {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleString();
  };

  const visibleRecordings = profileRecordings
    .filter((recording) => matchesRecordingsFilter(recording as RecordingItem, filterMode))
    .sort((left, right) => {
      const leftBegin = left.beginUnixSeconds || 0;
      const rightBegin = right.beginUnixSeconds || 0;
      return sortMode === 'oldest' ? leftBegin - rightBegin : rightBegin - leftBegin;
    });
  const activeCount = profileRecordings.filter((recording) => mapRecordingToChip(recording as RecordingItem).state === 'recording').length;
  const resumeCount = profileRecordings.filter((recording) => {
    const resume = (recording as RecordingItem).resume;
    return Boolean(resume && isResumeEligible({
      ...resume,
      posSeconds: resume.posSeconds || 0,
      durationSeconds: resume.durationSeconds || 0,
      finished: resume.finished || false,
    }, recording.durationSeconds));
  }).length;
  const libraryRootLabel = t('recordings.libraryRoot');
  const currentRootLabel = resolveRecordingRootLabel(data?.roots, root || data?.currentRoot, libraryRootLabel);
  const currentPathLabel = data?.currentPath?.trim() || '';
  const librarySummary = currentPathLabel
    ? t('recordings.browsingPath', { root: currentRootLabel, path: currentPathLabel })
    : t('recordings.browsingRoot', { root: currentRootLabel });

  if (!canAccessDvrPlayback) {
    return (
      <div className={[styles.container, 'animate-enter'].join(' ')}>
        <div className={styles.emptyState}>
          <p>{t('recordings.profileBlocked')}</p>
        </div>
      </div>
    );
  }

  if (loading && !data) {
    return (
      <div className={[styles.container, 'animate-enter'].join(' ')}>
        <LoadingSkeleton variant="page" label={t('recordings.loading')} />
      </div>
    );
  }

  if (pageError && !data) {
    return (
      <div className={[styles.container, 'animate-enter'].join(' ')}>
        <ErrorPanel error={pageError} onRetry={handleRefresh} titleAs="h3" />
      </div>
    );
  }

  return (
    <div className={[styles.container, 'animate-enter'].join(' ')}>
      <section className={styles.hero}>
        <div className={styles.heroCopy}>
          <p className={styles.heroEyebrow}>{t('recordings.eyebrow')}</p>
          <h2 className={styles.heroTitle}>{t('recordings.title')}</h2>
          <p className={styles.heroSummary}>{t('recordings.summary')}</p>
          <p className={styles.heroContext}>{librarySummary}</p>
        </div>

        <div className={styles.heroStats}>
          <div className={styles.heroStat}>
            <span className={styles.heroStatLabel}>{t('recordings.metricItems')}</span>
            <span className={styles.heroStatValue}>{profileRecordings.length}</span>
            <span className={styles.heroStatDetail}>{t('recordings.metricItemsDetail')}</span>
          </div>
          <div className={styles.heroStat}>
            <span className={styles.heroStatLabel}>{t('recordings.metricFolders')}</span>
            <span className={styles.heroStatValue}>{data?.directories?.length ?? 0}</span>
            <span className={styles.heroStatDetail}>{t('recordings.metricFoldersDetail')}</span>
          </div>
          <div className={styles.heroStat}>
            <span className={styles.heroStatLabel}>{t('recordings.metricRecording')}</span>
            <span className={styles.heroStatValue}>{activeCount}</span>
            <span className={styles.heroStatDetail}>{t('recordings.metricRecordingDetail')}</span>
          </div>
          <div className={styles.heroStat}>
            <span className={styles.heroStatLabel}>{t('recordings.metricResume')}</span>
            <span className={styles.heroStatValue}>{resumeCount}</span>
            <span className={styles.heroStatDetail}>{t('recordings.metricResumeDetail')}</span>
          </div>
        </div>
      </section>

      {/* Header / Toolbar */}
      <div className={styles.toolbar}>
        <div className={styles.toolbarGroup}>
            <label className={styles.infoLabel}>{t('recordings.location')}</label>
            <select value={root} onChange={handleRootChange} disabled={loading || deleteLoading}>
              {data?.roots?.map(r => (
              <option key={r.id} value={r.id}>{resolveRecordingRootLabel([r], r.id, libraryRootLabel)}</option>
            ))}
          </select>
        </div>

        <div className={styles.breadcrumbs}>
          <span className={styles.crumb} onClick={() => handleNavigate('')}>{t('common.home')}</span>
          {data?.breadcrumbs?.map((crumb, i) => (
            <React.Fragment key={i}>
              <span className={styles.separator}>/</span>
              <span className={styles.crumb} onClick={() => handleNavigate(crumb.path || '')}>{crumb.name}</span>
            </React.Fragment>
          ))}
        </div>

        <div className={styles.toolbarActions}>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleRefresh}
              disabled={loading || deleteLoading}
            >
              {t('common.refresh')}
            </Button>
            {canManageDvr && selectionMode ? (
            <>
              <Button
                variant="danger"
                disabled={selectedIds.size === 0 || deleteLoading}
                onClick={handleBulkDelete}
              >
                {deleteLoading ? t('common.loading') : t('recordings.deleteSelected', { count: selectedIds.size })}
              </Button>
              <Button variant="secondary" onClick={toggleSelectionMode} disabled={deleteLoading}>
                {t('recordings.cancelSelection')}
              </Button>
            </>
          ) : canManageDvr ? (
            <button
              className={styles.iconButton}
              title={t('recordings.selectionMode')}
              onClick={toggleSelectionMode}
            >
              <TrashIcon className={styles.iconSm} />
            </button>
          ) : null}
        </div>

        <div className={styles.workflowControls}>
          <div className={styles.toolbarGroup}>
            <label className={styles.infoLabel} htmlFor="recordings-filter-select">{t('recordings.view')}</label>
            <select
              id="recordings-filter-select"
              data-testid="recordings-filter-select"
              value={filterMode}
              onChange={(e) => setFilterMode(e.target.value as RecordingsFilter)}
              disabled={deleteLoading}
            >
              <option value="all">{t('recordings.viewAll')}</option>
              <option value="active">{t('recordings.viewActive')}</option>
              <option value="resume">{t('recordings.viewResume')}</option>
              <option value="unwatched">{t('recordings.viewUnwatched')}</option>
            </select>
          </div>

          <div className={styles.toolbarGroup}>
            <label className={styles.infoLabel} htmlFor="recordings-sort-select">{t('recordings.sort')}</label>
            <select
              id="recordings-sort-select"
              data-testid="recordings-sort-select"
              value={sortMode}
              onChange={(e) => setSortMode(e.target.value as RecordingsSort)}
              disabled={deleteLoading}
            >
              <option value="newest">{t('recordings.sortNewest')}</option>
              <option value="oldest">{t('recordings.sortOldest')}</option>
            </select>
          </div>
        </div>
      </div>

      {/* Content Grid */}
      <div className={[styles.grid, selectionMode && canManageDvr ? styles.selectionMode : null].filter(Boolean).join(' ')}>
        {/* Directories */}
        {data?.directories?.map((dir, i) => (
          <Card
            key={`dir-${i}`}
            interactive
            onClick={() => handleNavigate(dir.path || '')}
          >
            <CardBody className={styles.itemContent}>
              <div className={styles.iconWrapper}>
                <FolderIcon className={styles.folderIcon} />
              </div>
                <div className={styles.itemDetails}>
                  <span className={styles.itemName}>{dir.name}</span>
                  <StatusChip state="idle" label={t('recordings.directory')} showIcon={false} />
                </div>
              </CardBody>
            </Card>
        ))}

        {/* Recordings */}
        {visibleRecordings.map((rec, i) => {
          const isSelected = rec.recordingId ? selectedIds.has(rec.recordingId) : false;
          const { state, labelKey } = mapRecordingToChip(rec);

          return (
            <Card
              key={`rec-${i}`}
              interactive
              className={[styles.recordingCard, isSelected ? styles.selected : null].filter(Boolean).join(' ')}
              variant={state === 'recording' || state === 'live' ? 'live' : 'standard'}
              onClick={() => selectionMode && canManageDvr && rec.recordingId ? toggleSelect(rec.recordingId) : handlePlay(rec)}
            >
              <CardBody className={styles.itemContent}>
                <div className={styles.iconWrapper}>
                  <FileIcon className={styles.fileIcon} />
                </div>
                <div className={styles.itemDetails}>
                  <div className={styles.itemName} data-testid="recording-title">{rec.title}</div>
                  <div className={`${styles.itemMetaRow} tabular`.trim()}>
                    <span className={styles.metaDate}>{formatTime(rec.beginUnixSeconds)}</span>
                    <span className={styles.metaLength}>{rec.length || formatRecordingLength(rec.durationSeconds)}</span>
                  </div>
                  <p className={styles.itemDesc}>
                    {rec.description || t('recordings.descriptionFallback')}
                  </p>

                  <div className={styles.badgeGroup}>
                    <StatusChip state={state} label={t(`recordings.badges.${labelKey}`)} />
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
                      <div className={styles.resumeContainer}>
                        <RecordingResumeBar
                          resume={safeResume}
                          durationSeconds={rec.durationSeconds}
                        />
                      </div>
                    );
                  })()}
                </div>

                {selectionMode && canManageDvr && (
                  <div className={styles.selectionIndicator}>
                    {isSelected && <CheckCircleIcon className={styles.checkIcon} />}
                  </div>
                )}

                {!selectionMode && (
                  <div className={styles.playOverlay}>
                    <span className={styles.playLabel}>{t('player.play')}</span>
                  </div>
                )}
              </CardBody>
            </Card>
          );
        })}

        {/* Empty State */}
        {(!data?.directories?.length && !visibleRecordings.length) && (
          <div className={styles.emptyState}>
            <p>
              {filterMode === 'all'
                ? t('recordings.emptyLocation')
                : t('recordings.emptyFilter')}
            </p>
          </div>
        )}
      </div>

      {/* V3Player Overlay */}
      {playing && (
        <Suspense fallback={
          <div className={styles.playerFallback}>
            <LoadingSkeleton variant="section" label={t('recordings.loadingPlayer')} />
          </div>
        }>
          <V3Player
            recordingId={playing.recordingId}
            token={auth.token || undefined}
            autoStart={true}
            onClose={handlePlayerClose}
            duration={playing.durationSeconds}
          />
        </Suspense>
      )}
    </div>
  );
}

function resolveRecordingRootLabel(
  roots: Array<{ id?: string; name?: string }> | undefined,
  currentRoot: string | undefined,
  fallbackLabel: string
): string {
  const normalizedRoot = String(currentRoot || '').trim();
  if (!normalizedRoot) {
    return fallbackLabel;
  }

  const match = roots?.find((root) => String(root.id || '').trim() === normalizedRoot);
  return String(match?.name || normalizedRoot).trim() || normalizedRoot;
}

function formatRecordingLength(durationSeconds?: number): string {
  if (!durationSeconds || durationSeconds <= 0) {
    return '0m';
  }

  const totalMinutes = Math.round(durationSeconds / 60);
  if (totalMinutes < 60) {
    return `${totalMinutes}m`;
  }

  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (minutes === 0) {
    return `${hours}h`;
  }

  return `${hours}h ${minutes}m`;
}

function matchesRecordingsFilter(recording: RecordingItem, filterMode: RecordingsFilter): boolean {
  if (filterMode === 'all') {
    return true;
  }

  const status = mapRecordingToChip(recording);
  const resume = recording.resume;
  const hasResumeProgress = !!(resume && isResumeEligible({
    ...resume,
    posSeconds: resume.posSeconds || 0,
    durationSeconds: resume.durationSeconds || 0,
    finished: resume.finished || false,
  }, recording.durationSeconds));

  if (filterMode === 'active') {
    return status.state === 'recording';
  }

  if (filterMode === 'resume') {
    return hasResumeProgress;
  }

  if (filterMode === 'unwatched') {
    return status.labelKey === 'new';
  }

  return true;
}
