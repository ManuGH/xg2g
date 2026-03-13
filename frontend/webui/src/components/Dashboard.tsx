import type { CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ChipState } from './ui/StatusChip';
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus
} from '../hooks/useServerQueries';
import { toAppError } from '../lib/appErrors';
import { ROUTE_MAP } from '../routes';
import { Button, Card, StatusChip } from './ui';
import ErrorPanel from './ErrorPanel';
import LoadingSkeleton from './LoadingSkeleton';
import StreamsList from './StreamsList';
import styles from './Dashboard.module.css';

type HeroTone = 'streaming' | 'control' | 'standby';

export default function Dashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
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
  const currentChannel = receiver?.channel?.name || (receiverUnavailable ? t('common.receiverStandby') : 'Receiver ready');
  const now = receiver?.now;
  const next = receiver?.next;
  const missingChannels = health.epg?.missingChannels || 0;
  const heroTone: HeroTone = streamCount > 0 ? 'streaming' : receiverUnavailable ? 'standby' : 'control';
  const hasProgramProgress = !!(now?.beginTimestamp && now?.durationSec);
  const showReceiverContext = !hasProgramProgress && !receiverUnavailable;
  const progressPercent = hasProgramProgress && now?.beginTimestamp && now?.durationSec
    ? Math.min(100, Math.max(0, (((Date.now() / 1000) - now.beginTimestamp) / now.durationSec) * 100))
    : 0;

  const heroTitle = streamCount > 0
    ? (now?.title || currentChannel)
    : currentChannel;

  const heroSummary = streamCount > 0
    ? (now?.description || t('dashboard.heroStreamingSummary', { count: streamCount }))
    : receiverUnavailable
      ? t('dashboard.heroStandbySummary')
      : next?.title
        ? t('dashboard.heroNextUp', { title: next.title })
        : t('dashboard.heroDefaultSummary');

  const healthChip = mapHealthChip(health.status, t);
  const signalItems = [
    {
      label: t('dashboard.connectedDevices'),
      value: streamCount > 0 ? t('dashboard.sessions', { count: streamCount }) : t('dashboard.readyForFirstSession')
    },
    {
      label: t('dashboard.lastSyncLabel'),
      value: formatTimeAgo(health.receiver?.lastCheck, t)
    },
    {
      label: t('dashboard.guideGaps'),
      value: missingChannels === 0 ? t('dashboard.none') : t('dashboard.missing', { count: missingChannels })
    },
    {
      label: t('dashboard.recorder'),
      value: recording?.serviceName || (recording?.isRecording ? t('dashboard.active') : t('dashboard.idle'))
    },
    {
      label: t('dashboard.versionLabel'),
      value: health.version
    }
  ];

  return (
    <div className={`${styles.page} animate-enter`.trim()}>
      <Card variant="action" className={[styles.heroCard, styles[`hero${capitalize(heroTone)}`]].join(' ')}>
        <div className={styles.heroTop}>
          <div className={styles.heroIdentity}>
            <p className={styles.heroEyebrow}>{heroTone === 'streaming' ? t('dashboard.heroStreamingMoment') : t('dashboard.heroControlDeck')}</p>
            <h2 className={styles.heroTitle}>{heroTitle}</h2>
            <p className={styles.heroSummary}>{heroSummary}</p>
          </div>
          <div className={styles.heroToolbar}>
            <StatusChip state={healthChip.state} label={healthChip.label} />
            <Button variant="secondary" onClick={() => refetch()}>
              {t('common.refresh')}
            </Button>
          </div>
        </div>

        <div className={styles.heroBody}>
          <div className={styles.heroMain}>
            {hasProgramProgress && now?.beginTimestamp && now?.durationSec ? (
              <div className={styles.heroTimeline}>
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
              <div className={styles.heroContextCard}>
                <span className={styles.contextLabel}>{t('dashboard.receiverContext')}</span>
                <span className={styles.contextValue}>{currentChannel}</span>
                <span className={styles.contextDetail}>{t('dashboard.readyForPlayback')}</span>
              </div>
            ) : null}

            <div className={styles.heroActions}>
              <Button onClick={() => navigate(ROUTE_MAP.epg)}>{t('nav.epg')}</Button>
              <Button variant="secondary" onClick={() => navigate(ROUTE_MAP.recordings)}>
                {t('nav.recordings')}
              </Button>
              <Button variant="ghost" onClick={() => navigate(ROUTE_MAP.timers)}>
                {t('nav.timers')}
              </Button>
            </div>
          </div>
        </div>
      </Card>

      <section className={styles.diagnosticsGrid}>
        <Card className={styles.streamSurface}>
          <div className={styles.panelHeader}>
            <div>
              <p className={styles.panelEyebrow}>{t('dashboard.diagnostics')}</p>
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
              <p className={styles.panelEyebrow}>{t('dashboard.signal')}</p>
              <h3 className={styles.panelTitle}>{t('dashboard.receiverAndGuideHealth')}</h3>
            </div>
          </div>
          <div className={styles.statusList}>
            {signalItems.map((item) => (
              <div key={item.label} className={styles.statusRow}>
                <span className={styles.statusLabel}>{item.label}</span>
                <span className={styles.statusValue}>{item.value}</span>
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
