import React from 'react';

export default function Streaming({
  bouquets,
  channels,
  loading,
  selectedBouquet,
  onSelectBouquet,
  onPlay
}) {
  return (
    <div className="streaming-page">
      <div className="streaming-sidebar">
        <h3>Bouquets</h3>
        <div className="streaming-select-wrapper">
          <select
            value={selectedBouquet}
            onChange={(e) => onSelectBouquet(e.target.value)}
          >
            <option value="">Alle Sender</option>
            {bouquets.map(b => (
              <option key={b.name} value={b.name}>{b.name}</option>
            ))}
          </select>
        </div>
        <div className="streaming-count">
          {channels.length} Sender
        </div>
      </div>

      <div className="streaming-list">
        {loading ? (
          <div className="streaming-card">Lade Sender …</div>
        ) : (
          channels.map((ch) => (
            <div className="streaming-card" key={ch.id}>
              <div className="streaming-row">
                <div className="streaming-logo">
                  {ch.logo_url ? (
                    <img src={ch.logo_url} alt={ch.name} />
                  ) : (
                    <span>🎬</span>
                  )}
                </div>
                <div className="streaming-meta">
                  <div className="streaming-name">{ch.name}</div>
                  <div className="streaming-ref">{ch.id}</div>
                </div>
                <button className="btn-play" onClick={() => onPlay(ch)}>
                  ▶︎ Play
                </button>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
