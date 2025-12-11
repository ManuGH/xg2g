import { useState, useEffect } from 'react';
import './App.css';
import Channels from './components/Channels';
import Player from './components/Player';
import Dashboard from './components/Dashboard';
import Files from './components/Files';
import Logs from './components/Logs';
import Config from './components/Config';
import StatusIndicator from './components/StatusIndicator';
import { OpenAPI } from './client/core/OpenAPI';
import { DefaultService } from './client/services/DefaultService';

function App() {
  const [view, setView] = useState('dashboard');
  const [showAuth, setShowAuth] = useState(!localStorage.getItem('XG2G_API_TOKEN'));
  const [token, setToken] = useState(localStorage.getItem('XG2G_API_TOKEN') || '');

  // Channel Data State (Lifted from Channels.jsx)
  const [bouquets, setBouquets] = useState([]);
  const [selectedBouquet, setSelectedBouquet] = useState(''); // Empty string = "All Channels"
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);
  const [dataLoaded, setDataLoaded] = useState(false); // To avoid re-fetching on tab switch

  useEffect(() => {
    const handleAuth = () => setShowAuth(true);
    window.addEventListener('auth-required', handleAuth);

    // Initialize OpenAPI client with token
    const storedToken = localStorage.getItem('XG2G_API_TOKEN');
    if (storedToken) {
      OpenAPI.TOKEN = storedToken;
      // Initial load if token exists
      if (!dataLoaded) {
        loadBouquetsAndChannels();
      }
    }

    return () => window.removeEventListener('auth-required', handleAuth);
  }, []); // Run once on mount

  const loadBouquetsAndChannels = async () => {
    setLoading(true);
    try {
      // 1. Fetch Bouquets
      const bouquetData = await DefaultService.getServicesBouquets();
      setBouquets(bouquetData || []);

      // 2. Fetch All Channels (default view)
      // If we already have a selected bouquet (e.g. from saved state), use it. 
      // Otherwise default to '' (All).
      await loadChannels(selectedBouquet);

      setDataLoaded(true);
    } catch (err) {
      console.error('Failed to load initial data:', err);
      if (err.status === 401) setShowAuth(true);
    } finally {
      setLoading(false);
    }
  };

  const loadChannels = async (bouquetName) => {
    setLoading(true);
    try {
      // If bouquetName is empty string, we fetch ALL services (no bouquet param)
      // If bouquetName is set, we fetch for that bouquet
      const params = bouquetName ? { bouquet: bouquetName } : {};
      const data = await DefaultService.getServices(params);
      setChannels(data || []);
      setSelectedBouquet(bouquetName);
    } catch (err) {
      console.error('Failed to load channels:', err);
      if (err.status === 401) setShowAuth(true);
    } finally {
      setLoading(false);
    }
  };

  const handleToggle = async (channel, enabled) => {
    // Optimistic update
    const originalChannels = [...channels];
    setChannels(channels.map(c =>
      (c.id === channel.id) // v2 uses 'id' (schema: Service.id)
        ? { ...c, enabled }
        : c
    ));

    try {
      const id = channel.id; // v2 service ID
      await DefaultService.postServicesToggle(id, { enabled });
    } catch (err) {
      console.error(err);
      // Revert on error
      setChannels(originalChannels);
      alert('Failed to update channel status');
    }
  };

  const saveToken = () => {
    localStorage.setItem('XG2G_API_TOKEN', token);
    OpenAPI.TOKEN = token;
    setShowAuth(false);
    window.location.reload();
  };

  // Player State
  const [playingChannel, setPlayingChannel] = useState(null);

  // ... (existing helper functions)

  return (
    <div className="app-container">
      {/* Auth Modal ... */}
      {showAuth && (
        <div className="auth-modal-overlay">
          {/* ... existing auth modal content ... */}
          <div className="auth-modal">
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

      {/* Player Overlay */}
      {playingChannel && (
        <Player
          streamUrl={`/stream/${playingChannel.id}/playlist.m3u8`}
          onClose={() => setPlayingChannel(null)}
        />
      )}

      <header className="app-header">
        {/* ... existing header content ... */}
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
        </nav>
      </header>
      <main className="app-main">
        {view === 'dashboard' && <Dashboard />}
        {view === 'channels' && (
          <Channels
            bouquets={bouquets}
            channels={channels}
            loading={loading}
            selectedBouquet={selectedBouquet}
            onSelectBouquet={loadChannels}
            // setChannels={setChannels} // No longer needed directly if we pass handleToggle
            onToggle={handleToggle}
            onPlay={setPlayingChannel}
          />
        )}
        {view === 'files' && <Files />}
        {view === 'logs' && <Logs />}
        {view === 'config' && <Config />}
      </main>
    </div>
  );
}

export default App;

