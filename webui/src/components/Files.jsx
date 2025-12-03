import { useEffect, useState } from 'react';

function Files() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(false);
  const [regenerating, setRegenerating] = useState(false);
  const [error, setError] = useState(null);

  const fetchStatus = async () => {
    setLoading(true);
    try {
      const [filesRes, urlsRes] = await Promise.all([
        fetch('/api/files/status'),
        fetch('/api/v1/ui/urls')
      ]);

      if (!filesRes.ok || !urlsRes.ok) throw new Error('Failed to fetch status');

      const filesData = await filesRes.json();
      const urlsData = await urlsRes.json();

      setStatus({ ...filesData, urls: urlsData });
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
      const res = await fetch('/api/m3u/regenerate', { method: 'POST' });
      if (!res.ok) throw new Error('Regeneration failed');
      await fetchStatus(); // Refresh status
    } catch (err) {
      setError(err.message);
    } finally {
      setRegenerating(false);
    }
  };

  if (loading && !status) return <div>Loading...</div>;
  if (error) return <div className="error">Error: {error}</div>;

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
          {status?.m3u?.exists ? (
            <>
              <p>Size: {(status.m3u.size / 1024).toFixed(2)} KB</p>
              <p>Modified: {new Date(status.m3u.last_modified).toLocaleString()}</p>
              <a href="/api/m3u/download" className="button" download>Download M3U</a>
            </>
          ) : (
            <p className="missing">File missing</p>
          )}
        </div>

        <div className="file-card">
          <h3>XMLTV Guide</h3>
          {status?.xmltv?.exists ? (
            <>
              <p>Size: {(status.xmltv.size / 1024 / 1024).toFixed(2)} MB</p>
              <p>Modified: {new Date(status.xmltv.last_modified).toLocaleString()}</p>
              <a href="/api/xmltv/download" className="button" download>Download XMLTV</a>
            </>
          ) : (
            <p className="missing">File missing</p>
          )}
        </div>

        <div className="file-card">
          <h3>HDHomeRun (Plex)</h3>
          <p className="description">Use this IP/URL to add xg2g as a DVR in Plex or Jellyfin.</p>
          {status?.urls?.hdhr_url ? (
            <>
              <div className="code-block">
                {status.urls.hdhr_url.replace('/device.xml', '')}
              </div>
              <button
                className="button secondary"
                onClick={() => navigator.clipboard.writeText(status.urls.hdhr_url.replace('/device.xml', ''))}
              >
                Copy IP
              </button>
            </>
          ) : (
            <p className="missing">Loading...</p>
          )}
        </div>
      </div>
    </div>
  );
}

export default Files;
