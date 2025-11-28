import { useState } from 'react';
import './App.css';
import Channels from './components/Channels';
import Dashboard from './components/Dashboard';
import Logs from './components/Logs';

function App() {
  const [view, setView] = useState('dashboard');

  return (
    <div className="app-container">
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
            className={view === 'logs' ? 'active' : ''}
            onClick={() => setView('logs')}
          >
            Logs
          </button>
        </nav>
      </header>
      <main className="app-main">
        {view === 'dashboard' && <Dashboard />}
        {view === 'channels' && <Channels />}
        {view === 'logs' && <Logs />}
      </main>
    </div>
  );
}

export default App;
