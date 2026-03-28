// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { StreamSession } from '../client-ts';
import { useUiOverlay } from '../context/UiOverlayContext';
import { useStopStreamMutation, useStreams } from '../hooks/useServerQueries';
import { Button, StatusChip } from './ui';
import styles from './Streams.module.css';

interface StreamsListProps {
  compact?: boolean;
}

function assertNever(x: never): never {
  throw new Error(`Unhandled StreamSessionState: ${String(x)}`);
}

/**
 * mapStreamToChip
 * Strictly follows Domain Truth Mapping (CTO Hardrail) for P3-2.
 * Maps exact OpenAPI enum states to UI semantics.
 */
function mapStreamToChip(session: StreamSession, label: string) {
  switch (session.state) { // xg2g:allow-webui-logic
    case 'starting':
      return { state: 'idle', label } as const;
    case 'buffering':
      return { state: 'warning', label } as const;
    case 'active':
      return { state: 'live', label } as const;
    case 'stalled':
      return { state: 'warning', label } as const;
    case 'ending':
      return { state: 'idle', label } as const;
    case 'idle':
      return { state: 'idle', label } as const;
    case 'error':
      return { state: 'error', label } as const;
  }
  // Fail-closed for unknown states (runtime safety + strict exhaustiveness)
  return assertNever(session.state);
}

const maskIP = (ip: string | undefined): string => {
  if (!ip) return '';
  return ip.replace(/\.\d+$/, '.xxx');
};

const formatDuration = (date: Date): string => {
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return `${diffMins}m`;
  const diffHours = Math.floor(diffMins / 60);
  return `${diffHours}h ${diffMins % 60}m`;
};

const formatClientFamily = (family: string | undefined): string => {
  switch (family) {
    case 'chromium_hlsjs':
      return 'Chromium';
    case 'firefox_hlsjs':
      return 'Firefox';
    case 'safari_native':
      return 'Safari';
    case 'ios_safari_native':
      return 'iOS Safari';
    default:
      return family ?? '';
  }
};

const formatPreferredHlsEngine = (engine: string | undefined): string => {
  switch (engine) {
    case 'hlsjs':
      return 'hls.js';
    case 'native':
      return 'Native HLS';
    default:
      return engine ?? '';
  }
};

const shouldShowDeviceType = (deviceType: string | undefined): boolean =>
  Boolean(deviceType && deviceType !== 'web');

export default function StreamsList({ compact = false }: StreamsListProps) {
  const { t } = useTranslation();
  const { confirm, toast } = useUiOverlay();
  const { data: streams = [], error } = useStreams();
  const stopStreamMutation = useStopStreamMutation();
  const [stoppingSessionId, setStoppingSessionId] = useState<string | null>(null);
  const count = streams.length;

  if (count === 0 && !error) return null;

  const handleStop = async (session: StreamSession) => {
    const channelName = session.channelName || t('dashboard.sessionUnknownChannel');
    const ok = await confirm({
      title: t('dashboard.stopStreamTitle'),
      message: t('dashboard.stopStreamMessage', { channel: channelName }),
      confirmLabel: t('common.stop'),
      cancelLabel: t('common.close'),
      tone: 'danger',
    });

    if (!ok) return;

    setStoppingSessionId(session.sessionId);
    try {
      await stopStreamMutation.mutateAsync(session.sessionId);
      toast({
        kind: 'success',
        message: t('dashboard.stopStreamSuccess', { channel: channelName }),
      });
    } catch (mutationError) {
      const details = mutationError instanceof Error ? mutationError.message : undefined;
      toast({
        kind: 'error',
        message: t('dashboard.stopStreamError', { channel: channelName }),
        details,
      });
    } finally {
      setStoppingSessionId(null);
    }
  };

  return (
    <div className={[styles.section, compact ? styles.sectionCompact : null].filter(Boolean).join(' ')}>
      {!compact && (
        <h3 className={styles.heading}>
          {t('dashboard.activeStreams')} <span className="tabular">({count})</span>
        </h3>
      )}
      {error && <p className={styles.errorText}>{t('dashboard.streamDetailsError')}</p>}

      <div className={styles.list} role="list">
        {streams.map((s: StreamSession) => {
          const chip = mapStreamToChip(
            s,
            t(`player.statusStates.${s.detailedState ?? s.state}`, { defaultValue: s.detailedState ?? s.state })
          );
          const isStopping = stopStreamMutation.isPending && stoppingSessionId === s.sessionId;
          const metaItems = [
            {
              key: 'client',
              label: t('dashboard.sessionClient'),
              value: formatClientFamily(s.clientFamily) || t('common.notAvailable'),
            },
            {
              key: 'player',
              label: t('dashboard.sessionPlayer'),
              value: formatPreferredHlsEngine(s.preferredHlsEngine) || t('common.notAvailable'),
            },
            shouldShowDeviceType(s.deviceType) ? {
              key: 'device',
              label: t('dashboard.sessionDevice'),
              value: s.deviceType || t('common.notAvailable'),
            } : null,
            s.clientIp ? {
              key: 'ip',
              label: t('dashboard.sessionIp'),
              value: maskIP(s.clientIp),
            } : null,
          ].filter(Boolean) as Array<{ key: string; label: string; value: string }>;

          return (
            <article key={s.sessionId} className={styles.row} role="listitem">
              <div className={styles.rowPrimary}>
                <div className={styles.rowHeading}>
                  <div className={styles.rowTitleBlock}>
                    <div className={styles.streamChannel}>{s.channelName || t('dashboard.sessionUnknownChannel')}</div>
                    <p className={styles.streamProgram}>
                      {s.program?.title || t('dashboard.sessionProgramFallback')}
                    </p>
                  </div>
                  <StatusChip state={chip.state} label={chip.label} />
                </div>

                <div className={styles.metaList}>
                  {metaItems.map((item) => (
                    <div key={`${s.sessionId}-${item.key}`} className={styles.metaPill}>
                      <span className={styles.metaLabel}>{item.label}</span>
                      <span>{item.value}</span>
                    </div>
                  ))}
                </div>

                <p className={styles.sessionMeta}>
                  {t('common.session')} <span className="tabular">{s.sessionId}</span>
                </p>
              </div>

              <div className={styles.rowRuntime}>
                <span className={styles.runtimeLabel}>{t('dashboard.sessionRuntime')}</span>
                <span className={`${styles.runtimeValue} tabular`.trim()}>
                  {s.startedAt ? formatDuration(new Date(s.startedAt)) : t('common.notAvailable')}
                </span>
              </div>

              <div className={styles.rowAction}>
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => { void handleStop(s); }}
                  disabled={stopStreamMutation.isPending}
                >
                  {isStopping ? t('dashboard.stoppingStream') : t('common.stop')}
                </Button>
              </div>
            </article>
          );
        })}
      </div>
    </div>
  );
}
