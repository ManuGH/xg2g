// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { getSystemHealth, getLogs, getStreams, getDvrStatus, type SystemHealth, type LogEntry, type StreamSession } from '../client-ts';

interface DvrStatus {
  isRecording?: boolean;
  serviceName?: string;
}

export default function Dashboard() {
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchHealth = async (): Promise<void> => {
    try {
      const result = await getSystemHealth();

      if (result.error) {
        if (result.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          setError('Failed to fetch health');
        }
      } else if (result.data) {
        setHealth(result.data);
      }
    } catch (err) {
      const error = err as Error;
      setError(error.message || 'Failed to fetch health');
    }
  };

  useEffect(() => {
    fetchHealth();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (error) return <div className="error">Error: {error}</div>;
  if (!health) return <div>Loading...</div>;

  return (
    <div className="dashboard">
      <div className="dashboard-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2>System Status</h2>
        <div style={{ display: 'flex', gap: '10px' }}>
          <RecordingStatusIndicator />
          <button onClick={fetchHealth}>Refresh</button>
        </div>
      </div>
      <div className="card-grid">

        {/* Startup Safety Net Indicator */}
        {((health.epg?.missing_channels || 0) > 0 && (health.uptime_seconds || 0) < 300) && (
          <div className="card full-width warning-banner" style={{ background: '#fff3cd', color: '#856404', borderColor: '#ffeeba' }}>
            <h3>⚠️ System Initializing</h3>
            <p>The system is currently syncing with your receiver in the background. Some data may be missing temporarily.</p>
            <small>This allows the UI to remain reachable while data loads. Please check "Recent Logs" below for progress.</small>
          </div>
        )}

        {/* V3.0.0 Welcome Banner */}
        {health.version === 'v3.0.0' && (
          <div className="card full-width info-banner" style={{ background: '#d1e7dd', color: '#0f5132', borderColor: '#badbcc' }}>
            <h3>🎉 New Version 3.0.0</h3>
            <p>Welcome to the new major release! The application has been updated to version 3.0.0.</p>
          </div>
        )}

        <div className="card">
          <h3>System Status</h3>
          <div className={`status-indicator ${health.status}`}>
            {health.status === 'ok' ? 'HEALTHY' : 'DEGRADED'}
          </div>
          <p>Uptime: {formatUptime(health.uptime_seconds || 0)}</p>
          <p>Version: {health.version}</p>
        </div>

        <StreamsCard />

        <div className="card">
          <h3>Enigma2 Link</h3>
          <div className={`status-indicator ${health.receiver?.status}`}>
            {health.receiver?.status === 'ok' ? 'CONNECTED' : 'ERROR'}
          </div>
          <p>Last Sync: {formatTimeAgo(health.receiver?.last_check)}</p>
        </div>

        <div className="card">
          <h3>EPG Data</h3>
          <div className={`status-indicator ${health.epg?.status}`}>
            {health.epg?.status === 'ok' ? 'SYNCED' : health.epg?.status === 'missing' ? 'PARTIAL' : 'ERROR'}
          </div>
          <p>{health.epg?.missing_channels || 0} channels missing data</p>
        </div>

      </div>

      <div className="recent-logs-section" style={{ marginTop: '30px' }}>
        <h3>Recent Logs</h3>
        <LogList />
      </div>
    </div>
  );
}

function RecordingStatusIndicator() {
  const [status, setStatus] = useState<DvrStatus | null>(null);

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;
    let mounted = true;

    // CONTRACT-FE-002: Standard polling interval
    const POLL_INTERVAL_MS = 30000; // 30s per contract

    const fetch = async (): Promise<void> => {
      try {
        const result = await getDvrStatus();

        if (!mounted) return;

        if (result.data) {
          setStatus(result.data as DvrStatus);
          // Success: use standard interval
          timeoutId = setTimeout(fetch, POLL_INTERVAL_MS);
        }
      } catch {
        if (!mounted) return;
        setStatus(null);
        // Error: use standard interval (no custom backoff)
        timeoutId = setTimeout(fetch, POLL_INTERVAL_MS);
      }
    };

    fetch();

    return () => {
      mounted = false;
      clearTimeout(timeoutId);
    };
  }, []);

  if (!status) {
    return <div className="recording-badge unknown">REC: UNKNOWN</div>;
  }

  return (
    <div className={`recording-badge ${status.isRecording ? 'active' : 'idle'}`}>
      {status.isRecording ? `REC: ACTIVE ${status.serviceName ? `(${status.serviceName})` : ''}` : 'REC: IDLE'}
    </div>
  );
}

function StreamsCard() {
  const [streams, setStreams] = useState<StreamSession[]>([]);
  const [error, setError] = useState<boolean>(false);

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;
    let mounted = true;

    // CONTRACT-FE-002: Standard polling for active streams
    const POLL_INTERVAL_MS = 5000; // 5s for streams (less critical than diagnostics)

    const fetch = async (): Promise<void> => {
      try {
        const result = await getStreams();

        if (!mounted) return;

        if (result.data) {
          setStreams(result.data || []);
          setError(false);
          // Success: use standard interval
          timeoutId = setTimeout(fetch, POLL_INTERVAL_MS);
        }
      } catch {
        if (!mounted) return;
        setError(true);
        // Error: use standard interval (no custom backoff)
        timeoutId = setTimeout(fetch, POLL_INTERVAL_MS);
      }
    };

    fetch();

    return () => {
      mounted = false;
      clearTimeout(timeoutId);
    };
  }, []);

  const count = streams.length;

  const maskIP = (ip: string | undefined): string => {
    if (!ip) return '';
    // Simple IPv4 masking: 1.2.3.4 -> 1.2.3.xxx
    return ip.replace(/\.\d+$/, '.xxx');
  };

  return (
    <div className="card">
      <h3>Active Streams</h3>
      <div className={`status-indicator ${count > 0 ? 'active' : 'idle'}`}>
        {error ? 'UNAVAILABLE' : `${count} Active`}
      </div>
      {streams.length > 0 && (
        <ul className="stream-list">
          {streams.map(s => (
            <li key={s.id}>
              <div style={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
                <span className="channel">{s.channel_name || 'Unknown'}</span>
                <span className="ip-hint" style={{ fontSize: '0.8em', color: '#888' }}>{maskIP(s.client_ip)}</span>
              </div>
              <div className="duration" style={{ fontSize: '0.85em' }}>{s.started_at ? formatDuration(new Date(s.started_at)) : ''}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function formatDuration(startDate: Date): string {
  const diff = Math.floor((new Date().getTime() - startDate.getTime()) / 1000);
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  const s = diff % 60;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${s}s`;
}

function LogList() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchLogs = async (): Promise<void> => {
      try {
        const result = await getLogs();

        if (result.error) {
          setError('Failed to load logs');
        } else if (result.data) {
          setLogs((result.data || []).slice(0, 5));
        }
      } catch (err) {
        console.error('Failed to fetch logs', err);
        setError('Failed to load logs');
      } finally {
        setLoading(false);
      }
    };

    fetchLogs();
  }, []);

  if (error) return <div className="error-message">{error}</div>;
  if (loading) return <div>Loading logs...</div>;
  if (!logs || logs.length === 0) return <div className="no-data">No recent logs</div>;

  return (
    <table className="log-table" style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ textAlign: 'left', borderBottom: '1px solid #ccc' }}>
          <th>Time</th>
          <th>Level</th>
          <th>Message</th>
        </tr>
      </thead>
      <tbody>
        {logs.map((log, i) => (
          <tr key={i} style={{ borderBottom: '1px solid #eee' }}>
            <td>{new Date(log.time || '').toLocaleTimeString()}</td>
            <td className={`log-level ${(log.level || '').toLowerCase()}`}>{log.level}</td>
            <td>{log.message}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

function formatTimeAgo(dateString: string | undefined): string {
  if (!dateString) return 'Never';
  const date = new Date(dateString);
  // Check for invalid date or crazy old dates (Year 1)
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return 'Never';

  const now = new Date();
  const diffSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (diffSeconds < 60) return 'Just now';
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m ago`;
  if (diffSeconds < 86400) return `${Math.floor(diffSeconds / 3600)}h ago`;
  return date.toLocaleDateString(); // > 1 day
}
