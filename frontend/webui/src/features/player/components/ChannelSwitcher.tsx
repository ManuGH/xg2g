import { useEffect, useMemo, useRef, useState } from 'react';
import type { Service } from '../../../client-ts';
import styles from './ChannelSwitcher.module.css';

interface ChannelSwitcherProps {
  channels: Service[];
  current?: Service;
  onSwitch: (channel: Service) => void;
}

const refOf = (c?: Service): string => c?.serviceRef ?? c?.id ?? '';
const initials = (name?: string) => (name ?? '?').replace(/\s+/g, '').slice(0, 3).toUpperCase();

/**
 * Simple in-player channel list: a visible "Sender" button opens a frosted-glass
 * panel (web: right, mobile: bottom sheet) listing every channel with logo + name,
 * the current one highlighted, with search. Click a channel to switch in place
 * (onSwitch = AppContext.handlePlay) — no second page. Live-only.
 */
export function ChannelSwitcher({ channels, current, onSwitch }: ChannelSwitcherProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const listRef = useRef<HTMLDivElement>(null);
  const currentRef = refOf(current);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter(
      (c) => (c.name ?? '').toLowerCase().includes(q) || (c.number ?? '').includes(q),
    );
  }, [channels, query]);

  // Center the current channel when opening.
  useEffect(() => {
    if (!open) return;
    listRef.current
      ?.querySelector<HTMLElement>(`[data-ref="${CSS.escape(currentRef)}"]`)
      ?.scrollIntoView({ block: 'center' });
  }, [open, currentRef]);

  // Esc closes.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  return (
    <>
      <button className={styles.toggle} onClick={() => setOpen((o) => !o)} aria-label="Senderliste öffnen">
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path fill="currentColor" d="M4 6h16v2H4zm0 5h16v2H4zm0 5h16v2H4z" />
        </svg>
        <span className={styles.toggleLabel}>Sender</span>
      </button>

      {open ? (
        <div className={styles.scrim} onClick={() => setOpen(false)}>
          <aside className={styles.sheet} onClick={(e) => e.stopPropagation()} role="dialog" aria-label="Sender wechseln">
            <div className={styles.header}>
              <input
                className={styles.search}
                placeholder="Sender suchen…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                autoFocus
                aria-label="Sender suchen"
              />
              <button className={styles.close} onClick={() => setOpen(false)} aria-label="Schließen">✕</button>
            </div>
            <div className={styles.list} ref={listRef}>
              {filtered.map((c) => {
                const ref = refOf(c);
                const active = ref === currentRef;
                return (
                  <button
                    key={ref}
                    data-ref={ref}
                    className={`${styles.row} ${active ? styles.active : ''}`}
                    onClick={() => {
                      if (!active) onSwitch(c);
                      setOpen(false);
                    }}
                  >
                    {c.logoUrl ? (
                      <img className={styles.logo} src={c.logoUrl} alt="" loading="lazy" />
                    ) : (
                      <span className={styles.logoFallback}>{initials(c.name)}</span>
                    )}
                    {c.number ? <span className={styles.num}>{c.number}</span> : null}
                    <span className={styles.name}>{c.name ?? ref}</span>
                    {active ? <span className={styles.live}>● live</span> : null}
                  </button>
                );
              })}
              {filtered.length === 0 ? <div className={styles.empty}>Kein Sender gefunden</div> : null}
            </div>
          </aside>
        </div>
      ) : null}
    </>
  );
}

export default ChannelSwitcher;
