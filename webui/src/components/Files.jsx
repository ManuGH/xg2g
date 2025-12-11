import { useEffect, useState } from 'react';
import { getHealth, regeneratePlaylist } from '../api';

function Files() {
  const [health, setHealth] = useState(null);
  const [loading, setLoading] = useState(false);
  const [regenerating, setRegenerating] = useState(false);
  const [error, setError] = useState(null);

  const fetchStatus = async () => {
    setLoading(true);
    try {
      const data = await getHealth();
      setHealth(data);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStatus();
  }, []);

  const handleRegenerate = async () => {
    setRegenerating(true);
    try {
      await regeneratePlaylist();
      // Wait a bit for files to likely be written before refreshing status
      setTimeout(fetchStatus, 1000);
    } catch (err) {
      setError(err.message);
    } finally {
      setRegenerating(false);
    }
  };

  if (loading && !health) return <div>Loading...</div>;
  if (error) return <div className="error">Error: {error}</div>;

  const hdhrUrl = `${window.location.protocol}//${window.location.host}/device.xml`;
  const m3uUrl = '/files/playlist.m3u';
  const xmltvUrl = '/files/epg.xml';

  return (
    <div className="files-container">
      <h2>Playlist & EPG</h2>

      <div className="actions">
        <button onClick={handleRegenerate} disabled={regenerating}>
          {regenerating ? 'Regenerating...' : 'Regenerate Files'}
        </button>
      </div>

      <div className="file-list">
        <div className="file-card">
          <h3>M3U Playlist</h3>
          <p className="description">Standard M3U8 playlist for VLC, Kodi, TiviMate.</p>
          <a href={m3uUrl} className="button" download>Download M3U</a>
        </div>

        <div className="file-card">
          <h3>XMLTV Guide</h3>
          <p className="description">EPG Data.</p>
          {health?.epg?.status === 'ok' ? (
            <p className="success">EPG Loaded</p>
          ) : (
            <p className="warning">EPG Missing or Partial</p>
          )}
          <div className="code-block" style={{ marginBottom: '10px' }}>
            {`${window.location.protocol}//${window.location.host}/xmltv.xml`}
          </div>
          <div className="actions-row" style={{ display: 'flex', gap: '10px' }}>
            <button
              className="button secondary"
              onClick={() => navigator.clipboard.writeText(`${window.location.protocol}//${window.location.host}/xmltv.xml`)}
            >
              Copy URL
            </button>
            <a href="/xmltv.xml" className="button" download>Download</a>
          </div>
        </div>

        <div className="file-card">
          <h3>HDHomeRun (Plex)</h3>
          <p className="description">Use this IP/URL to add xg2g as a DVR in Plex or Jellyfin.</p>
          <div className="code-block">
            {hdhrUrl.replace('/device.xml', '')}
          </div>
          <button
            className="button secondary"
            onClick={() => navigator.clipboard.writeText(hdhrUrl.replace('/device.xml', ''))}
          >
            Copy IP
          </button>
        </div>
      </div>
    </div>
  );
}

export default Files;

