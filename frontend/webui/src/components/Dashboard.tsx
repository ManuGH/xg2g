import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { ChipState } from './ui/StatusChip';
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus
} from '../hooks/useServerQueries';
import { toAppError } from '../lib/appErrors';
import { Card, StatusChip } from './ui';
import ErrorPanel from './ErrorPanel';
import LoadingSkeleton from './LoadingSkeleton';
import StreamsList from './StreamsList';
import styles from './Dashboard.module.css';

type SummaryTone = 'streaming' | 'control' | 'standby';

export default function Dashboard() {
  const { t } = useTranslation();
  const { data: health, error, isLoading, refetch } = useSystemHealth();
  const { data: receiver } = useReceiverCurrent();
  const { data: streams = [] } = useStreams();
  const { data: recording } = useDvrStatus();

  if (error) {
    return (
      <div className={`${styles.page} animate-enter`.trim()}>
        <ErrorPanel
          error={toAppError(error, {
            fallbackTitle: t('dashboard.loadErrorTitle', { defaultValue: 'Unable to load system health' }),
            fallbackDetail: t('dashboard.loadErrorDetail', { defaultValue: 'Try again to refresh the current receiver and guide status.' }),
          })}
          onRetry={() => { void refetch(); }}
        />
      </div>
    );
  }
  if (isLoading || !health) {
    return (
      <div className={`${styles.page} animate-enter`.trim()}>
        <LoadingSkeleton variant="section" label={t('common.loading', { defaultValue: 'Loading...' })} />
      </div>
    );
  }

  const streamCount = streams.length;
  const receiverUnavailable = receiver?.status === 'unavailable';
  const currentChannel = receiver?.channel?.name || (receiverUnavailable ? t('common.receiverStandby') : t('dashboard.receiverReady'));
  const now = receiver?.now;
  const next = receiver?.next;
  const missingChannels = health.epg?.missingChannels || 0;
  const summaryTone: SummaryTone = streamCount > 0 ? 'streaming' : receiverUnavailable ? 'standby' : 'control';
  const hasProgramProgress = !!(now?.beginTimestamp && now?.durationSec);
  const showReceiverContext = !hasProgramProgress && !receiverUnavailable;
  const progressPercent = hasProgramProgress && now?.beginTimestamp && now?.durationSec
    ? Math.min(100, Math.max(0, (((Date.now() / 1000) - now.beginTimestamp) / now.durationSec) * 100))
    : 0;

  const summaryTitle = currentChannel;

  const summaryDescription = streamCount > 0
    ? (now?.description || t('dashboard.heroStreamingSummary', { count: streamCount }))
    : receiverUnavailable
      ? t('dashboard.heroStandbySummary')
      : next?.title
        ? t('dashboard.heroNextUp', { title: next.title })
        : t('dashboard.readOnlySummary');

  const healthChip = mapHealthChip(health.status, t);
  const guideHealthLabel = missingChannels === 0
    ? t('dashboard.guideSynced')
    : t('dashboard.missing', { count: missingChannels });
  const recorderLabel = recording?.serviceName || (recording?.isRecording ? t('dashboard.recordingActive') : t('dashboard.recorderIdle'));
  const summaryItems = [
    {
      label: t('dashboard.receiverLabel'),
      value: receiverUnavailable ? t('dashboard.standby') : currentChannel,
      detail: receiverUnavailable ? t('dashboard.heroStandbySummary') : (now?.title || t('dashboard.readyForPlayback'))
    },
    {
      label: t('dashboard.activeStreams'),
      value: streamCount > 0 ? t('dashboard.sessions', { count: streamCount }) : t('dashboard.readyForFirstSession'),
      detail: streamCount > 0 ? t('dashboard.activePlaybackSessions') : t('dashboard.noActiveSessions')
    },
    {
      label: t('dashboard.guideHealth'),
      value: guideHealthLabel,
      detail: t('dashboard.lastSync', { time: formatTimeAgo(health.receiver?.lastCheck, t) })
    },
    {
      label: t('dashboard.recorder'),
      value: recorderLabel,
      detail: recording?.isRecording ? t('dashboard.recordingActive') : t('dashboard.noActiveRecordingTask')
    }
  ];
  const systemFacts = [
    {
      label: t('dashboard.receiverLabel'),
      value: receiverUnavailable ? t('dashboard.standby') : t('dashboard.connected'),
      detail: currentChannel
    },
    {
      label: t('dashboard.lastSyncLabel'),
      value: formatTimeAgo(health.receiver?.lastCheck, t),
      detail: t('dashboard.readOnlySummary')
    },
    {
      label: t('dashboard.guideHealth'),
      value: guideHealthLabel,
      detail: missingChannels === 0 ? t('dashboard.allChannelsHaveData') : t('dashboard.channelsMissingGuideData')
    },
    {
      label: t('dashboard.recorder'),
      value: recorderLabel,
      detail: recording?.isRecording ? t('dashboard.recordingActive') : t('dashboard.recorderIdle')
    },
    {
      label: t('dashboard.versionLabel'),
      value: health.version || t('common.notAvailable'),
      detail: healthChip.label
    }
  ];

  return (
    <div className={`${styles.page} animate-enter`.trim()}>
      <Card variant="action" className={[styles.summaryCard, styles[`summary${capitalize(summaryTone)}`]].join(' ')}>
        <div className={styles.summaryHeader}>
          <div className={styles.summaryIdentity}>
            <p className={styles.summaryEyebrow}>{t('dashboard.controlSummary')}</p>
            <h2 className={styles.summaryTitle}>{summaryTitle}</h2>
            <p className={styles.summaryDescription}>{summaryDescription}</p>
          </div>
          <div className={styles.summaryChips}>
            <StatusChip state={healthChip.state} label={healthChip.label} />
            <StatusChip
              state={streamCount > 0 ? 'live' : 'idle'}
              label={streamCount > 0 ? t('dashboard.sessions', { count: streamCount }) : t('dashboard.noSessions')}
            />
          </div>
        </div>

        <div className={styles.summaryMetrics}>
          {summaryItems.map((item) => (
            <div key={item.label} className={styles.metricCard}>
              <span className={styles.metricLabel}>{item.label}</span>
              <span className={styles.metricValue}>{item.value}</span>
              <span className={styles.metricDetail}>{item.detail}</span>
            </div>
          ))}
        </div>

        {hasProgramProgress && now?.beginTimestamp && now?.durationSec ? (
          <div className={styles.contextCard}>
            <div className={styles.timelineHeader}>
              <span className={styles.timelineLabel}>{t('dashboard.onReceiverNow')}</span>
              <span className={styles.timelineMeta}>
                {new Date(now.beginTimestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                {' \u2192 '}
                {new Date((now.beginTimestamp + now.durationSec) * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
              </span>
            </div>
            <div className={styles.timelineTrack}>
              <div
                className={styles.timelineFill}
                style={{ '--xg2g-progress': `${progressPercent}%` } as CSSProperties & { [key: string]: string }}
              />
            </div>
            {next?.title && <p className={styles.timelineNext}>{t('dashboard.upNext', { title: next.title })}</p>}
          </div>
        ) : showReceiverContext ? (
          <div className={styles.contextCard}>
            <span className={styles.contextLabel}>{t('dashboard.receiverContext')}</span>
            <span className={styles.contextValue}>{currentChannel}</span>
            <span className={styles.contextDetail}>{t('dashboard.readyForPlayback')}</span>
          </div>
        ) : null}
      </Card>

      <section className={styles.diagnosticsGrid}>
        <Card className={styles.streamSurface}>
          <div className={styles.panelHeader}>
            <div>
              <p className={styles.panelEyebrow}>{t('dashboard.operatorSessions')}</p>
              <h3 className={styles.panelTitle}>{t('dashboard.activeStreams')}</h3>
            </div>
            <StatusChip
              state={streamCount > 0 ? 'live' : 'idle'}
              label={streamCount > 0 ? t('dashboard.sessions', { count: streamCount }) : t('dashboard.noSessions')}
            />
          </div>
          <div className={styles.panelBody}>
            {streamCount > 0 ? (
              <StreamsList compact />
            ) : (
              <div className={styles.emptyState}>
                <p className={styles.emptyTitle}>{t('dashboard.noActiveStreams')}</p>
                <p className={styles.emptyText}>{t('dashboard.startPlaybackHint')}</p>
              </div>
            )}
          </div>
        </Card>

        <Card className={styles.signalSurface}>
          <div className={styles.panelHeader}>
            <div>
              <p className={styles.panelEyebrow}>{t('dashboard.systemState')}</p>
              <h3 className={styles.panelTitle}>{t('dashboard.receiverAndGuideHealth')}</h3>
            </div>
          </div>
          <div className={styles.statusList}>
            {systemFacts.map((item) => (
              <div key={item.label} className={styles.statusRow}>
                <span className={styles.statusLabel}>{item.label}</span>
                <span className={styles.statusValue}>{item.value}</span>
                <span className={styles.statusDetail}>{item.detail}</span>
              </div>
            ))}
          </div>
        </Card>
      </section>
    </div>
  );
}

function mapHealthChip(status: string | undefined, t: (key: string) => string): { state: ChipState; label: string } {
  if (status === 'ok') return { state: 'success', label: t('dashboard.systemHealthy') };
  if (!status) return { state: 'warning', label: t('dashboard.healthUnknown') };
  return { state: 'warning', label: t('dashboard.systemDegraded') };
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function formatTimeAgo(dateString: string | undefined, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (!dateString) return t('dashboard.timeNever');
  const date = new Date(dateString);
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return t('dashboard.timeNever');

  const now = new Date();
  const diffSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (diffSeconds < 60) return t('dashboard.timeJustNow');
  if (diffSeconds < 3600) return t('dashboard.timeMinutesAgo', { count: Math.floor(diffSeconds / 60) });
  if (diffSeconds < 86400) return t('dashboard.timeHoursAgo', { count: Math.floor(diffSeconds / 3600) });
  return date.toLocaleDateString();
}
