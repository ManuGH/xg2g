// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Phase 2E: Recordings View refactored to primitives (Card + StatusChip)
// CTO Contract: No custom surfaces/badges, layout-only CSS, tabular technical data

import React, { useState, useEffect, lazy, Suspense, useRef, type CSSProperties } from 'react';
import { type RecordingItem } from '../client-ts';
import { useAppContext } from '../context/AppContext';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import { filterRecordingsForProfile } from '../features/household/model';
import { useTranslation } from 'react-i18next';
import RecordingResumeBar, { isResumeEligible } from '../features/resume/RecordingResumeBar';
import { usePlayerHistoryBridge } from '../features/player/usePlayerHistoryBridge';
import { useUiOverlay } from '../context/UiOverlayContext';
import { useRecordings } from '../hooks/useServerQueries';
import { toAppError } from '../lib/appErrors';
import { Button, Card, CardBody, StatusChip, type ChipState } from './ui';
import ErrorPanel from './ErrorPanel';
import LoadingSkeleton from './LoadingSkeleton';
import styles from './Recordings.module.css';

const importV3Player = () => import('../features/player/components/V3Player');
let v3PlayerModulePromise: ReturnType<typeof importV3Player> | null = null;

const loadV3Player = () => {
  if (!v3PlayerModulePromise) {
    v3PlayerModulePromise = importV3Player();
  }
  return v3PlayerModulePromise;
};

const V3Player = lazy(loadV3Player);

// Simple Icons
const FolderIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M19.5 21a3 3 0 003-3v-4.5a3 3 0 00-3-3h-15a3 3 0 00-3 3V18a3 3 0 003 3h15zM1.5 10.146V6a3 3 0 013-3h5.379a2.25 2.25 0 011.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 013 3v1.146A4.483 4.483 0 0019.5 9h-15a4.483 4.483 0 00-1.89.417z" />
  </svg>
);

const PlayCircleIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="none" className={className}>
    <circle cx="24" cy="24" r="23" fill="currentColor" fillOpacity="0.14" stroke="currentColor" strokeWidth="2" />
    <path d="M20 16.8c0-1.54 1.67-2.5 3.01-1.72l11.16 6.47c1.32.77 1.32 2.67 0 3.44L23 31.46c-1.34.78-3-.18-3-1.72V16.8Z" fill="currentColor" />
  </svg>
);

const TrashIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path fillRule="evenodd" d="M16.5 4.478v.227a48.816 48.816 0 0 1 3.878.512.75.75 0 1 1-.49 1.478 47.4 47.4 0 0 0-3.899-.514H7.991a47.403 47.403 0 0 0-3.899.513.75.75 0 0 1-.492-1.478 48.817 48.817 0 0 1 3.879-.512v-.227c0-1.168.968-2.147 2.135-2.288 1.477-.178 3.013-.178 4.49 0 1.167.14 2.135 1.12 2.135 2.288ZM8.33 12a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75H9.08a.75.75 0 0 1-.75-.75V12Zm3.75 0a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75h-.008a.75.75 0 0 1-.75-.75V12Zm3.75 0a.75.75 0 0 1 .75-.75h.008a.75.75 0 0 1 .75.75v5.25a.75.75 0 0 1-.75.75h-.008a.75.75 0 0 1-.75-.75V12Z" clipRule="evenodd" />
    <path d="M5 9.75a.75.75 0 0 1 .75-.75h12.5a.75.75 0 0 1 .75.75v7.5a9 9 0 0 1-9 9h-4.5a9 9 0 0 1-9-9v-7.5Z" />
  </svg>
);

const PencilIcon = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
    <path d="M16.862 3.487a2.625 2.625 0 1 1 3.712 3.713l-10.5 10.5a2.25 2.25 0 0 1-1.06.598l-3.11.777a.75.75 0 0 1-.91-.91l.777-3.11a2.25 2.25 0 0 1 .598-1.06l10.5-10.5ZM18.8 4.548a1.125 1.125 0 0 0-1.591 0l-.71.71 1.59 1.59.71-.709a1.125 1.125 0 0 0 0-1.59ZM16.43 7.318l-1.59-1.59-8.41 8.41a.75.75 0 0 0-.2.354l-.42 1.68 1.68-.42a.75.75 0 0 0 .354-.2l8.586-8.234Z" />
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
  description: string;
  beginUnixSeconds?: number;
  lengthLabel: string;
  durationSeconds: number;
  startPositionSeconds: number;
  suppressResumePrompt: boolean;
}

type RecordingsFilter = 'all' | 'active' | 'resume' | 'unwatched';
type RecordingsSort = 'newest' | 'oldest';
type RecordingChipLabel = 'watched' | 'resume' | 'rec' | 'scheduled' | 'failed' | 'unknown' | 'new';

const RECORDING_PREVIEW_ACCENTS = [
  'var(--accent-action)',
  'var(--accent-live)',
  'color-mix(in srgb, var(--accent-action) 52%, var(--surface-highlight) 48%)',
  'color-mix(in srgb, var(--accent-live) 46%, var(--surface-highlight) 54%)',
] as const;

const recordingThumbnailObjectUrlCache = new Map<string, string>();
const recordingThumbnailMissCache = new Set<string>();

function buildRecordingAdminHeaders(authToken?: string | null, includeJSON: boolean = false): Record<string, string> {
  const headers: Record<string, string> = {};
  const normalizedToken = String(authToken || '').trim();
  if (normalizedToken) {
    headers.Authorization = `Bearer ${normalizedToken}`;
  }
  if (includeJSON) {
    headers['Content-Type'] = 'application/json';
  }
  return headers;
}

async function readRecordingAdminErrorMessage(response: Response): Promise<string> {
  try {
    const payload = await response.json() as { detail?: string; title?: string; message?: string };
    return payload.detail || payload.title || payload.message || `Request failed (${response.status})`;
  } catch {
    return `Request failed (${response.status})`;
  }
}

async function requestRecordingDelete(recordingId: string, authToken?: string | null): Promise<void> {
  const response = await fetch(`/api/v3/recordings/${encodeURIComponent(recordingId)}/delete`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: buildRecordingAdminHeaders(authToken),
  });

  if (!response.ok) {
    throw new Error(await readRecordingAdminErrorMessage(response));
  }
}

async function requestRecordingRename(recordingId: string, title: string, authToken?: string | null): Promise<void> {
  const response = await fetch(`/api/v3/recordings/${encodeURIComponent(recordingId)}/rename`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: buildRecordingAdminHeaders(authToken, true),
    body: JSON.stringify({ title }),
  });

  if (!response.ok) {
    throw new Error(await readRecordingAdminErrorMessage(response));
  }
}

function normalizeRecordingRenameInput(value: string): string {
  return value.trim().replace(/\s+/g, ' ');
}

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

function resolveRecordingPreviewStyle(recording: RecordingItem): CSSProperties {
  const seed = `${recording.recordingId || ''}:${recording.title || ''}`;
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) {
    hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  }

  return {
    ['--recording-accent' as string]: RECORDING_PREVIEW_ACCENTS[hash % RECORDING_PREVIEW_ACCENTS.length],
  };
}

function resolveRecordingPreviewMonogram(title?: string): string {
  const normalized = String(title || '').trim();
  if (!normalized) {
    return 'REC';
  }

  const words = normalized.split(/\s+/).filter(Boolean);
  if (words.length === 1) {
    const [firstWord] = words;
    return firstWord ? firstWord.slice(0, 2).toUpperCase() : 'REC';
  }

  return words.slice(0, 2).map((word) => word.slice(0, 1).toUpperCase()).join('');
}

function resolveRecordingThumbnailUrl(recording: RecordingItem): string | null {
  const recordingId = String(recording.recordingId || '').trim();
  if (!recordingId) {
    return null;
  }

  return `/api/v3/recordings/${encodeURIComponent(recordingId)}/thumbnail.jpg`;
}

function RecordingPreviewArtwork({ recording, authToken }: { recording: RecordingItem; authToken?: string | null }) {
  const thumbnailUrl = resolveRecordingThumbnailUrl(recording);
  const [resolvedThumbnailUrl, setResolvedThumbnailUrl] = useState<string | null>(() => {
    if (!thumbnailUrl) {
      return null;
    }
    return recordingThumbnailObjectUrlCache.get(thumbnailUrl) ?? null;
  });
  const [thumbnailUnavailable, setThumbnailUnavailable] = useState<boolean>(() => {
    if (!thumbnailUrl) {
      return true;
    }
    return recordingThumbnailMissCache.has(thumbnailUrl);
  });

  useEffect(() => {
    if (!thumbnailUrl) {
      setResolvedThumbnailUrl(null);
      setThumbnailUnavailable(true);
      return;
    }

    const cachedObjectUrl = recordingThumbnailObjectUrlCache.get(thumbnailUrl);
    if (cachedObjectUrl) {
      setResolvedThumbnailUrl(cachedObjectUrl);
      setThumbnailUnavailable(false);
      return;
    }

    if (recordingThumbnailMissCache.has(thumbnailUrl)) {
      setResolvedThumbnailUrl(null);
      setThumbnailUnavailable(true);
      return;
    }

    const controller = new AbortController();
    const normalizedToken = String(authToken || '').trim();
    const headers: Record<string, string> = {};
    if (normalizedToken) {
      headers.Authorization = `Bearer ${normalizedToken}`;
    }

    void fetch(thumbnailUrl, {
      method: 'GET',
      credentials: 'same-origin',
      headers,
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`thumbnail ${response.status}`);
        }
        return response.blob();
      })
      .then((blob) => {
        if (controller.signal.aborted) {
          return;
        }
        const objectUrl = URL.createObjectURL(blob);
        recordingThumbnailObjectUrlCache.set(thumbnailUrl, objectUrl);
        setResolvedThumbnailUrl(objectUrl);
        setThumbnailUnavailable(false);
      })
      .catch(() => {
        if (controller.signal.aborted) {
          return;
        }
        recordingThumbnailMissCache.add(thumbnailUrl);
        setResolvedThumbnailUrl(null);
        setThumbnailUnavailable(true);
      });

    return () => {
      controller.abort();
    };
  }, [authToken, thumbnailUrl]);

  return (
    <>
      {resolvedThumbnailUrl && !thumbnailUnavailable ? (
        <img
          className={styles.mediaPreviewImage}
          data-testid="recording-thumbnail"
          src={resolvedThumbnailUrl}
          alt=""
          draggable={false}
        />
      ) : null}
      {!resolvedThumbnailUrl || thumbnailUnavailable ? (
        <div className={styles.mediaPreviewBackdrop}>
          <div className={styles.mediaPreviewWordmark}>{resolveRecordingPreviewMonogram(recording.title)}</div>
        </div>
      ) : null}
    </>
  );
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
  const [preplayRecording, setPreplayRecording] = useState<RecordingItem | null>(null);
  const [launchingRecordingId, setLaunchingRecordingId] = useState<string | null>(null);
  const closePlayerOverlay = () => {
    setPlaying(null);
    setPreplayRecording(null);
    setLaunchingRecordingId(null);
    exitPlaybackFullscreen();
  };
  const handlePlayerClose = usePlayerHistoryBridge(playing !== null, closePlayerOverlay);
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
  const [adminBusyIds, setAdminBusyIds] = useState<Set<string>>(new Set());
  const deleteLoading = adminBusyIds.size > 0;
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
    if (!canAccessDvrPlayback || !data?.recordings?.length) {
      return;
    }

    const preloadTimer = window.setTimeout(() => {
      void loadV3Player();
    }, 180);

    return () => {
      window.clearTimeout(preloadTimer);
    };
  }, [canAccessDvrPlayback, data?.recordings?.length]);

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

  const handlePlay = async (
    item: RecordingItem,
    options: { startPositionSeconds?: number; suppressResumePrompt?: boolean } = {}
  ) => {
    if (!canAccessDvrPlayback) return;
    if (selectionMode) return;
    const recordingId = item.recordingId;
    if (!recordingId) return;

    setLaunchingRecordingId(recordingId);

    try {
      await loadV3Player();
    } catch {
      setLaunchingRecordingId(null);
      return;
    }

    setPreplayRecording(null);
    setPlaying({
      recordingId,
      title: item.title || 'Recording',
      description: item.description || '',
      beginUnixSeconds: item.beginUnixSeconds,
      lengthLabel: item.length || formatRecordingLength(item.durationSeconds ?? item.resume?.durationSeconds),
      durationSeconds: item.durationSeconds ?? item.resume?.durationSeconds ?? 0,
      startPositionSeconds: options.startPositionSeconds ?? 0,
      suppressResumePrompt: options.suppressResumePrompt ?? true
    });
    setLaunchingRecordingId(null);
  };

  const handleOpenPreplay = (item: RecordingItem) => {
    if (!canAccessDvrPlayback) return;
    if (selectionMode) return;
    if (!item.recordingId) return;

    void loadV3Player();
    setPreplayRecording(item);
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

  const updateAdminBusy = (recordingIds: string[], busy: boolean) => {
    setAdminBusyIds(prev => {
      const next = new Set(prev);
      for (const recordingId of recordingIds) {
        if (!recordingId) {
          continue;
        }
        if (busy) {
          next.add(recordingId);
        } else {
          next.delete(recordingId);
        }
      }
      return next;
    });
  };

  const handleRenameRecording = async (recording: RecordingItem) => {
    if (!canManageDvr) return;
    const recordingId = String(recording.recordingId || '').trim();
    if (!recordingId || adminBusyIds.has(recordingId)) return;

    const currentTitle = String(recording.title || '').trim() || t('recordings.untitled');
    const nextTitle = window.prompt(t('recordings.renamePrompt', { title: currentTitle }), currentTitle);
    if (nextTitle === null) return;

    const normalizedTitle = normalizeRecordingRenameInput(nextTitle);
    if (!normalizedTitle || normalizedTitle === currentTitle) {
      return;
    }

    updateAdminBusy([recordingId], true);
    try {
      await requestRecordingRename(recordingId, normalizedTitle, auth.token);
      await refetchRecordings();
      toast({ kind: 'success', message: t('recordings.renameSuccess') });
    } catch (renameError) {
      const message = renameError instanceof Error && renameError.message
        ? renameError.message
        : t('recordings.renameError');
      toast({ kind: 'error', message });
    } finally {
      updateAdminBusy([recordingId], false);
    }
  };

  const handleDeleteRecording = async (recording: RecordingItem) => {
    if (!canManageDvr) return;
    const recordingId = String(recording.recordingId || '').trim();
    if (!recordingId || adminBusyIds.has(recordingId)) return;

    const ok = await confirm({
      title: t('common.delete'),
      message: t('recordings.confirmDeleteOne', { title: recording.title || t('recordings.untitled') }),
      confirmLabel: t('common.delete'),
      cancelLabel: t('common.cancel'),
      tone: 'danger',
    });
    if (!ok) return;

    updateAdminBusy([recordingId], true);
    try {
      await requestRecordingDelete(recordingId, auth.token);
      await refetchRecordings();
      toast({ kind: 'success', message: t('recordings.deleteSuccess', { count: 1 }) });
    } catch (deleteError) {
      const message = deleteError instanceof Error && deleteError.message
        ? deleteError.message
        : t('recordings.deleteError');
      toast({ kind: 'error', message });
    } finally {
      updateAdminBusy([recordingId], false);
    }
  };

  const handleBulkDelete = async () => {
    if (!canManageDvr) return;
    if (selectedIds.size === 0) return;
    const ok = await confirm({
      title: t('common.delete'),
      message: t('recordings.confirmDelete', { count: selectedIds.size }),
      confirmLabel: t('common.delete'),
      cancelLabel: t('common.cancel'),
      tone: 'danger',
    });
    if (!ok) return;

    const ids = Array.from(selectedIds);
    updateAdminBusy(ids, true);
    try {
      const results = await Promise.allSettled(ids.map((recordingId) => requestRecordingDelete(recordingId, auth.token)));
      const successCount = results.filter((result) => result.status === 'fulfilled').length;
      const failedCount = ids.length - successCount;

      if (successCount > 0) {
        await refetchRecordings();
      }

      if (failedCount === 0) {
        toast({ kind: 'success', message: t('recordings.deleteSuccess', { count: successCount }) });
      } else if (successCount > 0) {
        toast({ kind: 'warning', message: t('recordings.deletePartial', { success: successCount, failed: failedCount }) });
      } else {
        toast({ kind: 'error', message: t('recordings.deleteError') });
      }

      setSelectionMode(false);
      setSelectedIds(new Set());
    } finally {
      updateAdminBusy(ids, false);
    }
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
  const preplayResume = preplayRecording ? resolveEligibleResume(preplayRecording) : null;
  const preplayStatus = preplayRecording ? mapRecordingToChip(preplayRecording) : null;
  const isPreplayLaunching = Boolean(preplayRecording?.recordingId && preplayRecording.recordingId === launchingRecordingId);
  const preplayProgressPercent = preplayRecording ? resolveRecordingProgressPercent(preplayRecording) : null;
  const playingProgressPercent = playing
    ? resolvePlaybackProgressPercent(playing.startPositionSeconds, playing.durationSeconds)
    : null;
  const libraryRootLabel = t('recordings.libraryRoot');
  const currentRootLabel = resolveRecordingRootLabel(data?.roots, root || data?.currentRoot, libraryRootLabel);
  const currentPathLabel = data?.currentPath?.trim() || '';
  const librarySummary = currentPathLabel
    ? t('recordings.browsingPath', { root: currentRootLabel, path: currentPathLabel })
    : t('recordings.browsingRoot', { root: currentRootLabel });
  const filterOptions: Array<{ value: RecordingsFilter; label: string }> = [
    { value: 'all', label: t('recordings.viewAll') },
    { value: 'active', label: t('recordings.viewActive') },
    { value: 'resume', label: t('recordings.viewResume') },
    { value: 'unwatched', label: t('recordings.viewUnwatched') },
  ];
  const sortOptions: Array<{ value: RecordingsSort; label: string }> = [
    { value: 'newest', label: t('recordings.sortNewest') },
    { value: 'oldest', label: t('recordings.sortOldest') },
  ];
  const continueWatching = !selectionMode && filterMode === 'all'
    ? visibleRecordings.filter((recording) => Boolean(resolveEligibleResume(recording))).slice(0, 4)
    : [];
  const continueWatchingIds = new Set(
    continueWatching.map((recording) => recording.recordingId).filter((recordingId): recordingId is string => Boolean(recordingId))
  );
  const primaryRecordings = continueWatchingIds.size > 0
    ? visibleRecordings.filter((recording) => !recording.recordingId || !continueWatchingIds.has(recording.recordingId))
    : visibleRecordings;

  const renderRecordingCard = (rec: RecordingItem, variant: 'grid' | 'featured' = 'grid') => {
    const recordingId = String(rec.recordingId || '').trim();
    const isSelected = rec.recordingId ? selectedIds.has(rec.recordingId) : false;
    const { state, labelKey } = mapRecordingToChip(rec);
    const eligibleResume = resolveEligibleResume(rec);
    const progressPercent = resolveRecordingProgressPercent(rec);
    const isAdminBusy = Boolean(recordingId && adminBusyIds.has(recordingId));
    const canRenameRecording = Boolean(canManageDvr && recordingId && rec.localWritable === true);
    const showPreviewChip = variant === 'featured'
      && (labelKey === 'rec' || labelKey === 'scheduled' || labelKey === 'failed' || labelKey === 'unknown');
    const statusMetaLabel = !eligibleResume && (labelKey === 'rec' || labelKey === 'scheduled' || labelKey === 'failed' || labelKey === 'unknown')
      ? t(`recordings.badges.${labelKey}`)
      : null;
    const showCardDescription = variant === 'featured' && Boolean(rec.description);
    const isSelectableCard = selectionMode && canManageDvr && Boolean(rec.recordingId);
    const canOpenPlayer = !selectionMode && canAccessDvrPlayback && Boolean(rec.recordingId);
    const cardIsInteractive = isSelectableCard || canOpenPlayer;
    const handleCardClick = cardIsInteractive
      ? () => {
          if (isSelectableCard && rec.recordingId) {
            toggleSelect(rec.recordingId);
            return;
          }
          if (eligibleResume) {
            handleOpenPreplay(rec);
            return;
          }
          void handlePlay(rec, {
            startPositionSeconds: 0,
            suppressResumePrompt: true,
          });
        }
      : undefined;

    return (
      <Card
        key={rec.recordingId || `${variant}-${rec.title || 'recording'}`}
        interactive={cardIsInteractive}
        className={[
          styles.recordingCard,
          variant === 'featured' ? styles.recordingCardFeatured : null,
          isSelected ? styles.selected : null,
        ].filter(Boolean).join(' ')}
        variant={state === 'recording' || state === 'live' ? 'live' : 'standard'}
        onClick={handleCardClick}
      >
        <CardBody className={styles.mediaCardBody}>
          <div className={styles.mediaPreview} style={resolveRecordingPreviewStyle(rec)}>
            <RecordingPreviewArtwork recording={rec} authToken={auth.token} />
            {showPreviewChip ? (
              <div className={styles.mediaPreviewTop}>
                <StatusChip state={state} label={t(`recordings.badges.${labelKey}`)} />
              </div>
            ) : null}
            {canManageDvr && !selectionMode && recordingId ? (
              <div className={styles.mediaCardActions}>
                {canRenameRecording ? (
                  <button
                    type="button"
                    className={styles.mediaActionButton}
                    title={t('recordings.renameAction')}
                    aria-label={t('recordings.renameAction')}
                    disabled={isAdminBusy}
                    onClick={(event) => {
                      event.preventDefault();
                      event.stopPropagation();
                      void handleRenameRecording(rec);
                    }}
                  >
                    <PencilIcon className={styles.iconSm} />
                  </button>
                ) : null}
                <button
                  type="button"
                  className={[styles.mediaActionButton, styles.mediaActionButtonDanger].join(' ')}
                  title={t('common.delete')}
                  aria-label={t('common.delete')}
                  disabled={isAdminBusy}
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void handleDeleteRecording(rec);
                  }}
                >
                  <TrashIcon className={styles.iconSm} />
                </button>
              </div>
            ) : null}
            <div className={styles.mediaPreviewBottom}>
              <span className={styles.mediaPreviewLabel}>
                {eligibleResume
                  ? t('recordings.resumeProgress', { time: formatResumeClock(eligibleResume.posSeconds) })
                  : formatRecordingCardDate(rec.beginUnixSeconds)}
              </span>
              <span className={styles.mediaDuration}>
                {rec.length || formatRecordingLength(rec.durationSeconds || rec.resume?.durationSeconds)}
              </span>
            </div>
            {progressPercent !== null && (
              <div className={styles.mediaProgressTrack} aria-hidden="true">
                <div className={styles.mediaProgressFill} style={{ width: `${progressPercent}%` }}></div>
              </div>
            )}
            {selectionMode && canManageDvr && (
              <div className={styles.selectionIndicator}>
                {isSelected && <CheckCircleIcon className={styles.checkIcon} />}
              </div>
            )}
            {!selectionMode && (
              <div className={styles.playOverlay} aria-hidden="true">
                <PlayCircleIcon className={styles.playOverlayIcon} />
              </div>
            )}
          </div>
          <div className={styles.mediaText}>
            <div className={styles.itemName} data-testid="recording-title">{rec.title || t('recordings.untitled')}</div>
            <div className={`${styles.itemMetaRow} tabular`.trim()}>
              <span className={styles.metaDate}>{formatTime(rec.beginUnixSeconds)}</span>
              {eligibleResume ? (
                <span className={styles.metaResume}>
                  {t('recordings.resumeProgress', { time: formatResumeClock(eligibleResume.posSeconds) })}
                </span>
              ) : statusMetaLabel ? (
                <span className={styles.metaStatus}>{statusMetaLabel}</span>
              ) : null}
            </div>
            {showCardDescription ? (
              <p className={styles.itemDesc}>{rec.description}</p>
            ) : null}
          </div>
        </CardBody>
      </Card>
    );
  };

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

  if (playing) {
    return (
      <div className={[styles.container, styles.watchPage, 'animate-enter'].join(' ')}>
        <div className={styles.watchPageHeader}>
          <div className={styles.watchPageLead}>
            <span className={styles.watchPageLabel}>{t('nav.recordings')}</span>
          </div>
          <Button
            variant="secondary"
            size="sm"
            className={styles.watchPageClose}
            onClick={() => handlePlayerClose()}
          >
            {t('common.close')}
          </Button>
        </div>

        <div className={styles.watchPageBody}>
          <Suspense fallback={
            <div className={styles.watchPageFallback}>
              <div className={[styles.preplayStage, styles.watchPageFallbackStage].join(' ')} aria-hidden="true">
                <div className={styles.preplayStageCenter}>
                  <PlayCircleIcon className={styles.preplayStageIcon} />
                </div>
                {playingProgressPercent !== null && (
                  <div className={styles.preplayStageProgress}>
                    <div
                      className={styles.preplayStageProgressFill}
                      style={{ width: `${playingProgressPercent}%` }}
                    ></div>
                  </div>
                )}
                <div className={styles.preplayStageDock}>
                  <div className={styles.preplayStageCopy}>
                    <div className={styles.preplayEyebrow}>{t('recordings.preplayEyebrow')}</div>
                    <h2 className={styles.preplayTitle}>{playing.title || t('recordings.untitled')}</h2>
                    <div className={styles.preplayMeta}>
                      {playing.beginUnixSeconds ? <span>{formatTime(playing.beginUnixSeconds)}</span> : null}
                      <span>{playing.lengthLabel}</span>
                    </div>
                  </div>
                  <div className={styles.preplayStageActions}>
                    <div className={styles.playerLaunchStatus} role="status" aria-live="polite">
                      <span className={styles.playerLaunchSpinner} aria-hidden="true" />
                      <div className={styles.playerLaunchCopy}>
                        <div className={styles.playerLaunchLabel}>{t('recordings.loadingPlayer')}</div>
                        <p className={styles.playerLaunchHint}>{t('recordings.loadingPlayerHint')}</p>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          }>
            <V3Player
              recordingId={playing.recordingId}
              recordingTitle={playing.title}
              recordingDescription={playing.description}
              recordingDateLabel={playing.beginUnixSeconds ? formatTime(playing.beginUnixSeconds) : undefined}
              recordingLengthLabel={playing.lengthLabel}
              layoutMode="page"
              token={auth.token || undefined}
              autoStart={true}
              onClose={handlePlayerClose}
              duration={playing.durationSeconds}
              startPositionSeconds={playing.startPositionSeconds}
              suppressResumePrompt={playing.suppressResumePrompt}
            />
          </Suspense>
        </div>
      </div>
    );
  }

  return (
    <div className={[styles.container, 'animate-enter'].join(' ')}>
      <section className={styles.hero}>
        <div className={styles.heroCopy}>
          <p className={styles.heroEyebrow}>{t('recordings.eyebrow')}</p>
          <div className={styles.heroHeadingRow}>
            <div>
              <h2 className={styles.heroTitle}>{t('recordings.title')}</h2>
              <p className={styles.heroSummary}>{t('recordings.summary')}</p>
            </div>
            <div className={styles.heroSignals}>
              <div className={styles.heroSignal}>
                <span className={styles.heroSignalValue}>{resumeCount}</span>
                <span className={styles.heroSignalLabel}>{t('recordings.metricResume')}</span>
              </div>
              <div className={styles.heroSignal}>
                <span className={styles.heroSignalValue}>{activeCount}</span>
                <span className={styles.heroSignalLabel}>{t('recordings.metricRecording')}</span>
              </div>
              <div className={styles.heroSignal}>
                <span className={styles.heroSignalValue}>{profileRecordings.length}</span>
                <span className={styles.heroSignalLabel}>{t('recordings.metricItems')}</span>
              </div>
            </div>
          </div>
          <p className={styles.heroContext}>{librarySummary}</p>
        </div>
      </section>

      <div className={styles.browserBar}>
        <div className={styles.toolbarPrimary}>
          <div className={styles.toolbarGroup}>
            <label className={styles.infoLabel}>{t('recordings.location')}</label>
            <select
              className={styles.rootSelect}
              value={root}
              onChange={handleRootChange}
              disabled={loading || deleteLoading}
            >
              {data?.roots?.map(r => (
                <option key={r.id} value={r.id}>{resolveRecordingRootLabel([r], r.id, libraryRootLabel)}</option>
              ))}
            </select>
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

        <div className={styles.segmentedControls}>
          <div className={styles.segmentGroup} role="tablist" aria-label={t('recordings.view')}>
            {filterOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                role="tab"
                aria-selected={filterMode === option.value}
                className={[
                  styles.segmentButton,
                  filterMode === option.value ? styles.segmentButtonActive : null,
                ].filter(Boolean).join(' ')}
                onClick={() => setFilterMode(option.value)}
                disabled={deleteLoading}
              >
                {option.label}
              </button>
            ))}
          </div>

          <div className={styles.segmentGroup} role="tablist" aria-label={t('recordings.sort')}>
            {sortOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                role="tab"
                aria-selected={sortMode === option.value}
                className={[
                  styles.segmentButton,
                  sortMode === option.value ? styles.segmentButtonActive : null,
                ].filter(Boolean).join(' ')}
                onClick={() => setSortMode(option.value)}
                disabled={deleteLoading}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {data?.directories?.length ? (
        <section className={styles.section}>
          <div className={styles.sectionHeader}>
            <h3 className={styles.sectionTitle}>{t('recordings.metricFolders')}</h3>
          </div>
          <div className={styles.directoryRail}>
            {data.directories.map((dir, i) => (
              <Card
                key={`dir-${i}`}
                interactive
                className={styles.directoryCard}
                onClick={() => handleNavigate(dir.path || '')}
              >
                <CardBody className={styles.directoryCardBody}>
                  <div className={styles.directoryCardIcon}>
                    <FolderIcon className={styles.iconSm} />
                  </div>
                  <div className={styles.directoryCardCopy}>
                    <span className={styles.directoryCardTitle}>{dir.name}</span>
                    <span className={styles.directoryCardMeta}>{t('recordings.directory')}</span>
                  </div>
                </CardBody>
              </Card>
            ))}
          </div>
        </section>
      ) : null}

      {continueWatching.length > 0 && (
        <section className={styles.section}>
          <div className={styles.sectionHeader}>
            <h3 className={styles.sectionTitle}>{t('recordings.continueWatchingTitle')}</h3>
          </div>
          <div className={styles.resumeShelf}>
            {continueWatching.map((recording) => renderRecordingCard(recording, 'featured'))}
          </div>
        </section>
      )}

      {primaryRecordings.length > 0 && (
        <section className={styles.section}>
          <div className={styles.sectionHeader}>
            <h3 className={styles.sectionTitle}>
              {continueWatching.length > 0 ? t('recordings.latestTitle') : t('recordings.title')}
            </h3>
          </div>
          <div className={[styles.grid, selectionMode && canManageDvr ? styles.selectionMode : null].filter(Boolean).join(' ')}>
            {primaryRecordings.map((recording) => renderRecordingCard(recording))}
          </div>
        </section>
      )}

      {(!data?.directories?.length && !visibleRecordings.length) && (
        <div className={styles.emptyState}>
          <p>
            {filterMode === 'all'
              ? t('recordings.emptyLocation')
              : t('recordings.emptyFilter')}
          </p>
        </div>
      )}

      {preplayRecording && preplayStatus && (
        <div
          className={styles.preplayOverlay}
          role="dialog"
          aria-modal="true"
          aria-labelledby="recording-preplay-title"
          aria-busy={isPreplayLaunching}
          onClick={() => {
            if (!isPreplayLaunching) {
              setPreplayRecording(null);
            }
          }}
        >
          <div
            className={[styles.preplayPanel, isPreplayLaunching ? styles.preplayPanelBusy : null].filter(Boolean).join(' ')}
            onClick={(event) => event.stopPropagation()}
          >
            <button
              type="button"
              className={styles.preplayClose}
              aria-label={t('recordings.closePreplay')}
              disabled={isPreplayLaunching}
              onClick={() => setPreplayRecording(null)}
            >
              ×
            </button>
            <div className={styles.preplayStage}>
              <div className={styles.preplayStageCenter}>
                <PlayCircleIcon className={styles.preplayStageIcon} />
              </div>
              {preplayProgressPercent !== null && (
                <div className={styles.preplayStageProgress}>
                  <div
                    className={styles.preplayStageProgressFill}
                    style={{ width: `${preplayProgressPercent}%` }}
                  ></div>
                </div>
              )}
              <div className={styles.preplayStageDock}>
                <div className={styles.preplayStageCopy}>
                  <div className={styles.preplayEyebrow}>{t('recordings.preplayEyebrow')}</div>
                  <h2 id="recording-preplay-title" className={styles.preplayTitle}>
                    {preplayRecording.title || t('recordings.untitled')}
                  </h2>
                  <div className={styles.preplayMeta}>
                    <span>{formatTime(preplayRecording.beginUnixSeconds)}</span>
                    <span>{preplayRecording.length || formatRecordingLength(preplayRecording.durationSeconds)}</span>
                    {preplayStatus.labelKey !== 'new' ? (
                      <StatusChip
                        state={preplayStatus.state}
                        label={t(`recordings.badges.${preplayStatus.labelKey}`)}
                      />
                    ) : null}
                  </div>
                </div>
                <div className={styles.preplayStageActions}>
                  {isPreplayLaunching ? (
                    <div className={styles.playerLaunchStatus} role="status" aria-live="polite">
                      <span className={styles.playerLaunchSpinner} aria-hidden="true" />
                      <div className={styles.playerLaunchCopy}>
                        <div className={styles.playerLaunchLabel}>{t('recordings.loadingPlayer')}</div>
                        <p className={styles.playerLaunchHint}>{t('recordings.loadingPlayerHint')}</p>
                      </div>
                    </div>
                  ) : (
                    <div className={styles.preplayActions}>
                      <Button
                        variant="primary"
                        onClick={() => {
                          void handlePlay(preplayRecording, {
                            startPositionSeconds: preplayResume?.posSeconds ?? 0,
                            suppressResumePrompt: true,
                          });
                        }}
                      >
                        {preplayResume
                          ? t('recordings.resumeAction', { time: formatResumeClock(preplayResume.posSeconds) })
                          : t('recordings.playAction')}
                      </Button>
                      {preplayResume && (
                        <Button
                          variant="secondary"
                          onClick={() => {
                            void handlePlay(preplayRecording, {
                              startPositionSeconds: 0,
                              suppressResumePrompt: true,
                            });
                          }}
                        >
                          {t('recordings.restartAction')}
                        </Button>
                      )}
                    </div>
                  )}
                </div>
              </div>
            </div>
            <div className={styles.preplayBody}>
              <div className={styles.preplayMain}>
                {preplayRecording.description ? (
                  <p className={styles.preplayDescription}>{preplayRecording.description}</p>
                ) : null}
                {preplayResume && (
                  <div className={styles.preplayResume}>
                    <RecordingResumeBar
                      resume={preplayResume}
                      durationSeconds={preplayRecording.durationSeconds}
                    />
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

    </div>
  );
}

function exitPlaybackFullscreen(): void {
  if (typeof document === 'undefined' || !document.fullscreenElement) {
    return;
  }
  if (typeof document.exitFullscreen !== 'function') {
    return;
  }

  document.exitFullscreen().catch(() => undefined);
}

function resolveEligibleResume(recording: RecordingItem) {
  const resume = recording.resume;
  if (!resume) {
    return null;
  }

  const normalizedResume = {
    ...resume,
    posSeconds: resume.posSeconds || 0,
    durationSeconds: resume.durationSeconds || recording.durationSeconds || 0,
    finished: resume.finished || false,
  };

  return isResumeEligible(normalizedResume, recording.durationSeconds) ? normalizedResume : null;
}

function resolveRecordingProgressPercent(recording: RecordingItem): number | null {
  const resume = resolveEligibleResume(recording);
  if (!resume) {
    return null;
  }

  return resolvePlaybackProgressPercent(resume.posSeconds || 0, resume.durationSeconds || recording.durationSeconds || 0);
}

function resolvePlaybackProgressPercent(positionSeconds?: number, durationSeconds?: number): number | null {
  if (!durationSeconds || durationSeconds <= 0) {
    return null;
  }

  const percent = (Math.max(0, positionSeconds || 0) / durationSeconds) * 100;
  return Math.max(0, Math.min(100, percent));
}

function formatResumeClock(value: number): string {
  if (!Number.isFinite(value) || value < 0) {
    return '00:00';
  }

  const total = Math.floor(value);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
}

function formatRecordingCardDate(ts?: number): string {
  if (!ts) {
    return '';
  }

  return new Date(ts * 1000).toLocaleDateString([], {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
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
