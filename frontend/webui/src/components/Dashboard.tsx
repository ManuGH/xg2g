import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { ChipState } from './ui/StatusChip';
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus,
  useLogs
} from '../hooks/useServerQueries';
import { useAppContext } from '../context/AppContext';
import { Button, Card, StatusChip } from './ui';
import StreamsList from './StreamsList';
import styles from './Dashboard.module.css';

interface MetricCardProps {
  eyebrow: string;
  value: string;
  detail: string;
  chipState: ChipState;
  chipLabel: string;
  accent?: 'action' | 'live';
}

type HeroTone = 'streaming' | 'control' | 'standby';

export default function Dashboard() {
  const { t } = useTranslation();
  const { setView } = useAppContext();
  const { data: health, error, isLoading, refetch } = useSystemHealth();
  const { data: receiver } = useReceiverCurrent();
  const { data: streams = [] } = useStreams();
  const { data: recording } = useDvrStatus();
  const { data: logs = [], isLoading: logsLoading, error: logsError } = useLogs(6);

  if (error) return <div className={styles.errorText}>Error: {(error as Error).message}</div>;
  if (isLoading || !health) return <div className={styles.loadingState}>{t('common.loading')}</div>;

  const streamCount = streams.length;
  const receiverUnavailable = receiver?.status === 'unavailable';
  const currentChannel = receiver?.channel?.name || (receiverUnavailable ? t('common.receiverStandby') : 'Receiver ready');
  const now = receiver?.now;
  const next = receiver?.next;
  const missingChannels = health.epg?.missingChannels || 0;
  const heroTone: HeroTone = streamCount > 0 ? 'streaming' : receiverUnavailable ? 'standby' : 'control';
  const hasProgramProgress = !!(now?.beginTimestamp && now?.durationSec);
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
  const epgChip = mapEpgChip(health.epg?.status, missingChannels, t);
  const receiverChip = receiverUnavailable
    ? { state: 'idle' as ChipState, label: t('dashboard.standby') }
    : { state: 'success' as ChipState, label: t('dashboard.receiverOnline') };
  const recordingChip = recording
    ? {
        state: (recording.isRecording ? 'recording' : 'idle') as ChipState,
        label: recording.isRecording ? t('dashboard.recordingActive') : t('dashboard.recorderIdle')
      }
    : { state: 'warning' as ChipState, label: t('dashboard.recorderUnknown') };

  const heroFacts = [
    {
      label: t('dashboard.connectedDevices'),
      value: `${streamCount}`,
      detail: streamCount > 0 ? t('dashboard.liveViewerSessions') : t('dashboard.readyForFirstSession')
    },
    {
      label: t('dashboard.receiverLink'),
      value: receiverUnavailable ? t('dashboard.standby') : t('dashboard.healthy'),
      detail: t('dashboard.lastSync', { time: formatTimeAgo(health.receiver?.lastCheck, t) })
    },
    {
      label: t('dashboard.recorder'),
      value: recording?.isRecording ? t('dashboard.active') : t('dashboard.idle'),
      detail: recording?.serviceName || t('dashboard.noActiveRecordingTask')
    }
  ];

  const metrics: MetricCardProps[] = [
    {
      eyebrow: t('dashboard.metricStreaming'),
      value: `${streamCount}`.padStart(2, '0'),
      detail: streamCount > 0 ? t('dashboard.activePlaybackSessions') : t('dashboard.noActiveSessions'),
      chipState: streamCount > 0 ? 'live' : 'idle',
      chipLabel: streamCount > 0 ? t('dashboard.liveTraffic') : t('dashboard.idle'),
      accent: streamCount > 0 ? 'action' : undefined
    },
    {
      eyebrow: t('dashboard.metricReceiver'),
      value: receiverUnavailable ? t('dashboard.standby') : t('dashboard.online'),
      detail: t('dashboard.lastSync', { time: formatTimeAgo(health.receiver?.lastCheck, t) }),
      chipState: receiverChip.state,
      chipLabel: receiverChip.label
    },
    {
      eyebrow: t('dashboard.metricEpg'),
      value: missingChannels === 0 ? t('dashboard.synced') : `${missingChannels}`,
      detail: missingChannels === 0 ? t('dashboard.allChannelsHaveData') : t('dashboard.channelsMissingGuideData'),
      chipState: epgChip.state,
      chipLabel: epgChip.label
    },
    {
      eyebrow: t('dashboard.metricRecorder'),
      value: recording?.isRecording ? t('dashboard.active') : t('dashboard.idle'),
      detail: recording?.serviceName || t('dashboard.uptimeLabel', { time: formatUptime(health.uptimeSeconds || 0) }),
      chipState: recordingChip.state,
      chipLabel: recordingChip.label,
      accent: recording?.isRecording ? 'live' : undefined
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
            <StatusChip state={recordingChip.state} label={recordingChip.label} />
            <Button variant="secondary" onClick={() => refetch()}>
              {t('common.refresh')}
            </Button>
          </div>
        </div>

        <div className={styles.heroBody}>
          <div className={styles.heroMain}>
            <div className={styles.heroChipRow}>
              <StatusChip state={receiverChip.state} label={receiverChip.label} />
              <StatusChip state={epgChip.state} label={epgChip.label} />
              <StatusChip
                state={streamCount > 0 ? 'live' : 'idle'}
                label={streamCount > 0 ? t('dashboard.activeSessions', { count: streamCount }) : t('dashboard.noActiveSessionsChip')}
              />
            </div>

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
            ) : (
              <div className={styles.heroContextCard}>
                <span className={styles.contextLabel}>{t('dashboard.receiverContext')}</span>
                <span className={styles.contextValue}>{currentChannel}</span>
                <span className={styles.contextDetail}>
                  {receiverUnavailable ? t('common.receiverUnavailable') : t('dashboard.readyForPlayback')}
                </span>
              </div>
            )}

            <div className={styles.heroActions}>
              <Button onClick={() => setView('epg')}>{t('nav.epg')}</Button>
              <Button variant="secondary" onClick={() => setView('recordings')}>
                {t('nav.recordings')}
              </Button>
              <Button variant="ghost" onClick={() => setView('timers')}>
                {t('nav.timers')}
              </Button>
            </div>
          </div>

          <div className={styles.heroAside}>
            {heroFacts.map((fact) => (
              <div key={fact.label} className={styles.heroFact}>
                <span className={styles.factLabel}>{fact.label}</span>
                <span className={styles.factValue}>{fact.value}</span>
                <span className={styles.factDetail}>{fact.detail}</span>
              </div>
            ))}
          </div>
        </div>
      </Card>

      <section className={styles.metricGrid}>
        {metrics.map((metric) => (
          <MetricCard key={metric.eyebrow} {...metric} />
        ))}
      </section>

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

        <div className={styles.sideStack}>
          <Card className={styles.signalSurface}>
            <div className={styles.panelHeader}>
              <div>
                <p className={styles.panelEyebrow}>{t('dashboard.signal')}</p>
                <h3 className={styles.panelTitle}>{t('dashboard.receiverAndGuideHealth')}</h3>
              </div>
              <StatusChip state={healthChip.state} label={healthChip.label} />
            </div>
            <div className={styles.statusList}>
              <div className={styles.statusRow}>
                <span className={styles.statusLabel}>{t('dashboard.receiverLabel')}</span>
                <span className={styles.statusValue}>{receiverUnavailable ? t('dashboard.standby') : t('dashboard.connected')}</span>
              </div>
              <div className={styles.statusRow}>
                <span className={styles.statusLabel}>{t('dashboard.lastSyncLabel')}</span>
                <span className={styles.statusValue}>{formatTimeAgo(health.receiver?.lastCheck, t)}</span>
              </div>
              <div className={styles.statusRow}>
                <span className={styles.statusLabel}>{t('dashboard.guideGaps')}</span>
                <span className={styles.statusValue}>{missingChannels === 0 ? t('dashboard.none') : t('dashboard.missing', { count: missingChannels })}</span>
              </div>
              <div className={styles.statusRow}>
                <span className={styles.statusLabel}>{t('dashboard.versionLabel')}</span>
                <span className={styles.statusValue}>{health.version}</span>
              </div>
            </div>
          </Card>

          <Card className={styles.feedSurface}>
            <div className={styles.panelHeader}>
              <div>
                <p className={styles.panelEyebrow}>{t('dashboard.feed')}</p>
                <h3 className={styles.panelTitle}>{t('dashboard.recentLogs')}</h3>
              </div>
            </div>

            {logsError ? (
              <div className={styles.errorText}>{(logsError as Error).message}</div>
            ) : logsLoading ? (
              <div className={styles.loadingInline}>{t('dashboard.loadingLogs')}</div>
            ) : logs.length === 0 ? (
              <div className={styles.emptyStateCompact}>{t('dashboard.noRecentLogs')}</div>
            ) : (
              <div className={styles.feedList}>
                {logs.map((log, index) => {
                  const chip = mapLogLevel(log.level || '', t);
                  return (
                    <div key={`${log.time || 'log'}-${index}`} className={styles.feedItem}>
                      <div className={styles.feedItemTop}>
                        <span className={`${styles.feedTime} tabular`.trim()}>
                          {log.time ? new Date(log.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '--:--'}
                        </span>
                        <StatusChip state={chip.state} label={chip.label} showIcon={false} />
                      </div>
                      <p className={styles.feedMessage}>{log.message}</p>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>
        </div>
      </section>
    </div>
  );
}

function MetricCard({ eyebrow, value, detail, chipState, chipLabel, accent }: MetricCardProps) {
  return (
    <Card className={[
      styles.metricCard,
      accent === 'action' ? styles.metricAction : null,
      accent === 'live' ? styles.metricLive : null
    ].filter(Boolean).join(' ')}>
      <div className={styles.metricHeader}>
        <span className={styles.metricEyebrow}>{eyebrow}</span>
        <StatusChip state={chipState} label={chipLabel} showIcon={false} />
      </div>
      <div className={styles.metricValue}>{value}</div>
      <p className={styles.metricDetail}>{detail}</p>
    </Card>
  );
}

function mapHealthChip(status: string | undefined, t: (key: string) => string): { state: ChipState; label: string } {
  if (status === 'ok') return { state: 'success', label: t('dashboard.systemHealthy') };
  if (!status) return { state: 'warning', label: t('dashboard.healthUnknown') };
  return { state: 'warning', label: t('dashboard.systemDegraded') };
}

function mapEpgChip(status: string | undefined, missingChannels: number, t: (key: string) => string): { state: ChipState; label: string } {
  if (status === 'ok' && missingChannels === 0) return { state: 'success', label: t('dashboard.guideSynced') };
  if (status === 'missing' || missingChannels > 0) return { state: 'warning', label: t('dashboard.guidePartial') };
  return { state: 'error', label: t('dashboard.guideOffline') };
}

function mapLogLevel(
  level: string,
  t: (key: string, options?: Record<string, unknown>) => string
): { state: ChipState; label: string } {
  const normalized = level.toLowerCase();
  if (normalized === 'error') return { state: 'error', label: t('dashboard.logLevelError', { defaultValue: 'Error' }) };
  if (normalized === 'warn' || normalized === 'warning') {
    return { state: 'warning', label: t('dashboard.logLevelWarn', { defaultValue: 'Warn' }) };
  }
  if (normalized === 'info' || !normalized) {
    return { state: 'success', label: t('dashboard.logLevelInfo', { defaultValue: 'Info' }) };
  }
  return { state: 'success', label: normalized.toUpperCase() };
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
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
