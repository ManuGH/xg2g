import { useEffect, useState } from 'react';
import { getBouquets, getChannels } from '../api';

export default function Channels() {
  const [bouquets, setBouquets] = useState([]);
  const [selectedBouquet, setSelectedBouquet] = useState('');
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    getBouquets().then(data => {
      setBouquets(data);
      if (data.length > 0) setSelectedBouquet(data[0].name);
    });
  }, []);

  useEffect(() => {
    if (!selectedBouquet) return;
    setLoading(true);
    getChannels(selectedBouquet)
      .then(setChannels)
      .finally(() => setLoading(false));
  }, [selectedBouquet]);

  const handleToggle = async (channel, enabled) => {
    // Optimistic update
    const originalChannels = [...channels];
    setChannels(channels.map(c =>
      (c.tvg_id === channel.tvg_id && c.name === channel.name)
        ? { ...c, enabled }
        : c
    ));

    try {
      const id = channel.tvg_id || channel.name;
      const res = await fetch('/api/v1/channels/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, enabled })
      });

      if (!res.ok) throw new Error('Failed to toggle channel');
    } catch (err) {
      console.error(err);
      // Revert on error
      setChannels(originalChannels);
      alert('Failed to update channel status');
    }
  };

  const handleBulkToggle = async (enabled) => {
    if (!confirm(`Are you sure you want to ${enabled ? 'enable' : 'disable'} ALL channels?`)) {
      return;
    }

    // Optimistic update
    const originalChannels = [...channels];
    setChannels(channels.map(c => ({ ...c, enabled })));

    try {
      const res = await fetch('/api/v1/channels/toggle-all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled })
      });

      if (!res.ok) throw new Error('Failed to toggle channels');
    } catch (err) {
      console.error(err);
      setChannels(originalChannels);
      alert('Failed to update channel status');
    }
  };

  return (
    <div className="channels-view">
      <div className="sidebar">
        <h3>Bouquets</h3>
        <ul>
          {bouquets.map(b => (
            <li
              key={b.name}
              className={b.name === selectedBouquet ? 'active' : ''}
              onClick={() => setSelectedBouquet(b.name)}
            >
              {b.name}
            </li>
          ))}
        </ul>
      </div>
      <div className="main-content">
        <div className="header-actions" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h3>Channels ({channels.length})</h3>
          <div className="actions">
            <button onClick={() => handleBulkToggle(true)} style={{ marginRight: '8px' }}>Select All</button>
            <button onClick={() => handleBulkToggle(false)}>Deselect All</button>
          </div>
        </div>
        {loading ? (
          <div>Loading...</div>
        ) : (
          <table className="channel-table">
            <thead>
              <tr>
                <th>Active</th>
                <th>#</th>
                <th>Name</th>
                <th>TVG ID</th>
                <th>EPG</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((ch, idx) => (
                <tr key={idx} className={ch.enabled === false ? 'disabled' : ''}>
                  <td>
                    <input
                      type="checkbox"
                      checked={ch.enabled !== false}
                      onChange={(e) => handleToggle(ch, e.target.checked)}
                    />
                  </td>
                  <td>{ch.number || idx + 1}</td>
                  <td>{ch.name}</td>
                  <td>{ch.tvg_id}</td>
                  <td>{ch.has_epg ? '✅' : '❌'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
