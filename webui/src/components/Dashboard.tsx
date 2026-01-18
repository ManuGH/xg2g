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
import type { SystemHealth } from '../client-ts';
import StreamsList from './StreamsList';
import { Card, CardHeader, CardTitle, CardBody } from './ui/Card';
import { StatusChip } from './ui/StatusChip';
import './Dashboard.css';

export default function Dashboard() {
  const { data: health, error, isLoading, refetch } = useSystemHealth();

  if (error) return <div className="error">Error: {(error as Error).message}</div>;
  if (isLoading || !health) return <div>Loading...</div>;

  return (
    <div className="dashboard animate-enter">
      <div className="dashboard-header">
        <h2>xg2g Dashboard</h2>
        <div className="dashboard-actions">
          <RecordingStatusIndicator />
          <button onClick={() => refetch()}>Refresh</button>
        </div>
      </div>

      {/* Startup Warning Banner */}
      {((health.epg?.missing_channels || 0) > 0 && (health.uptime_seconds || 0) < 300) && (
        <Card className="dashboard__warning">
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
      <div className="dashboard__primary">
        <LiveTVCard />
      </div>

      {/* Second Row: Streaming Status */}
      <div className="dashboard__status-grid">
        <BoxStreamingCard />
        <ProgramStatusCard health={health} />
      </div>

      {/* Active Streams Detail */}
      <StreamsDetailSection />

      {/* Bottom Row: EPG, Receiver, Info */}
      <div className="dashboard__info-grid">
        <Card>
          <CardHeader>
            <CardTitle>Enigma2 Link</CardTitle>
          </CardHeader>
          <CardBody>
            <StatusChip
              state={health.receiver?.status === 'ok' ? 'success' : 'error'}
              label={health.receiver?.status === 'ok' ? 'CONNECTED' : 'ERROR'}
            />
            <p className="info-text">Last Sync: {formatTimeAgo(health.receiver?.last_check)}</p>
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
            <p className="info-text tabular">{health.epg?.missing_channels || 0} channels missing data</p>
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Program Info</CardTitle>
          </CardHeader>
          <CardBody>
            <div className="info-list">
              <div className="info-item">
                <span className="info-label">Version</span>
                <span className="info-value">{health.version}</span>
              </div>
              <div className="info-item">
                <span className="info-label">Uptime</span>
                <span className="info-value tabular">{formatUptime(health.uptime_seconds || 0)}</span>
              </div>
            </div>
          </CardBody>
        </Card>
      </div>

      {/* Recent Logs */}
      <div className="dashboard__logs">
        <h3>Recent Logs</h3>
        <LogList />
      </div>
    </div>
  );
}

// Live TV Card (HDMI Output) - Refactored to primitives
function LiveTVCard() {
  const { data: info, isLoading } = useReceiverCurrent();

  if (isLoading && !info) return <Card><CardBody>Loading Live TV info...</CardBody></Card>;

  const hasNow = !!info?.now?.title;
  const now = info?.now;
  const channel = info?.channel;
  const next = info?.next;

  return (
    <Card variant="live" className="live-tv-card">
      <CardHeader>
        <div className="live-tv-header">
          <CardTitle>Live on Receiver</CardTitle>
          <div className="badge-group">
            <StatusChip state="warning" label="HDMI" />
            <StatusChip state="live" label="LIVE" />
          </div>
        </div>
        <div className="receiver-channel">{channel?.name || 'Unknown Channel'}</div>
      </CardHeader>

      <CardBody>
        {hasNow && now ? (
          <>
            <div className="program-current">
              <div className="program-title">{now.title}</div>
              <div className="program-desc">{now.description}</div>
            </div>
            {now.begin_timestamp && now.duration_sec && (
              <div className="program-progress">
                <div className="program-times tabular">
                  <span>{new Date(now.begin_timestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                  <span>{new Date((now.begin_timestamp + now.duration_sec) * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                </div>
                <div className="progress-bar">
                  <div
                    className="progress-fill"
                    style={{ '--xg2g-prog': `${Math.min(100, Math.max(0, ((Date.now() / 1000) - now.begin_timestamp) / now.duration_sec * 100))}%` } as React.CSSProperties & { [key: string]: string }}
                  />
                </div>
              </div>
            )}
            {next?.title && (
              <div className="program-next">
                <span className="next-label">UP NEXT:</span> {next.title}
              </div>
            )}
          </>
        ) : (
          <div className="no-data">
            {info?.status === 'unavailable' ? 'Receiver currently unavailable' : 'No EPG information available'}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// Box Streaming Card - Refactored to primitives
function BoxStreamingCard() {
  const { data: streams = [] } = useStreams();
  const streamCount = streams.length;
  const isStreaming = streamCount > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Box Streaming</CardTitle>
      </CardHeader>
      <CardBody>
        <div className="status-row">
          <span className="status-label">Enigma2 → xg2g</span>
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
  const streamCount = streams.length;
  const isActive = streamCount > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Program Status</CardTitle>
      </CardHeader>
      <CardBody>
        <div className="status-row">
          <span className="status-label">xg2g → Clients</span>
          <StatusChip
            state={isActive ? 'live' : 'idle'}
            label={isActive ? `${streamCount} ACTIVE` : 'IDLE'}
          />
        </div>
        <div className="info-list">
          <div className="info-item">
            <span className="info-label">Health</span>
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
    <div className="dashboard__streams">
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

  if (error) return <div className="error-text">{(error as Error).message}</div>;
  if (isLoading) return <div>Loading logs...</div>;
  if (!logs || logs.length === 0) return <div className="no-data">No recent logs</div>;

  return (
    <table className="log-table">
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
            <td className={`log-level log-level--${(log.level || '').toLowerCase()}`}>
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
