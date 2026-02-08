// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Phase 2D: Dashboard refactored to primitives (Card + StatusChip)
// CTO Contract: No custom card/badge styling, layout-only CSS

import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus,
  useLogs
} from '../hooks/useServerQueries';
import { useTranslation } from 'react-i18next';
import type { SystemHealth } from '../client-ts';
import StreamsList from './StreamsList';
import { Button, Card, CardHeader, CardTitle, CardBody, StatusChip } from './ui';
import styles from './Dashboard.module.css';

export default function Dashboard() {
  const { data: health, error, isLoading, refetch } = useSystemHealth();

  if (error) return <div className={styles.errorText}>Error: {(error as Error).message}</div>;
  if (isLoading || !health) return <div>Loading...</div>;

  return (
    <div className={`${styles.page} animate-enter`.trim()}>
      <div className={styles.header}>
        <h2>xg2g Dashboard</h2>
        <div className={styles.actions}>
          <RecordingStatusIndicator />
          <Button variant="secondary" onClick={() => refetch()}>Refresh</Button>
        </div>
      </div>

      {/* Startup Warning Banner */}
      {((health.epg?.missingChannels || 0) > 0 && (health.uptimeSeconds || 0) < 300) && (
        <Card className={styles.warning}>
          <CardHeader>
            <CardTitle>System Initializing</CardTitle>
            <StatusChip state="warning" label="SYNC" />
          </CardHeader>
          <CardBody>
            <p>xg2g is syncing with the receiver in the background. Some data may be temporarily missing.</p>
          </CardBody>
        </Card>
      )}

      {/* Primary Row: Receiver Status (HDMI Output) */}
      <div className={styles.primary}>
        <LiveTVCard />
      </div>

      {/* Second Row: Streaming Status */}
      <div className={styles.statusGrid}>
        <BoxStreamingCard />
        <ProgramStatusCard health={health} />
      </div>

      {/* Active Streams Detail */}
      <StreamsDetailSection />

      {/* Bottom Row: EPG, Receiver, Info */}
      <div className={styles.infoGrid}>
        <Card>
          <CardHeader>
            <CardTitle>Enigma2 Link</CardTitle>
          </CardHeader>
          <CardBody>
            <StatusChip
              state={health.receiver?.status === 'ok' ? 'success' : 'error'}
              label={health.receiver?.status === 'ok' ? 'CONNECTED' : 'ERROR'}
            />
            <p className={styles.infoText}>Last Sync: {formatTimeAgo(health.receiver?.lastCheck)}</p>
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>EPG Data</CardTitle>
          </CardHeader>
          <CardBody>
            <StatusChip
              state={
                health.epg?.status === 'ok' ? 'success' :
                  health.epg?.status === 'missing' ? 'warning' : 'error'
              }
              label={
                health.epg?.status === 'ok' ? 'SYNCED' :
                  health.epg?.status === 'missing' ? 'PARTIAL' : 'ERROR'
              }
            />
            <p className={`${styles.infoText} tabular`.trim()}>{health.epg?.missingChannels || 0} channels missing data</p>
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Program Info</CardTitle>
          </CardHeader>
          <CardBody>
            <div className={styles.infoList}>
              <div className={styles.infoItem}>
                <span className={styles.infoLabel}>Version</span>
                <span className={styles.infoValue}>{health.version}</span>
              </div>
              <div className={styles.infoItem}>
                <span className={styles.infoLabel}>Uptime</span>
                <span className={`${styles.infoValue} tabular`.trim()}>{formatUptime(health.uptimeSeconds || 0)}</span>
              </div>
            </div>
          </CardBody>
        </Card>
      </div>

      {/* Recent Logs */}
      <div className={styles.logs}>
        <h3>Recent Logs</h3>
        <LogList />
      </div>
    </div>
  );
}

// Live TV Card (HDMI Output) - Refactored to primitives
function LiveTVCard() {
  const { t } = useTranslation();
  const { data: info, isLoading } = useReceiverCurrent();

  if (isLoading && !info) return <Card><CardBody>{t('common.loading')}</CardBody></Card>;

  const hasNow = !!info?.now?.title;
  const now = info?.now;
  const channel = info?.channel;
  const next = info?.next;

  const isUnavailable = info?.status === 'unavailable';

  return (
    <Card variant="live" className={styles.liveTvCard}>
      <CardHeader>
        <div className={styles.liveTvHeader}>
          <CardTitle>Live on Receiver</CardTitle>
          <div className={styles.badgeGroup}>
            {!isUnavailable && <StatusChip state="idle" label="HDMI" showIcon={false} />}
            {!isUnavailable && <StatusChip state="live" label="LIVE" />}
            {isUnavailable && <StatusChip state="idle" label="STANDBY" />}
          </div>
        </div>
        <div className={styles.receiverChannel}>
          {isUnavailable ? t('common.receiverStandby') : (channel?.name || 'Unknown Channel')}
        </div>
      </CardHeader>

      <CardBody>
        {hasNow && now ? (
          <>
            <div className={styles.programCurrent}>
              <div className={styles.programTitle}>{now.title}</div>
              <div className={styles.programDesc}>{now.description}</div>
            </div>
            {now.beginTimestamp && now.durationSec && (
              <div className={styles.programProgress}>
                <div className={`${styles.programTimes} tabular`.trim()}>
                  <span>{new Date(now.beginTimestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                  <span>{new Date((now.beginTimestamp + now.durationSec) * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                </div>
                <div className={styles.progressBar}>
                  <div
                    className={styles.progressFill}
                    style={{ '--xg2g-prog': `${Math.min(100, Math.max(0, ((Date.now() / 1000) - now.beginTimestamp) / now.durationSec * 100))}%` } as React.CSSProperties & { [key: string]: string }}
                  />
                </div>
              </div>
            )}
            {next?.title && (
              <div className={styles.programNext}>
                <span className={styles.nextLabel}>UP NEXT:</span> {next.title}
              </div>
            )}
          </>
        ) : (
          <div className={styles.noData}>
            {next?.title ? (
              <div>
                <div>{t('common.noCurrentProgram')}</div>
                <div className={styles.programNext}>
                  <span className={styles.nextLabel}>UP NEXT:</span> {next.title}
                </div>
              </div>
            ) : (
              <>{info?.status === 'unavailable' ? t('common.receiverUnavailable') : 'No EPG information available'}</>
            )}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// Box Streaming Card - Refactored to primitives
function BoxStreamingCard() {
  const { data: streams = [] } = useStreams();
  const { t } = useTranslation();
  const streamCount = streams.length;
  const isStreaming = streamCount > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('nav.cards.boxStreaming.title')}</CardTitle>
      </CardHeader>
      <CardBody>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>{t('nav.cards.boxStreaming.inputLabel')}</span>
          <StatusChip
            state={isStreaming ? 'live' : 'idle'}
            label={isStreaming ? `STREAMING (${streamCount})` : 'IDLE'}
          />
        </div>
      </CardBody>
    </Card>
  );
}

// Program Status Card - Refactored to primitives
function ProgramStatusCard({ health }: { health: SystemHealth }) {
  const { data: streams = [] } = useStreams();
  const { t } = useTranslation();
  const streamCount = streams.length;
  const isActive = streamCount > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('nav.cards.programStatus.title')}</CardTitle>
      </CardHeader>
      <CardBody>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>{t('nav.cards.programStatus.outputLabel')}</span>
          <StatusChip
            state={isActive ? 'live' : 'idle'}
            label={isActive ? `${streamCount} ACTIVE` : 'IDLE'}
          />
        </div>
        <div className={styles.infoList}>
          <div className={styles.infoItem}>
            <span className={styles.infoLabel}>Health</span>
            <StatusChip
              state={health.status === 'ok' ? 'success' : 'warning'}
              label={health.status === 'ok' ? 'HEALTHY' : 'DEGRADED'}
              showIcon={false}
            />
          </div>
        </div>
      </CardBody>
    </Card>
  );
}

// Streams Detail Section - Refactored to primitive grid
function StreamsDetailSection() {
  return (
    <div className={styles.streams}>
      <h3>Active Streams</h3>
      <StreamsList />
    </div>
  );
}

// Recording Status Indicator - Refactored to StatusChip primitive
function RecordingStatusIndicator() {
  const { data: status } = useDvrStatus();

  if (!status) {
    return <StatusChip state="warning" label="REC: UNKNOWN" />;
  }

  return (
    <StatusChip
      state={status.isRecording ? 'recording' : 'idle'}
      label={status.isRecording ? `REC: ACTIVE ${status.serviceName ? `(${status.serviceName})` : ''}` : 'REC: IDLE'}
    />
  );
}

// Log List Component - Unchanged (table, not card-based)
function LogList() {
  const { data: logs = [], isLoading, error } = useLogs(5);

  if (error) return <div className={styles.errorText}>{(error as Error).message}</div>;
  if (isLoading) return <div>Loading logs...</div>;
  if (!logs || logs.length === 0) return <div className={styles.noData}>No recent logs</div>;

  return (
    <table className={styles.logTable}>
      <thead>
        <tr>
          <th>Time</th>
          <th>Level</th>
          <th>Message</th>
        </tr>
      </thead>
      <tbody>
        {logs.map((log, i) => (
          <tr key={i}>
            <td className="tabular">{new Date(log.time || '').toLocaleTimeString()}</td>
            <td className={styles.logLevel} data-level={(log.level || '').toLowerCase() || undefined}>
              {log.level}
            </td>
            <td>{log.message}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// Helper Functions
function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

function formatTimeAgo(dateString: string | undefined): string {
  if (!dateString) return 'Never';
  const date = new Date(dateString);
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return 'Never';

  const now = new Date();
  const diffSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (diffSeconds < 60) return 'Just now';
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m ago`;
  if (diffSeconds < 86400) return `${Math.floor(diffSeconds / 3600)}h ago`;
  return date.toLocaleDateString();
}
