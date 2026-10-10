import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { postServicesNowNext, type Service } from '../../../client-ts';
import { getStoredToken } from '../../../utils/tokenStorage';
import { debugWarn } from '../../../utils/logging';
import styles from './ChannelSwitcher.module.css';

interface ChannelSwitcherProps {
  channels: Service[];
  current?: Service;
  onSwitch: (channel: Service) => void;
  open: boolean;
  onClose: () => void;
  token?: string | null;
  bouquets?: { name: string; services?: number }[];
  selectedBouquet?: string;
  onSelectBouquet?: (bouquet: string) => void;
}

const refOf = (c?: Service): string => c?.serviceRef?.trim() || c?.id?.trim() || '';
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
export function ChannelSwitcher({
  channels,
  current,
  onSwitch,
  open,
  onClose,
  token,
  bouquets,
  selectedBouquet,
  onSelectBouquet,
}: ChannelSwitcherProps) {
  const [query, setQuery] = useState('');
  const [pendingBouquet, setPendingBouquet] = useState<string | null>(null);
  const [nowNextMap, setNowNextMap] = useState<Record<string, string>>({});
  const listRef = useRef<HTMLDivElement>(null);
  const bouquetBarRef = useRef<HTMLDivElement>(null);
  const currentRef = refOf(current);
  const { t } = useTranslation();

  useEffect(() => {
    setPendingBouquet(null);
  }, [selectedBouquet, channels]);

  useEffect(() => {
    if (!open || channels.length === 0) return;
    let cancelled = false;

    const fetchNowNext = async () => {
      try {
        const authToken = (token || getStoredToken()).trim();
        const serviceRefs = channels
          .map((c) => refOf(c))
          .filter((ref) => ref.length > 0);
        if (serviceRefs.length === 0) return;

        const result = await postServicesNowNext({
          headers: { ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}) },
          body: { services: serviceRefs },
        });
        if (cancelled) return;

        const items = result.data?.items ?? [];
        const nextMap: Record<string, string> = {};
        for (const item of items) {
          if (item.serviceRef && item.now?.title) {
            nextMap[item.serviceRef] = item.now.title.trim();
          }
        }
        setNowNextMap(nextMap);
      } catch (err) {
        if (cancelled) return;
        debugWarn('ChannelSwitcher now/next fetch failed', err);
      }
    };

    void fetchNowNext();
    return () => {
      cancelled = true;
    };
  }, [open, channels, token]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter((c) => {
      const ref = refOf(c);
      const name = (c.name ?? '').toLowerCase();
      const num = String(c.number ?? '');
      const now = (nowNextMap[ref] ?? '').toLowerCase();
      return name.includes(q) || num.includes(q) || now.includes(q);
    });
  }, [channels, query, nowNextMap]);

  // Reset search when the panel closes, so the initial render after opening
  // already contains all channels and the DOM elements are available for scrolling.
  useEffect(() => {
    if (!open) {
      setQuery('');
      setPendingBouquet(null);
      return;
    }
    if (filtered.length === 0) return;
    const raf = requestAnimationFrame(() => {
      const activeRow = currentRef
        ? listRef.current?.querySelector<HTMLElement>(`[data-ref="${CSS.escape(currentRef)}"]`)
        : null;
      const targetRow = activeRow ?? listRef.current?.querySelector<HTMLElement>('button[data-ref]');
      targetRow?.scrollIntoView?.({ block: 'center' });
      targetRow?.focus?.({ preventScroll: true });
    });
    return () => cancelAnimationFrame(raf);
  }, [open, currentRef, filtered.length]);

  useEffect(() => {
    if (!open || !selectedBouquet) return;
    const raf = requestAnimationFrame(() => {
      bouquetBarRef.current
        ?.querySelector<HTMLElement>(`[data-bouquet="${CSS.escape(selectedBouquet)}"]`)
        ?.scrollIntoView?.({ block: 'nearest', inline: 'center' });
    });
    return () => cancelAnimationFrame(raf);
  }, [open, selectedBouquet]);

  // Esc closes (only while open) and ArrowUp/ArrowDown navigate channel rows.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        const rows = Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>('button[data-ref]') ?? []);
        if (rows.length === 0) return;
        const active = document.activeElement as HTMLButtonElement | null;
        const idx = active ? rows.indexOf(active) : -1;
        e.preventDefault();
        e.stopPropagation();
        const nextIdx =
          idx === -1
            ? 0
            : e.key === 'ArrowDown'
              ? Math.min(rows.length - 1, idx + 1)
              : Math.max(0, idx - 1);
        rows[nextIdx]?.focus();
        rows[nextIdx]?.scrollIntoView?.({ block: 'nearest' });
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, onClose]);

  if (!open) return null;

  const showBouquets = Boolean(bouquets && bouquets.length > 1 && onSelectBouquet);

  return (
    <div className={styles.scrim} onClick={onClose}>
      <aside className={styles.sheet} onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t('player.switchChannel', { defaultValue: 'Sender wechseln' })}>
        <div className={styles.header}>
          <input
            className={styles.search}
            placeholder={t('player.searchChannel', { defaultValue: 'Sender suchen…' })}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label={t('player.searchChannel', { defaultValue: 'Sender suchen' })}
          />
          <button className={styles.close} onClick={onClose} aria-label={t('common.close', { defaultValue: 'Schließen' })}>✕</button>
        </div>
        {showBouquets ? (
          <div
            ref={bouquetBarRef}
            className={styles.bouquetBar}
            role="tablist"
            aria-label={t('player.selectBouquet', { defaultValue: 'Bouquet wählen' })}
          >
            {bouquets!.map((b) => {
              const isSelected = b.name === selectedBouquet;
              const isPending = pendingBouquet === b.name;
              return (
                <button
                  key={b.name}
                  type="button"
                  role="tab"
                  aria-selected={isSelected}
                  aria-busy={isPending}
                  data-bouquet={b.name}
                  className={`${styles.bouquetPill} ${isSelected || isPending ? styles.bouquetPillActive : ''}`}
                  onClick={() => {
                    if (!isSelected && !isPending) {
                      setPendingBouquet(b.name);
                      onSelectBouquet!(b.name);
                    }
                  }}
                >
                  {b.name}
                </button>
              );
            })}
          </div>
        ) : null}
        <div className={styles.list} ref={listRef} aria-busy={Boolean(pendingBouquet && pendingBouquet !== selectedBouquet)}>
          {filtered.map((c, idx) => {
            const isBouquetLoading = Boolean(pendingBouquet && pendingBouquet !== selectedBouquet);
            const ref = refOf(c);
            const active = ref.length > 0 && ref === currentRef;
            const nowTitle = nowNextMap[ref];
            return (
              <button
                key={ref || `${c.name ?? 'ch'}-${c.number ?? idx}`}
                data-ref={ref}
                disabled={isBouquetLoading}
                aria-disabled={isBouquetLoading}
                className={`${styles.row} ${active ? styles.active : ''} ${isBouquetLoading ? styles.rowUnavailable : ''}`}
                onClick={() => {
                  if (isBouquetLoading) return;
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
                <div className={styles.channelMeta}>
                  <div className={styles.channelNameRow}>
                    <span className={styles.name}>{c.name ?? ref}</span>
                  </div>
                  {nowTitle && (
                    <span className={styles.nowTitle}>{nowTitle}</span>
                  )}
                </div>
                {active ? <span className={styles.live}>● live</span> : null}
              </button>
            );
          })}
          {filtered.length === 0 ? <div className={styles.empty}>{t('player.noChannelFound', { defaultValue: 'Kein Sender gefunden' })}</div> : null}
        </div>
      </aside>
    </div>
  );
}

export default ChannelSwitcher;
