import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { ChipState } from './ui/StatusChip';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus
} from '../hooks/useServerQueries';
import { toAppError } from '../lib/appErrors';
import { buildEpgRoute, buildRecordingsRoute, buildSettingsRoute, ROUTE_MAP } from '../routes';
import { Button, Card, StatusChip } from './ui';
import ErrorPanel from './ErrorPanel';
import LoadingSkeleton from './LoadingSkeleton';
import StreamsList from './StreamsList';
import ContinueWatchingRail from '../features/resume/ContinueWatchingRail';
import styles from './Dashboard.module.css';

type SummaryTone = 'streaming' | 'control' | 'standby';


export default function Dashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const {
    canAccessDvrPlayback,
    canManageDvr,
    canAccessSettings,
  } = useHouseholdProfiles();
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
  const currentChannel = receiver?.channel?.name;
  const now = receiver?.now;
  const next = receiver?.next;
  const missingChannels = health.epg?.missingChannels || 0;
  const summaryTone: SummaryTone = streamCount > 0 ? 'streaming' : receiverUnavailable ? 'standby' : 'control';

  const summaryTitle = receiverUnavailable
    ? t('dashboard.heroStandbyTitle', { defaultValue: 'Bereit für Wiedergabe' })
    : (currentChannel || t('dashboard.receiverReady', { defaultValue: 'Live-TV' }));

  const summaryDescription = streamCount > 0
    ? (now?.description || t('dashboard.heroStreamingSummary', { count: streamCount }))
    : receiverUnavailable
      ? t('dashboard.heroStandbySummary')
      : next?.title
        ? t('dashboard.heroNextUp', { title: next.title })
        : t('dashboard.heroDefaultSummary');

  const healthChip = mapHealthChip(health.status, t);
  const guideHealthLabel = missingChannels === 0
    ? t('dashboard.guideSynced', { defaultValue: 'Synchronisiert' })
    : t('dashboard.missing', { count: missingChannels });
  const recorderLabel = recording?.isRecording
    ? (recording.serviceName || t('dashboard.recordingActive'))
    : t('dashboard.recorderReady', {});

  const liveAction = {
    label: t('dashboard.start.live.action', { defaultValue: 'Open Live TV' }),
    onAction: () => navigate(ROUTE_MAP.epg),
  };
  const recordingsAction = canAccessDvrPlayback
    ? {
      label: t('dashboard.start.recordings.action', { defaultValue: 'Open Recordings' }),
      onAction: () => navigate(buildRecordingsRoute()),
    }
    : null;

  const heroPrimaryAction = receiverUnavailable
    ? liveAction
    : streamCount > 0
      ? (recordingsAction ?? liveAction)
      : liveAction;

  const systemFacts = [
    {
      label: t('dashboard.systemState', { defaultValue: 'Systemstatus' }),
      value: health.status === 'ok'
        ? t('dashboard.systemOperational', { defaultValue: 'Betriebsbereit' })
        : t('dashboard.systemDegraded', { defaultValue: 'Beeinträchtigt' }),
      detail: formatCleanVersion(health.version) || t('dashboard.systemHealthyDetail', { defaultValue: 'Alle Dienste betriebsbereit' }),
      chip: healthChip,
    },
    {
      label: t('dashboard.receiverLabel', { defaultValue: 'Receiver' }),
      value: receiverUnavailable ? t('dashboard.standby', { defaultValue: 'Standby' }) : t('dashboard.connected', { defaultValue: 'Verbunden' }),
      detail: receiverUnavailable
        ? t('dashboard.receiverStandbyDetail', { defaultValue: 'Bereit für Fernsehen' })
        : (currentChannel || t('dashboard.receiverReady', { defaultValue: 'Bereit' })),
    },
    {
      label: t('dashboard.lastSyncLabel', { defaultValue: 'Letzte Synchronisation' }),
      value: formatTimeAgo(health.receiver?.lastCheck, t),
      detail: t('dashboard.readOnlySummary', { defaultValue: 'Automatisch synchronisiert' }),
    },
    {
      label: t('dashboard.guideHealth', { defaultValue: 'Programmführer' }),
      value: guideHealthLabel,
      detail: missingChannels === 0
        ? t('dashboard.allChannelsHaveData', { defaultValue: 'Alle Kanäle versorgt' })
        : t('dashboard.channelsMissingGuideData', { count: missingChannels, defaultValue: 'Kanäle unvollständig' }),
    },
    {
      label: t('dashboard.recorder', { defaultValue: 'Aufnahmen' }),
      value: recorderLabel,
      detail: recording?.isRecording
        ? t('dashboard.recordingActive', { defaultValue: 'Aufnahme aktiv' })
        : t('dashboard.recorderIdle', { defaultValue: 'Keine aktive Aufnahme' }),
    },
  ];

  const directActions = [
    canAccessSettings
      ? {
        id: 'household',
        label: t('settings.household.title', { defaultValue: 'Household profiles' }),
        onAction: () => navigate(buildSettingsRoute({ section: 'household' })),
      }
      : null,
    canManageDvr
      ? {
        id: 'timers',
        label: t('nav.timers', { defaultValue: 'Timers' }),
        onAction: () => navigate(buildEpgRoute('timers')),
      }
      : null,
    canManageDvr
      ? {
        id: 'series',
        label: t('recordings.seriesRulesAction', { defaultValue: 'Series Rules' }),
        onAction: () => navigate(buildRecordingsRoute({ section: 'series' })),
      }
      : null,
    canAccessSettings
      ? {
        id: 'files',
        label: t('nav.files', { defaultValue: 'Files' }),
        onAction: () => navigate(buildSettingsRoute({ section: 'advanced', tool: 'files' })),
      }
      : null,
    canAccessSettings
      ? {
        id: 'logs',
        label: t('nav.logs', { defaultValue: 'Logs' }),
        onAction: () => navigate(buildSettingsRoute({ section: 'advanced', tool: 'logs' })),
      }
      : null,
  ].filter((action): action is { id: string; label: string; onAction: () => void } => action !== null);

  return (
    <div className={`${styles.page} animate-enter`.trim()} data-testid="dashboard-view">
      {/* 1. HERO BANNER */}
      <Card variant="action" className={[styles.heroBanner, styles[`summary${capitalize(summaryTone)}`]].join(' ')}>
        <div className={styles.heroContent}>
          <div className={styles.heroIdentity}>
            <div className={styles.heroEyebrowRow}>
              {receiverUnavailable ? (
                <span className={styles.heroStandbyBadge}>
                  <span className={styles.heroStandbyDot} aria-hidden="true" />
                  {t('dashboard.standby', { defaultValue: 'Standby' })}
                </span>
              ) : (
                <span className={styles.heroChannelBadge}>
                  {t('dashboard.onReceiverNow', { defaultValue: 'Jetzt im Fernsehen' })}
                </span>
              )}
              {!receiverUnavailable && (
                <span className={styles.heroLiveBadge}>
                  <span className={styles.heroLiveDot} aria-hidden="true" />
                  {t('dashboard.metricStreaming', { defaultValue: 'Live' })}
                </span>
              )}
            </div>
            <h1 className={styles.heroTitle}>{summaryTitle}</h1>
            {now?.title && (
              <p className={styles.heroProgramTitle}>{now.title}</p>
            )}
            <p className={styles.heroDescription}>{summaryDescription}</p>
            {next?.title && (
              <p className={styles.heroNextHint}>
                {t('dashboard.heroNextUp', { title: next.title })}
              </p>
            )}
          </div>
          <div className={styles.heroAction}>
            <Button variant="primary" onClick={heroPrimaryAction.onAction} className={styles.heroActionButton}>
              <svg viewBox="0 0 24 24" fill="currentColor" className={styles.heroButtonIcon} aria-hidden="true">
                <polygon points="6 3 20 12 6 21 6 3" />
              </svg>
              {heroPrimaryAction.label}
            </Button>
          </div>
        </div>
      </Card>

      <ContinueWatchingRail />

      {/* 2. ACTIVE STREAMS (OPERATOR SESSIONS) */}
      {streamCount > 0 ? (
        <div className={styles.mainSection}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>
              {t('dashboard.operatorSessions', { defaultValue: 'Aktive Wiedergaben' })}
            </h2>
            <StatusChip
              state="live"
              label={t('dashboard.sessions', { count: streamCount })}
            />
          </div>
          <div className={styles.streamsGrid}>
            <StreamsList compact />
          </div>
        </div>
      ) : (
        <div className={styles.idleSessionsRow}>
          <div className={styles.idleSessionsContent}>
            <span className={styles.idleSessionsTitle}>
              {t('dashboard.operatorSessions', { defaultValue: 'Aktive Wiedergaben' })}
            </span>
            <span className={styles.idleSessionsDivider} aria-hidden="true">·</span>
            <span className={styles.idleSessionsText}>
              {t('dashboard.noActiveStreams', { defaultValue: 'Keine aktiven Streams' })}
            </span>
          </div>
        </div>
      )}

      {/* 3. FOOTER: SHORTCUTS & SYSTEM HEALTH */}
      <div className={styles.footerSection}>
        {directActions.length > 0 && (
          <div className={styles.shortcutsRow}>
            {directActions.map((action) => (
              <Button
                key={action.id}
                variant="secondary"
                size="sm"
                className={styles.shortcutButton}
                onClick={action.onAction}
              >
                {action.label}
              </Button>
            ))}
          </div>
        )}

        <div className={styles.healthGrid}>
          {systemFacts.map((item) => (
            <Card key={item.label} className={styles.healthWidget}>
              <div className={styles.healthHeaderRow}>
                <span className={styles.healthLabel}>{item.label}</span>
                {item.chip && (
                  <StatusChip state={item.chip.state} label={item.chip.label} />
                )}
              </div>
              <div className={styles.healthBody}>
                {item.value && <span className={styles.healthValue}>{item.value}</span>}
                {item.detail && <span className={styles.healthDetail}>{item.detail}</span>}
              </div>
            </Card>
          ))}
        </div>
      </div>
    </div>
  );
}

function mapHealthChip(status: string | undefined, t: (key: string, opts?: Record<string, unknown>) => string): { state: ChipState; label: string } {
  if (status === 'ok') return { state: 'success', label: t('dashboard.systemHealthy', { defaultValue: 'System gesund' }) };
  if (!status) return { state: 'warning', label: t('dashboard.healthUnknown', { defaultValue: 'Status unbekannt' }) };
  return { state: 'warning', label: t('dashboard.systemDegraded', { defaultValue: 'Beeinträchtigt' }) };
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function formatCleanVersion(version: string | undefined): string | undefined {
  if (!version) return undefined;
  let v = version.replace(/^v+/i, '');
  const dashIndex = v.indexOf('-');
  if (dashIndex !== -1) {
    v = v.substring(0, dashIndex);
  }
  return v ? `Version ${v}` : undefined;
}

function formatTimeAgo(dateString: string | undefined, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (!dateString) return t('dashboard.timeNever', { defaultValue: 'Nie' });
  const date = new Date(dateString);
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return t('dashboard.timeNever', { defaultValue: 'Nie' });

  const now = new Date();
  const diffSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (diffSeconds < 60) return t('dashboard.timeJustNow', { defaultValue: 'Gerade eben' });
  const minutes = Math.floor(diffSeconds / 60);
  if (diffSeconds < 3600) {
    return minutes === 1
      ? t('dashboard.timeMinuteAgo', { defaultValue: 'vor 1 Minute' })
      : t('dashboard.timeMinutesAgo', { count: minutes, defaultValue: `vor ${minutes} Minuten` });
  }
  const hours = Math.floor(diffSeconds / 3600);
  if (diffSeconds < 86400) {
    return hours === 1
      ? t('dashboard.timeHourAgo', { defaultValue: 'vor 1 Stunde' })
      : t('dashboard.timeHoursAgo', { count: hours, defaultValue: `vor ${hours} Stunden` });
  }
  return date.toLocaleDateString();
}
