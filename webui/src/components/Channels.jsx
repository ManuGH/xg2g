import { useEffect, useState } from 'react';
import { getBouquets, getChannels, toggleService } from '../api';

export default function Channels() {
  const [bouquets, setBouquets] = useState([]);
  const [selectedBouquet, setSelectedBouquet] = useState('');
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    getBouquets().then(data => {
      // API v2 returns array of Bouquet objects directly (check openapi.yaml)
      // Spec: array of items: $ref: "#/components/schemas/Bouquet"
      setBouquets(data || []);
      if (data && data.length > 0) setSelectedBouquet(data[0].name);
    }).catch(console.error);
  }, []);

  useEffect(() => {
    if (!selectedBouquet) return;
    setLoading(true);
    getChannels(selectedBouquet)
      .then(data => setChannels(data || []))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [selectedBouquet]);

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
      await toggleService(id, enabled);
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

    // This feature is not yet in API v2 (bulk toggle)
    alert("Bulk toggle not supported in API v2 yet.");
    /*
    // Optimistic update
    const originalChannels = [...channels];
    setChannels(channels.map(c => ({ ...c, enabled })));

    try {
      // TODO: Implement bulk toggle in backend v2
    } catch (err) { ... }
    */
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
          {/* Bulk actions disabled for now */}
        </div>
        {loading ? (
          <div>Loading...</div>
        ) : (
          <table className="channel-table">
            <thead>
              <tr>
                <th>Active</th>
                <th style={{ width: '40px' }}>#</th>
                <th style={{ width: '50px' }}>Logo</th>
                <th>Name</th>
                <th>Service Ref</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((ch, idx) => (
                <tr key={ch.id} className={ch.enabled === false ? 'disabled' : ''}>
                  <td>
                    <input
                      type="checkbox"
                      checked={ch.enabled !== false}
                      onChange={(e) => handleToggle(ch, e.target.checked)}
                    />
                  </td>
                  <td>{ch.number || idx + 1}</td>
                  <td className="picon-cell" title="Play in Web Player">
                    {ch.logo_url && (
                      <a
                        href={`/webplayer?channel=${encodeURIComponent(ch.id)}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{ display: 'block' }}
                      >
                        <img
                          src={ch.logo_url}
                          alt={ch.name}
                          referrerPolicy="no-referrer"
                          style={{
                            width: '100px',
                            height: 'auto',
                            display: 'block',
                            filter: 'drop-shadow(0px 1px 2px rgba(0,0,0,0.8))',
                            cursor: 'pointer'
                          }}
                        />
                      </a>
                    )}
                  </td>
                  <td>{ch.name}</td>
                  <td>{ch.id}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}


