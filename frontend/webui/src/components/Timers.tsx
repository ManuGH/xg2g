// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Timer } from '../client-ts';
import EditTimerDialog from './EditTimerDialog';
import { useAppContext } from '../context/AppContext';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import { filterServicesForProfile, filterTimersForProfile } from '../features/household/model';
import { debugWarn } from '../utils/logging';
import { useUiOverlay } from '../context/UiOverlayContext';
import { useDeleteTimerMutation, useDvrCapabilities, useTimers } from '../hooks/useServerQueries';
import { ROUTE_MAP } from '../routes';
import { Button, EmptyState } from './ui';
import LegacyRouteNotice from './LegacyRouteNotice';
import styles from './Timers.module.css';

interface TimersProps {
  showLegacyNotice?: boolean;
}

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

export default function Timers({ showLegacyNotice = true }: TimersProps) {
  const { t } = useTranslation();
  const { channels } = useAppContext();
  const { confirm, toast } = useUiOverlay();
  const { selectedProfile, canManageDvr } = useHouseholdProfiles();
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
  const errorMessage = error ? t('timers.loadError') : null;
  const visibleTimers = filterTimersForProfile(selectedProfile, timers);
  const availableServices = filterServicesForProfile(selectedProfile, channels.channels);

  const handleDelete = async (timer: Timer): Promise<void> => {
    if (!canManageDvr) {
      return;
    }

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
      {showLegacyNotice ? (
        <LegacyRouteNotice
          parentLabel={t('nav.epg')}
          description={t('legacyRoute.timersDescription', {
            defaultValue: 'For most workflows, schedule recordings directly from Live TV. This page stays available for direct timer management.',
          })}
          route={ROUTE_MAP.epg}
        />
      ) : null}
      <div className={styles.toolbar}>
        <h2>{t('timers.scheduledRecordings')}</h2>
        <div className={styles.actions}>
          <Button
            size="sm"
            onClick={() => setIsCreatingTimer(true)}
            disabled={!canManageDvr}
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
      {!canManageDvr && !loading && !errorMessage && (
        <EmptyState
          icon="⛔"
          title={t('timers.dvrBlocked')}
        />
      )}

      {!loading && !errorMessage && canManageDvr && visibleTimers.length === 0 && (
        <EmptyState
          icon="○"
          title={t('timers.empty')}
          description={t('timers.emptyHint')}
        />
      )}

      <div className={styles.list}>
        {visibleTimers.map((t, idx) => (
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
                disabled={!canManageDvr}
              >
                Edit
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => handleDelete(t)}
                disabled={deleteTimerMutation.isPending || !canManageDvr}
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
          availableServices={availableServices}
          onClose={() => setEditingTimer(null)}
          onSave={async () => {
            await refetchTimers();
          }}
        />
      )}

      {isCreatingTimer && (
        <EditTimerDialog
          capabilities={capabilities || undefined}
          availableServices={availableServices}
          onClose={() => setIsCreatingTimer(false)}
          onSave={async () => {
            await refetchTimers();
          }}
        />
      )}
    </div>
  );
}
