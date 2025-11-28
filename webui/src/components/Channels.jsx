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
        <h3>Channels ({channels.length})</h3>
        {loading ? (
          <div>Loading...</div>
        ) : (
          <table className="channel-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Name</th>
                <th>TVG ID</th>
                <th>EPG</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((ch, idx) => (
                <tr key={idx}>
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
