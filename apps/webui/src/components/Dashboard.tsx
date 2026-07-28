import { useNavigate } from 'react-router-dom';
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
  const currentChannel = receiver?.channel?.name || (receiverUnavailable ? t('common.receiverStandby') : t('dashboard.receiverReady'));
  const now = receiver?.now;
  const next = receiver?.next;
  const missingChannels = health.epg?.missingChannels || 0;
  const summaryTone: SummaryTone = streamCount > 0 ? 'streaming' : receiverUnavailable ? 'standby' : 'control';


  const summaryTitle = currentChannel;

  const summaryDescription = streamCount > 0
    ? (now?.description || t('dashboard.heroStreamingSummary', { count: streamCount }))
    : receiverUnavailable
      ? t('dashboard.heroStandbySummary')
      : next?.title
        ? t('dashboard.heroNextUp', { title: next.title })
        : t('dashboard.heroDefaultSummary');

  const healthChip = mapHealthChip(health.status, t);
  const guideHealthLabel = missingChannels === 0
    ? t('dashboard.guideSynced')
    : t('dashboard.missing', { count: missingChannels });
  const recorderLabel = recording?.serviceName || (recording?.isRecording ? t('dashboard.recordingActive') : t('dashboard.recorderIdle'));

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
  const settingsAction = canAccessSettings
    ? {
      label: t('dashboard.start.settings.action', { defaultValue: 'Open Setup' }),
      onAction: () => navigate(buildSettingsRoute({ section: 'setup' })),
    }
    : null;
  const summarySpotlight = receiverUnavailable
    ? {
      eyebrow: t('dashboard.heroAside.eyebrow', { defaultValue: 'Next best move' }),
      title: t('dashboard.heroAside.standbyTitle', {
        defaultValue: 'Start from live TV once the receiver wakes',
      }),
      detail: t('dashboard.heroAside.standbyDetail', {
        defaultValue: 'The box is sleeping right now. Use the live TV path first so guide and channel state can rebuild cleanly.',
      }),
      chip: {
        state: 'warning' as const,
        label: t('dashboard.start.status.standby', { defaultValue: 'Receiver in standby' }),
      },
      primaryAction: liveAction,
      secondaryAction: settingsAction,
    }
    : streamCount > 0
      ? {
        eyebrow: t('dashboard.heroAside.eyebrow', { defaultValue: 'Next best move' }),
        title: t('dashboard.heroAside.streamingTitle', {
          defaultValue: 'Playback is already live on the bridge',
        }),
        detail: t('dashboard.heroAside.streamingDetail', {
          defaultValue: 'Use recordings for the next task or jump back into live TV without losing the current operator overview.',
        }),
        chip: {
          state: 'live' as const,
          label: t('dashboard.sessions', { count: streamCount }),
        },
        primaryAction: recordingsAction ?? liveAction,
        secondaryAction: recordingsAction ? liveAction : settingsAction,
      }
      : {
        eyebrow: t('dashboard.heroAside.eyebrow', { defaultValue: 'Next best move' }),
        title: t('dashboard.heroAside.readyTitle', {
          defaultValue: 'Everything is ready for the normal household path',
        }),
        detail: t('dashboard.heroAside.readyDetail', {
          defaultValue: 'Live TV is the fastest entry. Recordings and setup stay one click away if the current profile allows them.',
        }),
        chip: {
          state: 'success' as const,
          label: t('dashboard.start.status.ready', { defaultValue: 'Ready now' }),
        },
        primaryAction: liveAction,
        secondaryAction: recordingsAction ?? settingsAction,
      };

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
      
      {/* 1. SLIM HERO BANNER */}
      <Card variant="action" className={[styles.heroBanner, styles[`summary${capitalize(summaryTone)}`]].join(' ')}>
        <div className={styles.heroContent}>
          <div className={styles.heroIdentity}>
            <div className={styles.heroTitleRow}>
              <h1 className={styles.heroTitle}>{summaryTitle}</h1>
              <StatusChip state={healthChip.state} label={healthChip.label} />
            </div>
            <p className={styles.heroDescription}>{summaryDescription}</p>
          </div>
          <div className={styles.heroAction}>
            <Button variant="primary" onClick={summarySpotlight.primaryAction.onAction}>
              {summarySpotlight.primaryAction.label}
            </Button>
          </div>
        </div>
      </Card>

      <ContinueWatchingRail />

      {/* 2. ACTIVE STREAMS (The Main Event) */}
      <div className={styles.mainSection}>
        <div className={styles.sectionHeader}>
          <h2 className={styles.sectionTitle}>{t('dashboard.operatorSessions', { defaultValue: 'Operator-Sitzungen' })}</h2>
          <StatusChip
            state={streamCount > 0 ? 'live' : 'idle'}
            label={streamCount > 0 ? t('dashboard.sessions', { count: streamCount }) : t('dashboard.noSessions')}
          />
        </div>
        
        {streamCount > 0 ? (
          <div className={styles.streamsGrid}>
             <StreamsList />
          </div>
        ) : (
          <Card className={styles.emptyStreamsCard}>
            <div className={styles.emptyState}>
              <p className={styles.emptyTitle}>{t('dashboard.noActiveStreams')}</p>
              <p className={styles.emptyText}>{t('dashboard.startPlaybackHint')}</p>
            </div>
          </Card>
        )}
      </div>

      {/* 3. FOOTER: SHORTCUTS & HEALTH WIDGETS */}
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
              <span className={styles.healthLabel}>{item.label}</span>
              <span className={styles.healthValue}>{item.value}</span>
              <span className={styles.healthDetail}>{item.detail}</span>
            </Card>
          ))}
        </div>

      </div>

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
