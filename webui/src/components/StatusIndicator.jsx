import { useState, useEffect } from 'react';
import { DefaultService } from '../client';

export default function StatusIndicator() {
  const [status, setStatus] = useState('unknown'); // unknown, connected, disconnected
  const [lastCheck, setLastCheck] = useState(null);

  useEffect(() => {
    const check = async () => {
      try {
        await DefaultService.getSystemHealth();
        setStatus('connected');
        setLastCheck(new Date());
      } catch (err) {
        // OpenAPI client throws errors that might have status/message
        if (err.status === 401 || err.status === 403) {
          setStatus('connected'); // Valid response but auth issue
        } else {
          setStatus('disconnected');
        }
      }
    };

    check();
    const interval = setInterval(check, 10000); // Check every 10s
    return () => clearInterval(interval);
  }, []);

  const getColor = () => {
    switch (status) {
      case 'connected': return '#4caf50'; // Green
      case 'disconnected': return '#f44336'; // Red
      default: return '#9e9e9e'; // Grey
    }
  };

  return (
    <div className="status-indicator" title={`Backend: ${status} (Last check: ${lastCheck ? lastCheck.toLocaleTimeString() : 'never'})`}>
      <span style={{
        display: 'inline-block',
        width: '10px',
        height: '10px',
        borderRadius: '50%',
        backgroundColor: getColor(),
        marginRight: '8px',
        boxShadow: status === 'connected' ? '0 0 5px #4caf50' : 'none',
        transition: 'background-color 0.3s ease'
      }}></span>
      <span style={{ fontSize: '0.8em', color: 'var(--text-color)', opacity: 0.8 }}>
        {status === 'connected' ? 'Online' : 'Offline'}
      </span>
    </div>
  );
}
