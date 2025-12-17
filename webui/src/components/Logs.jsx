import { useEffect, useState } from 'react';
import { DefaultService } from '../client';
import './Logs.css';

export default function Logs() {
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  const fetchLogs = () => {
    setLoading(true);
    DefaultService.getLogs()
      .then(setLogs)
      .catch(err => {
        if (err.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          setError(err.message || 'Failed to fetch logs');
        }
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchLogs();
  }, []);

  return (
    <div className="logs-view">
      <div className="logs-header">
        <h3>Recent Logs</h3>
        <button onClick={fetchLogs} disabled={loading} className="logs-btn logs-btn-primary">
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {error && <div className="logs-alert logs-alert-error">Error: {error}</div>}

      {logs.length === 0 && !loading ? (
        <p className="logs-empty">No logs available.</p>
      ) : (
        <div className="logs-table-wrap">
          <table className="logs-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Level</th>
                <th>Component</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log, idx) => (
                <tr key={idx} className={`log-row level-${String(log.level || '').toLowerCase()}`}>
                  <td className="log-time">{new Date(log.time).toLocaleTimeString()}</td>
                  <td className="log-level">{log.level}</td>
                  <td className="log-component">{log.component}</td>
                  <td className="log-message">{log.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
