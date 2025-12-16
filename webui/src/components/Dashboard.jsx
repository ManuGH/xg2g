import { useEffect, useState } from 'react';
import { DefaultService } from '../client/services/DefaultService';
import { DvrService } from '../client/services/DvrService';

export default function Dashboard() {
  const [health, setHealth] = useState(null);
  const [error, setError] = useState(null);

  const fetchHealth = () => {
    DefaultService.getSystemHealth()
      .then(setHealth)
      .catch(err => {
        // Trigger auth modal on 401
        if (err.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          setError(err.message || 'Failed to fetch health');
        }
      });
  };

  useEffect(() => {
    fetchHealth();
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

        <div className="card">
          <h3>System Status</h3>
          <div className={`status-indicator ${health.status}`}>
            {health.status === 'ok' ? 'HEALTHY' : 'DEGRADED'}
          </div>
          <p>Uptime: {formatUptime(health.uptime_seconds)}</p>
          <p>Version: {health.version}</p>
        </div>

        <StreamsCard />

        <div className="card">
          <h3>Enigma2 Link</h3>
          <div className={`status-indicator ${health.receiver.status}`}>
            {health.receiver.status === 'ok' ? 'CONNECTED' : 'ERROR'}
          </div>
          <p>Last Sync: {health.receiver.last_check ? new Date(health.receiver.last_check).toLocaleTimeString() : 'Never'}</p>
        </div>

        <div className="card">
          <h3>EPG Data</h3>
          <div className={`status-indicator ${health.epg.status}`}>
            {health.epg.status === 'ok' ? 'SYNCED' : health.epg.status === 'missing' ? 'PARTIAL' : 'ERROR'}
          </div>
          <p>{health.epg.missing_channels || 0} channels missing data</p>
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
  const [status, setStatus] = useState(null);

  useEffect(() => {
    let timeoutId;
    let mounted = true;
    let errors = 0;

    const fetch = () => {
      DvrService.getDvrStatus()
        .then(data => {
          if (!mounted) return;
          setStatus(data);
          errors = 0;
          timeoutId = setTimeout(fetch, 10000);
        })
        .catch(() => {
          if (!mounted) return;
          setStatus(null);
          errors++;
          const delay = Math.min(10000 + (errors * 5000), 30000);
          timeoutId = setTimeout(fetch, delay);
        });
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
  const [streams, setStreams] = useState([]);
  const [error, setError] = useState(false);

  useEffect(() => {
    let timeoutId;
    let mounted = true;
    let errors = 0;

    const fetch = () => {
      DefaultService.getStreams()
        .then(data => {
          if (!mounted) return;
          setStreams(data || []);
          setError(false);
          errors = 0;
          timeoutId = setTimeout(fetch, 3000); // 3s
        })
        .catch(() => {
          if (!mounted) return;
          setError(true);
          errors++;
          const delay = Math.min(3000 + (errors * 2000), 15000);
          timeoutId = setTimeout(fetch, delay);
        });
    };

    fetch();

    return () => {
      mounted = false;
      clearTimeout(timeoutId);
    };
  }, []);

  const count = streams.length;

  const maskIP = (ip) => {
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

function formatDuration(startDate) {
  const diff = Math.floor((new Date() - startDate) / 1000);
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  const s = diff % 60;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${s}s`;
}

function LogList() {
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    DefaultService.getLogs()
      .then(data => setLogs((data || []).slice(0, 5)))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, []);

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
            <td>{new Date(log.time).toLocaleTimeString()}</td>
            <td className={`log-level ${log.level.toLowerCase()}`}>{log.level}</td>
            <td>{log.message}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function formatUptime(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}
