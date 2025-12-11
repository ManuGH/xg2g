import { useEffect, useState } from 'react';
import { DefaultService } from '../client';

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
        <button onClick={fetchHealth}>Refresh</button>
      </div>
      <div className="card-grid">
        <div className="card">
          <h3>Overall Health</h3>
          <div className={`status-indicator ${health.status}`}>
            {health.status.toUpperCase()}
          </div>
          <p>Uptime: {formatUptime(health.uptime_seconds)}</p>
          <p>Version: {health.version}</p>
        </div>

        <div className="card">
          <h3>Receiver</h3>
          <div className={`status-indicator ${health.receiver.status}`}>
            {health.receiver.status.toUpperCase()}
          </div>
          <p>Last Check: {new Date(health.receiver.last_check).getFullYear() > 2000 ? new Date(health.receiver.last_check).toLocaleString() : 'Never'}</p>
        </div>

        <div className="card">
          <h3>EPG</h3>
          <div className={`status-indicator ${health.epg.status}`}>
            {health.epg.status.toUpperCase()}
          </div>
          {health.epg.missing_channels > 0 && (
            <p className="warning">{health.epg.missing_channels} channels missing EPG</p>
          )}
        </div>
      </div>

      <div className="recent-logs-section" style={{ marginTop: '30px' }}>
        <h3>Recent Logs</h3>
        <LogList />
      </div>
    </div>
  );
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
