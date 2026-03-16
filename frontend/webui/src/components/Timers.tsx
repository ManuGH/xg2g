// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState } from 'react';
import type { Timer } from '../client-ts';
import EditTimerDialog from './EditTimerDialog';
import { useAppContext } from '../context/AppContext';
import { debugWarn } from '../utils/logging';
import { useUiOverlay } from '../context/UiOverlayContext';
import { useDeleteTimerMutation, useDvrCapabilities, useTimers } from '../hooks/useServerQueries';
import { Button } from './ui';
import styles from './Timers.module.css';

function formatDateTime(ts: number | undefined): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleString([], {
    weekday: 'short',
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  });
}

export default function Timers() {
  const { channels } = useAppContext();
  const { confirm, toast } = useUiOverlay();
  const [editingTimer, setEditingTimer] = useState<Timer | null>(null);
  const [isCreatingTimer, setIsCreatingTimer] = useState<boolean>(false);
  const {
    data: timers = [],
    error,
    isPending,
    isFetching,
    refetch: refetchTimers
  } = useTimers();
  const { data: capabilities = null } = useDvrCapabilities();
  const deleteTimerMutation = useDeleteTimerMutation();
  const loading = isPending || isFetching;
  const errorMessage = error ? 'Failed to load timers. Ensure backend is running and authenticated.' : null;

  const handleDelete = async (timer: Timer): Promise<void> => {
    const ok = await confirm({
      title: 'Delete Timer',
      message: `Delete timer for "${timer.name}"?`,
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
      tone: 'danger',
    });
    if (!ok) return;

    try {
      if (timer.timerId) {
        await deleteTimerMutation.mutateAsync(timer.timerId);
      } else {
        debugWarn('No timerId found');
      }
    } catch (err) {
      const error = err as Error;
      toast({ kind: 'error', message: 'Failed to delete timer', details: error.message });
    }
  };

  return (
    <div className={`${styles.view} animate-enter`.trim()}>
      <div className={styles.toolbar}>
        <h2>Scheduled Recordings</h2>
        <div className={styles.actions}>
          <Button
            size="sm"
            onClick={() => setIsCreatingTimer(true)}
          >
            New Timer
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void refetchTimers()}
            disabled={loading || deleteTimerMutation.isPending}
          >
            Refresh
          </Button>
        </div>
      </div>

      {loading && <div className={styles.loading}>Loading...</div>}
      {errorMessage && <div className={styles.errorBanner} role="alert">{errorMessage}</div>}

      {!loading && !errorMessage && timers.length === 0 && (
        <div className={styles.empty}>No timers scheduled.</div>
      )}

      <div className={styles.list}>
        {timers.map((t, idx) => (
          <div key={t.timerId || idx} className={styles.card}>
            <div className={styles.info}>
              <div className={styles.name}>{t.name}</div>
              <div className={styles.service}>{t.serviceName || t.serviceRef}</div>
              <div className={styles.time}>
                {formatDateTime(t.begin)} - {formatDateTime(t.end)}
              </div>
              <div
                className={[
                  styles.state,
                  t.state === 'recording' ? styles.stateRecording : styles.stateScheduled,
                ].filter(Boolean).join(' ')}
              >
                {t.state}
              </div>
            </div>
            <div className={styles.actions}>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setEditingTimer(t)}
              >
                Edit
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => handleDelete(t)}
                disabled={deleteTimerMutation.isPending}
              >
                Delete
              </Button>
            </div>
          </div>
        ))}
      </div>

      {editingTimer && (
        <EditTimerDialog
          timer={editingTimer}
          capabilities={capabilities || undefined}
          availableServices={channels.channels}
          onClose={() => setEditingTimer(null)}
          onSave={async () => {
            await refetchTimers();
          }}
        />
      )}

      {isCreatingTimer && (
        <EditTimerDialog
          capabilities={capabilities || undefined}
          availableServices={channels.channels}
          onClose={() => setIsCreatingTimer(false)}
          onSave={async () => {
            await refetchTimers();
          }}
        />
      )}
    </div>
  );
}
