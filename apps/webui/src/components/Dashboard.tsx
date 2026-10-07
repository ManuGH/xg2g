import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus,
  useTimers,
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
  const { data: timers = [] } = useTimers();

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
    ? t('dashboard.heroStandbyTitle', { defaultValue: 'Was möchtest du ansehen?' })
    : (currentChannel || t('dashboard.receiverReady', { defaultValue: 'Live-TV' }));

  const summaryDescription = streamCount > 0
    ? (now?.description || t('dashboard.heroStreamingSummary', { count: streamCount }))
    : receiverUnavailable
      ? t('dashboard.heroStandbySummary', { defaultValue: 'Live-TV starten oder dort weitermachen, wo du aufgehört hast.' })
      : next?.title
        ? t('dashboard.heroNextUp', { title: next.title })
        : t('dashboard.heroDefaultSummary');

  const scheduledTimers = timers
    .filter((timer) => timer.state === 'scheduled')
    .sort((a, b) => a.begin - b.begin);
  const nextTimer = scheduledTimers[0];

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
  ].filter((action): action is { id: string; label: string; onAction: () => void } => action !== null);

  return (
    <div className={`${styles.page} animate-enter`.trim()} data-testid="dashboard-view">
      {/* 1. CONTINUATION: WEITER SCHAUEN */}
      <ContinueWatchingRail />

      {/* 2. HERO BANNER: LIVE FERNSEHEN / HAUPTAKTION */}
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

      {/* 3. ACTIVE STREAMS (OPERATOR SESSIONS) - ONLY IF ACTIVE */}
      {streamCount > 0 && (
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
      )}

      {/* 4. HEUTE / GEPLANT & ALLTAGSAKTIONEN */}
      {(directActions.length > 0 || nextTimer) && (
        <div className={styles.actionsSection}>
          <div className={styles.shortcutsRow}>
            {nextTimer && (
              <button
                type="button"
                className={styles.upcomingTimerPill}
                onClick={() => navigate(buildEpgRoute('timers'))}
                title={nextTimer.name}
              >
                <span className={styles.upcomingTimerDot} aria-hidden="true" />
                <span className={styles.upcomingTimerText}>
                  {t('dashboard.upcomingTimerSummary', {
                    count: scheduledTimers.length,
                    time: formatTimerTime(nextTimer.begin),
                    name: nextTimer.name,
                    defaultValue: `${scheduledTimers.length} geplante Aufnahmen · Nächste: ${formatTimerTime(nextTimer.begin)} (${nextTimer.name})`,
                  })}
                </span>
              </button>
            )}
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
        </div>
      )}

      {/* 5. DISKRETE STATUSLEISTE AM SEITENENDE (RÜCKVERSICHERUNG) */}
      <footer className={styles.statusFooter} aria-label={t('dashboard.statusOverview', { defaultValue: 'Systemstatus' })}>
        <div className={styles.systemStatusStrip} role="status">
          <span className={styles.statusPill}>
            <span className={health.status === 'ok' ? styles.statusPillDotSuccess : styles.statusPillDotWarning} aria-hidden="true" />
            <span className={styles.statusPillLabel}>
              {health.status === 'ok'
                ? t('dashboard.statusSystemReady', { defaultValue: 'System bereit' })
                : t('dashboard.systemDegraded', { defaultValue: 'Beeinträchtigt' })}
            </span>
          </span>
          <span className={styles.statusDivider} aria-hidden="true">·</span>
          <span className={styles.statusPill}>
            <span className={receiverUnavailable ? styles.statusPillDotWarning : styles.statusPillDotSuccess} aria-hidden="true" />
            <span className={styles.statusPillLabel}>
              {receiverUnavailable
                ? t('dashboard.statusReceiverStandby', { defaultValue: 'Receiver Standby' })
                : t('dashboard.statusReceiverConnected', { defaultValue: 'Receiver verbunden' })}
            </span>
          </span>
          <span className={styles.statusDivider} aria-hidden="true">·</span>
          <span className={styles.statusPill}>
            <span className={missingChannels === 0 ? styles.statusPillDotSuccess : styles.statusPillDotWarning} aria-hidden="true" />
            <span className={styles.statusPillLabel}>
              {missingChannels === 0
                ? t('dashboard.statusEpgReady', { defaultValue: 'EPG aktuell' })
                : t('dashboard.missing', { count: missingChannels, defaultValue: `${missingChannels} unvollständig` })}
            </span>
          </span>
          {recording?.isRecording && (
            <>
              <span className={styles.statusDivider} aria-hidden="true">·</span>
              <span className={styles.statusPill}>
                <span className={styles.statusPillDotRecording} aria-hidden="true" />
                <span className={styles.statusPillLabel}>
                  {recording.serviceName || t('dashboard.recordingActive', { defaultValue: 'Aufnahme aktiv' })}
                </span>
              </span>
            </>
          )}
        </div>
      </footer>
    </div>
  );
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function formatTimerTime(ts: number | undefined): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  const now = new Date();
  const isToday = d.toDateString() === now.toDateString();
  const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  if (isToday) {
    return timeStr;
  }
  const dateStr = d.toLocaleDateString([], { weekday: 'short' });
  return `${dateStr} ${timeStr}`;
}
