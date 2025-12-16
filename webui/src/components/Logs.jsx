import { useEffect, useState } from 'react';
import { DefaultService } from '../client';

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
    fetchLogs();
  }, []);

  return (
    <div className="logs-view">
      <div className="logs-header">
        <h3>Recent Logs</h3>
        <button onClick={fetchLogs} disabled={loading}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {error && <div className="error">Error: {error}</div>}

      {logs.length === 0 && !loading ? (
        <p>No logs available.</p>
      ) : (
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
              <tr key={idx} className={`log-row ${log.level.toLowerCase()}`}>
                <td className="log-time">{new Date(log.time).toLocaleTimeString()}</td>
                <td className="log-level">{log.level}</td>
                <td className="log-component">{log.component}</td>
                <td className="log-message">{log.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
