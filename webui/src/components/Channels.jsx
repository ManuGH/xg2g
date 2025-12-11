// Imports removed as they are no longer used

export default function Channels({
  bouquets,
  channels,
  loading,
  selectedBouquet,
  onSelectBouquet,
  onToggle,
  onPlay
}) {

  return (
    <div className="channels-view">
      <div className="sidebar">
        <h3>Bouquets</h3>
        <ul>
          <li
            className={selectedBouquet === '' ? 'active' : ''}
            onClick={() => onSelectBouquet('')}
          >
            All Channels
          </li>
          {bouquets.map(b => (
            <li
              key={b.name}
              className={b.name === selectedBouquet ? 'active' : ''}
              onClick={() => onSelectBouquet(b.name)}
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
                      onChange={(e) => onToggle(ch, e.target.checked)}
                    />
                  </td>
                  <td>{ch.number || idx + 1}</td>
                  <td className="picon-cell" title="Play">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                      {ch.logo_url ? (
                        <img
                          src={ch.logo_url}
                          alt={ch.name}
                          onClick={() => onPlay(ch)}
                          style={{
                            width: '80px',
                            height: 'auto',
                            display: 'block',
                            cursor: 'pointer',
                            filter: 'drop-shadow(0px 1px 2px rgba(0,0,0,0.8))'
                          }}
                        />
                      ) : (
                        <button onClick={() => onPlay(ch)} style={{ cursor: 'pointer' }}>▶️</button>
                      )}
                    </div>
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


