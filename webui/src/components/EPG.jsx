import React from 'react';

function formatTime(ts) {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function formatDateTime(ts) {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  const date = d.toLocaleDateString([], { weekday: 'short', day: '2-digit', month: '2-digit' });
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return `${date} ${time}`;
}

function formatRange(start, end) {
  if (!start || !end) return '';
  return `${formatTime(start)} – ${formatTime(end)}`;
}

function ProgrammeRow({ prog, now, highlight = false }) {
  const inProgress = now >= prog.start && now <= prog.end;
  const total = prog.end - prog.start;
  const elapsed = Math.max(0, Math.min(total, now - prog.start));
  const pct = total > 0 ? Math.round((elapsed / total) * 100) : 0;

  return (
    <div className={`epg-programme${highlight ? ' epg-programme-current' : ''}`}>
      <div className="epg-programme-time">{formatRange(prog.start, prog.end)}</div>
      <div className="epg-programme-body">
        <div className="epg-programme-title">
          {prog.title || '—'}
        </div>
        {prog.desc && (
          <div className="epg-programme-desc">{prog.desc}</div>
        )}
        {inProgress && (
          <div className="epg-progress">
            <div className="epg-progress-bar" style={{ width: `${pct}%` }} />
            <div className="epg-progress-meta">
              <span>{formatTime(prog.start)}</span>
              <span>{pct}%</span>
              <span>{formatTime(prog.end)}</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default function EPG({ channels, bouquets = [], selectedBouquet = '', onSelectBouquet, onPlay }) {
  const [programmes, setProgrammes] = React.useState([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState(null);
  const [hours, setHours] = React.useState(6);
  const [now, setNow] = React.useState(Date.now() / 1000);
  const [expanded, setExpanded] = React.useState({});
  const [searchQuery, setSearchQuery] = React.useState('');
  const [searchResults, setSearchResults] = React.useState([]);
  const [searchLoading, setSearchLoading] = React.useState(false);
  const [searchError, setSearchError] = React.useState(null);
  const [searchRan, setSearchRan] = React.useState(false);

  const runSearch = async (query, bouquet) => {
    const q = query.trim();
    if (!q) return;
    setSearchLoading(true);
    setSearchError(null);
    setSearchRan(true);
    try {
      const token = localStorage.getItem('XG2G_API_TOKEN');
      const bouquetParam = bouquet ? `&bouquet=${encodeURIComponent(bouquet)}` : '';
      const resp = await fetch(`/api/v2/epg?q=${encodeURIComponent(q)}${bouquetParam}`, {
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });
      if (!resp.ok) throw new Error(`Search failed (${resp.status})`);
      const data = await resp.json();
      setSearchResults(data.items || []);
    } catch (err) {
      console.error(err);
      setSearchError('Suche fehlgeschlagen.');
    } finally {
      setSearchLoading(false);
    }
  };

  React.useEffect(() => {
    const t = setInterval(() => setNow(Date.now() / 1000), 60_000);
    return () => clearInterval(t);
  }, []);

  const channelMap = React.useMemo(() => {
    const map = {};
    (channels || []).forEach((ch) => {
      const ref = ch.service_ref || ch.serviceRef || ch.id;
      if (ref) map[ref] = ch;
      if (ch.id && !map[ch.id]) {
        map[ch.id] = ch;
      }
    });
    return map;
  }, [channels]);

  const channelOrder = React.useMemo(() => {
    const order = new Map();
    (channels || []).forEach((ch, idx) => {
      const ref = ch.service_ref || ch.serviceRef || ch.id;
      if (!ref || order.has(ref)) return;
      order.set(ref, { idx, number: ch.number });
      if (ch.id && !order.has(ch.id)) {
        order.set(ch.id, { idx, number: ch.number });
      }
    });
    return order;
  }, [channels]);

  const grouped = React.useMemo(() => {
    const g = {};
    (programmes || []).forEach((p) => {
      if (!g[p.service_ref]) g[p.service_ref] = [];
      g[p.service_ref].push(p);
    });
    Object.values(g).forEach((list) => list.sort((a, b) => a.start - b.start));
    return g;
  }, [programmes]);

  const fetchEPG = async () => {
    setLoading(true);
    setError(null);
    try {
      const token = localStorage.getItem('XG2G_API_TOKEN');
      const from = Math.floor(Date.now() / 1000) - 1800; // 30 min ago
      const to = from + hours * 3600;
      const bouquetParam = selectedBouquet ? `&bouquet=${encodeURIComponent(selectedBouquet)}` : '';
      const resp = await fetch(`/api/v2/epg?from=${from}&to=${to}${bouquetParam}`, {
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });
      if (!resp.ok) throw new Error(`EPG fetch failed (${resp.status})`);
      const data = await resp.json();
      setProgrammes(data.items || []);
    } catch (err) {
      console.error(err);
      setError('EPG konnte nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  React.useEffect(() => {
    fetchEPG();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hours, selectedBouquet]);

  const orderedRefs = React.useMemo(() => {
    const keys = Object.keys(grouped);
    return keys.sort((a, b) => {
      const aMeta = channelOrder.get(a);
      const bMeta = channelOrder.get(b);

      // Try numeric channel number first
      if (aMeta && bMeta) {
        const aNum = parseInt(aMeta.number, 10);
        const bNum = parseInt(bMeta.number, 10);
        const aNumValid = !Number.isNaN(aNum);
        const bNumValid = !Number.isNaN(bNum);
        if (aNumValid && bNumValid && aNum !== bNum) {
          return aNum - bNum;
        }
        if (aMeta.idx !== bMeta.idx) {
          return aMeta.idx - bMeta.idx;
        }
      }

      if (aMeta && !bMeta) return -1;
      if (!aMeta && bMeta) return 1;

      // Fallback: alphabetical by channel name
      const ca = channelMap[a]?.name || a;
      const cb = channelMap[b]?.name || b;
      return ca.localeCompare(cb);
    });
  }, [grouped, channelMap, channelOrder]);

  return (
    <div className="epg-page">
      <div className="epg-toolbar">
        <div className="epg-toolbar-left">
          <h3>EPG Übersicht</h3>
          <p>Zeitraum: jetzt bis +{hours}h</p>
        </div>
        <div className="epg-toolbar-right">
          {bouquets.length > 0 && (
            <label>
              Bouquet
              <select
                value={selectedBouquet}
                onChange={(e) => onSelectBouquet && onSelectBouquet(e.target.value)}
              >
                <option value="">Alle</option>
                {bouquets.map((b) => (
                  <option key={b.name} value={b.name}>{b.name}</option>
                ))}
              </select>
            </label>
          )}
          <label>
            Zeitfenster
            <select value={hours} onChange={(e) => setHours(parseInt(e.target.value, 10))}>
              <option value={3}>3 Stunden</option>
              <option value={6}>6 Stunden</option>
              <option value={12}>12 Stunden</option>
            </select>
          </label>
          <button onClick={fetchEPG} disabled={loading}>Neu laden</button>
        </div>
      </div>

      <div className="epg-search">
        <div className="epg-search-left">
          <div className="epg-search-icon">⏎</div>
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => {
              const val = e.target.value;
              setSearchQuery(val);
              if (!val.trim()) {
                setSearchRan(false);
                setSearchResults([]);
                setSearchError(null);
                setSearchLoading(false);
              }
            }}
            placeholder="Suche nach Sendungen (z.B. ZIB)"
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                runSearch(searchQuery, selectedBouquet);
              }
            }}
          />
          {bouquets.length > 0 && (
            <select
              value={selectedBouquet}
              onChange={(e) => {
                const val = e.target.value;
                onSelectBouquet && onSelectBouquet(val);
                if (searchRan && searchQuery.trim()) {
                  runSearch(searchQuery, val);
                }
              }}
            >
              <option value="">Alle Bouquets</option>
              {bouquets.map((b) => (
                <option key={b.name} value={b.name}>{b.name}</option>
              ))}
            </select>
          )}
        </div>
        <div className="epg-search-right">
          <button
            onClick={() => runSearch(searchQuery, selectedBouquet)}
            disabled={searchLoading || !searchQuery.trim()}
          >
            {searchLoading ? 'Suche …' : 'Suche'}
          </button>
        </div>
      </div>

      {searchError && <div className="epg-card epg-error">{searchError}</div>}
      {searchRan && !searchLoading && searchResults.length === 0 && !searchError && (
        <div className="epg-card">Keine Treffer für “{searchQuery.trim()}” gefunden.</div>
      )}
      {searchResults.length > 0 && (
        <div className="epg-card">
          <div className="epg-channel">
            <div className="epg-channel-meta">
              <div className="epg-channel-name">Suchergebnisse: “{searchQuery.trim()}” (7 Tage)</div>
            </div>
          </div>
          <div className="epg-programmes">
            {searchResults.map((prog) => {
              const ch = channelMap[prog.service_ref];
              const logo = ch?.logo_url || ch?.logoUrl || ch?.logo;
              const displayName = ch
                ? `${ch.number ? `${ch.number} · ` : ''}${ch.name || ch.id || prog.service_ref}`
                : prog.service_ref;
              const range = (() => {
                const startStr = formatDateTime(prog.start);
                const endStr = formatDateTime(prog.end);
                const startDate = new Date(prog.start * 1000);
                const endDate = new Date(prog.end * 1000);
                if (startDate.toDateString() === endDate.toDateString()) {
                  return `${startStr} – ${formatTime(prog.end)}`;
                }
                return `${startStr} – ${endStr}`;
              })();
              return (
                <div className="epg-programme epg-programme-search" key={`${prog.service_ref}-${prog.start}`}>
                  <div className="epg-programme-time">{range}</div>
                  <div className="epg-programme-body">
                    <div className="epg-programme-title">
                      {prog.title || '—'}
                    </div>
                    <div className="epg-programme-desc">{displayName}</div>
                    {onPlay && ch && (
                      <button
                        className="epg-play-btn"
                        onClick={() => onPlay(ch)}
                        title="Play"
                      >
                        ▶
                      </button>
                    )}
                    {prog.desc && <div className="epg-programme-desc">{prog.desc}</div>}
                    {logo && (
                      <div className="epg-search-logo">
                        <img
                          src={logo}
                          alt={displayName}
                          onError={(e) => {
                            e.target.onerror = null;
                            e.target.style.display = 'none';
                          }}
                        />
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {loading && <div className="epg-card">EPG wird geladen …</div>}
      {error && <div className="epg-card epg-error">{error}</div>}

      {!searchRan && !loading && !error && orderedRefs.map((ref) => {
        const ch = channelMap[ref];
        const list = grouped[ref] || [];
        if (list.length === 0) return null;

        const current = list.find((p) => now >= p.start && now <= p.end) || list[0];
        const others = list.filter((p) => p !== current);
        const logo = ch?.logo_url || ch?.logoUrl || ch?.logo;
        const displayName = ch
          ? `${ch.number ? `${ch.number} · ` : ''}${ch.name || ch.id || ref}`
          : ref;

        return (
          <div className="epg-card" key={ref}>
            <div className="epg-channel">
              <div className="epg-logo">
                {logo ? (
                  <img
                    src={logo}
                    alt={ch?.name || ref}
                    onError={(e) => {
                      e.target.onerror = null;
                      e.target.style.display = 'none';
                      e.target.parentNode.innerHTML = '<span>🎬</span>';
                    }}
                  />
                ) : (
                  <span>🎬</span>
                )}
              </div>
              <div className="epg-channel-meta">
                <div className="epg-channel-name">{displayName}</div>
                {ch?.group && <div className="epg-channel-group">{ch.group}</div>}
              </div>
              {onPlay && ch && (
                <button
                  className="epg-play-btn"
                  onClick={() => onPlay(ch)}
                  title="Play"
                >
                  ▶
                </button>
              )}
            </div>
            <div className="epg-programmes">
              {current && (
                <ProgrammeRow
                  key={`${current.service_ref}-${current.start}-current`}
                  prog={current}
                  now={now}
                  highlight
                />
              )}

              {others.length > 0 && (
                <div className="epg-dropdown">
                  <button
                    className="epg-toggle"
                    onClick={() => setExpanded((prev) => ({ ...prev, [ref]: !prev[ref] }))}
                  >
                    {expanded[ref] ? 'Andere Sendungen ausblenden' : `Weitere Sendungen (${others.length})`}
                  </button>
                  {expanded[ref] && (
                    <div className="epg-programmes-noncurrent">
                      {others.map((prog) => (
                        <ProgrammeRow
                          key={`${prog.service_ref}-${prog.start}`}
                          prog={prog}
                          now={now}
                          highlight={false}
                        />
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
