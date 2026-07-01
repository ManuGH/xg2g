import { useEffect, useMemo, useRef, useState } from 'react';
import type { Service } from '../../../client-ts';
import styles from './ChannelSwitcher.module.css';

interface ChannelSwitcherProps {
  channels: Service[];
  current?: Service;
  onSwitch: (channel: Service) => void;
  open: boolean;
  onClose: () => void;
}

const refOf = (c?: Service): string => c?.serviceRef ?? c?.id ?? '';
const initials = (name?: string) => (name ?? '?').replace(/\s+/g, '').slice(0, 3).toUpperCase();

/**
 * In-player channel list. The trigger lives in the player control bar; this renders
 * only the frosted-glass panel (web: slides in from the right, mobile: bottom sheet),
 * listing every channel with logo + name, the current one highlighted, with search.
 * Click a channel to switch in place (onSwitch = AppContext.handlePlay) — no second
 * page. Live-only.
 *
 * Controlled (open/onClose) and positioned `absolute` WITHIN the player container —
 * never `fixed` — so it lands on the player whether windowed or fullscreen.
 */
export function ChannelSwitcher({ channels, current, onSwitch, open, onClose }: ChannelSwitcherProps) {
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

  // Reset search + center the current channel each time the panel opens.
  useEffect(() => {
    if (!open) return;
    setQuery('');
    listRef.current
      ?.querySelector<HTMLElement>(`[data-ref="${CSS.escape(currentRef)}"]`)
      ?.scrollIntoView({ block: 'center' });
  }, [open, currentRef]);

  // Esc closes (only while open).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className={styles.scrim} onClick={onClose}>
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
          <button className={styles.close} onClick={onClose} aria-label="Schließen">✕</button>
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
                  onClose();
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
  );
}

export default ChannelSwitcher;
