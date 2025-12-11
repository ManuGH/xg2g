import { useState, useEffect } from 'react';
import './App.css';
import Channels from './components/Channels';
import Dashboard from './components/Dashboard';
import Files from './components/Files';
import Logs from './components/Logs';
import Config from './components/Config';
import StatusIndicator from './components/StatusIndicator';

function App() {
  const [view, setView] = useState('dashboard');
  const [showAuth, setShowAuth] = useState(!localStorage.getItem('XG2G_API_TOKEN'));
  const [token, setToken] = useState(localStorage.getItem('XG2G_API_TOKEN') || '');

  useEffect(() => {
    const handleAuth = () => setShowAuth(true);
    window.addEventListener('auth-required', handleAuth);

    return () => window.removeEventListener('auth-required', handleAuth);
  }, []);

  const saveToken = () => {
    localStorage.setItem('XG2G_API_TOKEN', token);
    setShowAuth(false);
    // Reload to retry failed requests or just verify
    window.location.reload();
  };

  return (
    <div className="app-container">
      {showAuth && (
        <div className="auth-modal-overlay">
          <div className="auth-modal">
            {/* Force hash change 123 */}
            <h2>Authentication Required</h2>
            <p>Please enter your API Token</p>
            <input
              type="password"
              value={token}
              onChange={e => setToken(e.target.value)}
              placeholder="XG2G_API_TOKEN"
            />
            <button onClick={saveToken} disabled={!token}>Save</button>
          </div>
        </div>
      )}

      <header className="app-header">
        <h1>xg2g WebUI</h1>
        <nav>
          <button
            className={view === 'dashboard' ? 'active' : ''}
            onClick={() => setView('dashboard')}
          >
            Dashboard
          </button>
          <button
            className={view === 'channels' ? 'active' : ''}
            onClick={() => setView('channels')}
          >
            Channels
          </button>
          <button
            className={view === 'files' ? 'active' : ''}
            onClick={() => setView('files')}
          >
            Files
          </button>
          <button
            className={view === 'logs' ? 'active' : ''}
            onClick={() => setView('logs')}
          >
            Logs
          </button>
          <button
            className={view === 'config' ? 'active' : ''}
            onClick={() => setView('config')}
          >
            Config
          </button>
          <a href="/webplayer" target="_blank" rel="noopener noreferrer" style={{ textDecoration: 'none' }}>
            <button className="nav-btn-external">
              📺 Web Player
            </button>
          </a>
        </nav>
      </header>
      <main className="app-main">
        {view === 'dashboard' && <Dashboard />}
        {view === 'channels' && <Channels />}
        {view === 'files' && <Files />}
        {view === 'logs' && <Logs />}
        {view === 'config' && <Config />}
      </main>
    </div>
  );
}

export default App;

