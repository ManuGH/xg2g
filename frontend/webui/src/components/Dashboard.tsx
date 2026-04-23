import type { CSSProperties } from 'react';
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
import styles from './Dashboard.module.css';

type SummaryTone = 'streaming' | 'control' | 'standby';
type GuidedCard = {
  id: 'live' | 'recordings' | 'settings';
  stepLabel: string;
  title: string;
  description: string;
  detail: string;
  actionLabel: string;
  chip: { state: ChipState; label: string };
  disabled?: boolean;
  onAction: () => void;
};

export default function Dashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const {
    selectedProfile,
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
        : t('dashboard.heroDefaultSummary');

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
  const summarySpotlightFacts = [
    {
      label: t('dashboard.heroAside.profileLabel', { defaultValue: 'Active profile' }),
      value: selectedProfile.name,
    },
    {
      label: t('dashboard.guideHealth'),
      value: guideHealthLabel,
    },
    {
      label: t('dashboard.recorder'),
      value: recorderLabel,
    },
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
  const guidedCards: GuidedCard[] = [
    {
      id: 'live',
      stepLabel: t('dashboard.start.live.step', { defaultValue: 'Step 1' }),
      title: t('dashboard.start.live.title', { defaultValue: 'Watch live TV' }),
      description: t('dashboard.start.live.description', {
        defaultValue: 'Open the guide, browse channels and start playback from the simplest entry point.',
      }),
      detail: receiverUnavailable
        ? t('dashboard.start.live.detailStandby', {
          defaultValue: 'The receiver is in standby. Wake it first so guide and channel data can refresh.',
        })
        : t('dashboard.start.live.detailReady', {
          defaultValue: 'Guide, channel list and timer planning now live in one place.',
        }),
      actionLabel: t('dashboard.start.live.action', { defaultValue: 'Open Live TV' }),
      chip: receiverUnavailable
        ? { state: 'warning', label: t('dashboard.start.status.standby', { defaultValue: 'Receiver in standby' }) }
        : { state: 'success', label: t('dashboard.start.status.ready', { defaultValue: 'Ready now' }) },
      onAction: () => navigate(ROUTE_MAP.epg),
    },
    {
      id: 'recordings',
      stepLabel: t('dashboard.start.recordings.step', { defaultValue: 'Step 2' }),
      title: t('dashboard.start.recordings.title', { defaultValue: 'Open recordings' }),
      description: t('dashboard.start.recordings.description', {
        defaultValue: 'Resume unfinished sessions, browse folders and keep DVR playback separate from live TV.',
      }),
      detail: canAccessDvrPlayback
        ? canManageDvr
          ? t('dashboard.start.recordings.detailManage', {
            defaultValue: 'Series rules stay inside this area when you need them.',
          })
          : t('dashboard.start.recordings.detailWatch', {
            defaultValue: 'This profile can watch recordings but not change DVR rules.',
          })
        : t('dashboard.start.recordings.detailBlocked', {
          defaultValue: 'This profile cannot open recordings right now.',
        }),
      actionLabel: t('dashboard.start.recordings.action', { defaultValue: 'Open Recordings' }),
      chip: canAccessDvrPlayback
        ? { state: 'success', label: t('dashboard.start.status.available', { defaultValue: 'Available in this profile' }) }
        : { state: 'warning', label: t('dashboard.start.status.blocked', { defaultValue: 'Blocked in this profile' }) },
      disabled: !canAccessDvrPlayback,
      onAction: () => navigate(buildRecordingsRoute()),
    },
    {
      id: 'settings',
      stepLabel: t('dashboard.start.settings.step', { defaultValue: 'Step 3' }),
      title: t('dashboard.start.settings.title', { defaultValue: 'Setup and household' }),
      description: t('dashboard.start.settings.description', {
        defaultValue: 'Receiver setup, household profiles and streaming defaults all stay under Settings.',
      }),
      detail: canAccessSettings
        ? t('dashboard.start.settings.detailReady', {
          defaultValue: 'Use this when a family member needs setup help instead of direct expert tools.',
        })
        : t('dashboard.start.settings.detailBlocked', {
          defaultValue: 'This profile cannot open settings right now.',
        }),
      actionLabel: t('dashboard.start.settings.action', { defaultValue: 'Open Setup' }),
      chip: canAccessSettings
        ? { state: 'success', label: t('dashboard.start.status.available', { defaultValue: 'Available in this profile' }) }
        : { state: 'warning', label: t('dashboard.start.status.blocked', { defaultValue: 'Blocked in this profile' }) },
      disabled: !canAccessSettings,
      onAction: () => navigate(buildSettingsRoute({ section: 'setup' })),
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
      <Card variant="action" className={[styles.summaryCard, styles[`summary${capitalize(summaryTone)}`]].join(' ')}>
        <div className={styles.summaryHeroGrid}>
          <div className={styles.summaryHeroMain}>
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
          </div>

          <div className={styles.summarySpotlight}>
            <div className={styles.summarySpotlightHeader}>
              <p className={styles.summarySpotlightEyebrow}>{summarySpotlight.eyebrow}</p>
              <StatusChip state={summarySpotlight.chip.state} label={summarySpotlight.chip.label} />
            </div>
            <h3 className={styles.summarySpotlightTitle}>{summarySpotlight.title}</h3>
            <p className={styles.summarySpotlightCopy}>{summarySpotlight.detail}</p>

            <div className={styles.summarySpotlightMeta}>
              {summarySpotlightFacts.map((item) => (
                <div key={item.label} className={styles.summarySpotlightFact}>
                  <span className={styles.summarySpotlightFactLabel}>{item.label}</span>
                  <span className={styles.summarySpotlightFactValue}>{item.value}</span>
                </div>
              ))}
            </div>

            <div className={styles.summarySpotlightActions}>
              <Button variant="primary" onClick={summarySpotlight.primaryAction.onAction}>
                {summarySpotlight.primaryAction.label}
              </Button>
              {summarySpotlight.secondaryAction ? (
                <Button variant="secondary" onClick={summarySpotlight.secondaryAction.onAction}>
                  {summarySpotlight.secondaryAction.label}
                </Button>
              ) : null}
            </div>
          </div>
        </div>
      </Card>

      <Card className={styles.startSurface}>
        <div className={styles.startHeader}>
          <div>
            <p className={styles.panelEyebrow}>{t('dashboard.start.eyebrow', { defaultValue: 'Start here' })}</p>
            <h3 className={styles.panelTitle}>{t('dashboard.start.title', { defaultValue: 'Choose the task, not the tool' })}</h3>
          </div>
          <p className={styles.startSubtitle}>
            {t('dashboard.start.subtitle', {
              defaultValue: 'Each area now follows the household task first. Direct expert paths stay separate below.',
            })}
          </p>
        </div>

        <div className={styles.startGrid}>
          {guidedCards.map((card) => (
            <div key={card.id} className={styles.startCard} data-card={card.id}>
              <div className={styles.startCardHeader}>
                <div>
                  <p className={styles.startStep}>{card.stepLabel}</p>
                  <h4 className={styles.startCardTitle}>{card.title}</h4>
                </div>
                <StatusChip state={card.chip.state} label={card.chip.label} />
              </div>
              <p className={styles.startCardDescription}>{card.description}</p>
              <p className={styles.startCardDetail}>{card.detail}</p>
              <Button
                variant={card.id === 'live' ? 'primary' : 'secondary'}
                className={styles.startAction}
                onClick={card.onAction}
                disabled={card.disabled}
              >
                {card.actionLabel}
              </Button>
            </div>
          ))}
        </div>
      </Card>

      {directActions.length > 0 && (
        <Card className={styles.shortcutsSurface}>
          <div className={styles.panelHeader}>
            <div>
              <p className={styles.panelEyebrow}>{t('dashboard.shortcuts.eyebrow', { defaultValue: 'Direct paths' })}</p>
              <h3 className={styles.panelTitle}>{t('dashboard.shortcuts.title', { defaultValue: 'Open a specific area directly' })}</h3>
            </div>
          </div>
          <p className={styles.shortcutsDescription}>
            {t('dashboard.shortcuts.subtitle', {
              defaultValue: 'Use these shortcuts when you already know the exact destination.',
            })}
          </p>
          <div className={styles.shortcutsActions}>
            {directActions.map((action) => (
              <Button
                key={action.id}
                variant="ghost"
                size="sm"
                className={styles.shortcutButton}
                onClick={action.onAction}
              >
                {action.label}
              </Button>
            ))}
          </div>
        </Card>
      )}

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
