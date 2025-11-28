import { useEffect, useState } from 'react';
import { getHealth } from '../api';

export default function Dashboard() {
  const [health, setHealth] = useState(null);
  const [error, setError] = useState(null);

  const fetchHealth = () => {
    getHealth()
      .then(setHealth)
      .catch(err => setError(err.message));
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
          <p>Last Check: {new Date(health.receiver.last_check).toLocaleString()}</p>
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
    </div>
  );
}

function formatUptime(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}
