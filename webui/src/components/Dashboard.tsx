// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Phase 1: TanStack Query Migration - Server-State Layer (2026 SOTA)
import {
  useSystemHealth,
  useReceiverCurrent,
  useStreams,
  useDvrStatus,
  useLogs
} from '../hooks/useServerQueries';
import type { SystemHealth, StreamSession } from '../client-ts';
import './Dashboard.css';

export default function Dashboard() {
  const { data: health, error, isLoading, refetch } = useSystemHealth();

  if (error) return <div className="error">Error: {(error as Error).message}</div>;
  if (isLoading || !health) return <div>Loading...</div>;

  return (
    <div className="dashboard">
      <div className="dashboard-header">
        <h2>🎬 xg2g Dashboard</h2>
        <div className="dashboard-actions">
          <RecordingStatusIndicator />
          <button onClick={() => refetch()}>Refresh</button>
        </div>
      </div>

      {/* Startup Warning Banner */}
      {((health.epg?.missing_channels || 0) > 0 && (health.uptime_seconds || 0) < 300) && (
        <div className="status-card" style={{ background: 'rgba(251, 191, 36, 0.1)', border: '1px solid rgba(251, 191, 36, 0.3)', marginBottom: '20px' }}>
          <h3>⚠️ System Initializing</h3>
          <p style={{ margin: 0, fontSize: '13px', color: 'rgba(255, 255, 255, 0.7)' }}>
            xg2g is syncing with the receiver in the background. Some data may be temporarily missing.
          </p>
        </div>
      )}

      {/* Primary Row: Receiver Status (HDMI Output) */}
      <div className="main-status-row" style={{ marginBottom: '20px' }}>
        <LiveTVCard />
      </div>

      {/* Second Row: Streaming Status */}
      <div className="status-grid">
        <BoxStreamingCard />
        <ProgramStatusCard health={health} />
      </div>

      {/* Active Streams Detail */}
      <StreamsDetailSection />

      {/* Bottom Row: EPG, Receiver, Info */}
      <div className="bottom-row">
        <div className="info-card">
          <h3>📡 Enigma2 Link</h3>
          <div className={`status-indicator ${health.receiver?.status}`}>
            {health.receiver?.status === 'ok' ? 'CONNECTED' : 'ERROR'}
          </div>
          <p>Last Sync: {formatTimeAgo(health.receiver?.last_check)}</p>
        </div>

        <div className="info-card">
          <h3>📺 EPG Data</h3>
          <div className={`status-indicator ${health.epg?.status}`}>
            {health.epg?.status === 'ok' ? 'SYNCED' : health.epg?.status === 'missing' ? 'PARTIAL' : 'ERROR'}
          </div>
          <p>{health.epg?.missing_channels || 0} channels missing data</p>
        </div>

        <div className="info-card">
          <h3>ℹ️ Program Info</h3>
          <div className="program-info">
            <div className="info-item">
              <span className="info-label">Version</span>
              <span className="info-value">{health.version}</span>
            </div>
            <div className="info-item">
              <span className="info-label">Uptime</span>
              <span className="info-value">{formatUptime(health.uptime_seconds || 0)}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Recent Logs */}
      <div className="recent-logs-section">
        <h3>📝 Recent Logs</h3>
        <LogList />
      </div>
    </div>
  );
}

// Live TV Card (HDMI Output) - TanStack Query refactored
function LiveTVCard() {
  const { data: info, isLoading } = useReceiverCurrent();

  if (isLoading && !info) return <div className="status-card loading">Loading Live TV info...</div>;

  const hasNow = !!info?.now?.title;
  const now = info?.now;
  const channel = info?.channel;
  const next = info?.next;

  return (
    <div className="status-card live-tv-card">
      <div className="live-tv-header">
        <div className="live-tv-title">
          <h3>📺 Live on Receiver</h3>
          <div className="badge-row">
            <span className="source-badge hdmi">HDMI</span>
            <span className="live-badge">LIVE</span>
          </div>
        </div>
        <div className="receiver-channel">
          {channel?.name || 'Unknown Channel'}
        </div>
      </div>

      <div className="live-tv-content">
        {hasNow && now ? (
          <>
            <div className="current-program">
              <div className="program-title">{now.title}</div>
              <div className="program-desc">{now.description}</div>
            </div>
            {now.begin_timestamp && now.duration_sec && (
              <div className="program-progress-container">
                <div className="program-times">
                  <span>{new Date(now.begin_timestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                  <span>{new Date((now.begin_timestamp + now.duration_sec) * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                </div>
                <div className="progress-bar">
                  <div
                    className="progress-fill"
                    style={{
                      width: `${Math.min(100, Math.max(0,
                        ((Date.now() / 1000) - now.begin_timestamp) / now.duration_sec * 100
                      ))}%`
                    }}
                  />
                </div>
              </div>
            )}
            {next?.title && (
              <div className="next-program-hint">
                <span className="next-label">UP NEXT:</span> {next.title}
              </div>
            )}
          </>
        ) : (
          <div className="no-epg">
            {info?.status === 'unavailable' ? 'Receiver currently unavailable' : 'No EPG information available'}
          </div>
        )}
      </div>
    </div>
  );
}

// Box Streaming Status Card - TanStack Query refactored
function BoxStreamingCard() {
  const { data: streams = [] } = useStreams();
  const streamCount = streams.length;

  return (
    <div className="status-card">
      <h3>📡 Box Streaming</h3>
      <div className="streaming-status">
        <div className="streaming-item">
          <span className="streaming-label">Enigma2 → xg2g</span>
          <span className={`streaming-badge ${streamCount > 0 ? 'active' : 'idle'}`}>
            {streamCount > 0 ? `🔴 STREAMING (${streamCount})` : '🟢 IDLE'}
          </span>
        </div>
      </div>
    </div>
  );
}

// Program Status Card - TanStack Query refactored
function ProgramStatusCard({ health }: { health: SystemHealth }) {
  const { data: streams = [] } = useStreams();
  const streamCount = streams.length;

  return (
    <div className="status-card">
      <h3>🚀 Program Status</h3>
      <div className="streaming-status">
        <div className="streaming-item">
          <span className="streaming-label">xg2g → Clients</span>
          <span className={`streaming-badge ${streamCount > 0 ? 'active' : 'idle'}`}>
            {streamCount > 0 ? `🔴 ${streamCount} ACTIVE` : '🟢 IDLE'}
          </span>
        </div>
      </div>
      <div className="program-info" style={{ marginTop: '16px' }}>
        <div className="info-item">
          <span className="info-label">Health</span>
          <span className="info-value">{health.status === 'ok' ? 'HEALTHY' : 'DEGRADED'}</span>
        </div>
      </div>
    </div>
  );
}

// Streams Detail Section - TanStack Query refactored
function StreamsDetailSection() {
  const { data: streams = [], error } = useStreams();
  const count = streams.length;

  if (count === 0) return null;

  const maskIP = (ip: string | undefined): string => {
    if (!ip) return '';
    return ip.replace(/\.\d+$/, '.xxx');
  };

  return (
    <div className="streams-section">
      <h3>🎥 Active Streams ({count})</h3>
      {error && <p style={{ color: '#ef4444' }}>Failed to load stream details</p>}
      <div className="stream-grid">
        {streams.map((s: StreamSession) => (
          <div key={s.id} className="stream-card-enriched">
            <div className="stream-card-header">
              <div className="stream-card-channel-group">
                <span className="source-badge stream">STREAM</span>
                <div className="stream-card-channel">{s.channel_name || 'Unknown Channel'}</div>
              </div>
              <div className="stream-card-badge">ACTIVE</div>
            </div>

            <div className="stream-card-body">
              {s.program?.title && (
                <div className="stream-card-program">
                  <div className="stream-program-title">{s.program.title}</div>
                  <div className="stream-program-desc">{s.program.description}</div>
                </div>
              )}
              <div className="stream-card-meta">
                <div className="meta-item">
                  <span className="meta-label">Client:</span> {maskIP(s.client_ip)}
                </div>
                <div className="meta-item">
                  <span className="meta-label">Started:</span> {s.started_at ? formatDuration(new Date(s.started_at)) : 'unknown'}
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// Recording Status Indicator - TanStack Query refactored
function RecordingStatusIndicator() {
  const { data: status } = useDvrStatus();

  if (!status) {
    return <div className="recording-badge unknown">REC: UNKNOWN</div>;
  }

  return (
    <div className={`recording-badge ${status.isRecording ? 'active' : 'idle'}`}>
      {status.isRecording ? `REC: ACTIVE ${status.serviceName ? `(${status.serviceName})` : ''}` : 'REC: IDLE'}
    </div>
  );
}

// Log List Component - TanStack Query refactored
function LogList() {
  const { data: logs = [], isLoading, error } = useLogs(5);

  if (error) return <div className="error-message">{(error as Error).message}</div>;
  if (isLoading) return <div>Loading logs...</div>;
  if (!logs || logs.length === 0) return <div className="no-data">No recent logs</div>;

  return (
    <table className="log-table" style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ textAlign: 'left', borderBottom: '1px solid rgba(255, 255, 255, 0.1)' }}>
          <th style={{ padding: '8px', color: 'rgba(255, 255, 255, 0.6)', fontSize: '12px' }}>Time</th>
          <th style={{ padding: '8px', color: 'rgba(255, 255, 255, 0.6)', fontSize: '12px' }}>Level</th>
          <th style={{ padding: '8px', color: 'rgba(255, 255, 255, 0.6)', fontSize: '12px' }}>Message</th>
        </tr>
      </thead>
      <tbody>
        {logs.map((log, i) => (
          <tr key={i} style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.05)' }}>
            <td style={{ padding: '8px', fontSize: '12px', color: 'rgba(255, 255, 255, 0.7)' }}>
              {new Date(log.time || '').toLocaleTimeString()}
            </td>
            <td className={`log-level ${(log.level || '').toLowerCase()}`} style={{ padding: '8px', fontSize: '12px' }}>
              {log.level}
            </td>
            <td style={{ padding: '8px', fontSize: '12px', color: 'rgba(255, 255, 255, 0.8)' }}>
              {log.message}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// Helper Functions
function formatDuration(startDate: Date): string {
  const diff = Math.floor((new Date().getTime() - startDate.getTime()) / 1000);
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  const s = diff % 60;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${s}s`;
}

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
