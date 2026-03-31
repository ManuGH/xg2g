// EPG Channel List - Main view and search results rendering
// Zero API imports

import React from 'react';
import { useTranslation } from 'react-i18next';
import type { EpgEvent, EpgChannel } from '../types';
import { isEventVisible } from '../epgModel';
import { EpgEventRow } from './EpgEventList';
import { Button } from '../../../components/ui';
import { resolveHostEnvironment } from '../../../lib/hostBridge';
import styles from '../EPG.module.css';

const CHANNEL_JUMP_TIMEOUT_MS = 900;

// Helper: Get localized day string "Do 28.12."
// Returns null if date matches "today" (relative to currentTime, but user said "same day no date")
// Actually, standard is: If event starts on a different day than previous event (or today), show "Do 28.12."
function getDateLabel(ts: number): string {
  const d = new Date(ts * 1000);
  return d.toLocaleDateString('de-DE', {
    weekday: 'short',
    day: '2-digit',
    month: '2-digit',
  });
}

function getTodayLabel(): string {
  return new Date().toLocaleDateString('de-DE', {
    weekday: 'short',
    day: '2-digit',
    month: '2-digit',
  });
}

function buildChannelFallback(displayName: string, channel: EpgChannel): string {
  if (channel.number) {
    return channel.number.slice(0, 3);
  }

  const source = (channel.name || channel.id || displayName)
    .replace(/^\d+\s*[·.-]?\s*/, '')
    .trim();
  const letters = source
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() || '')
    .join('');

  return letters || 'TV';
}

// Channel Header Component
interface ChannelHeaderProps {
  channel: EpgChannel;
  channelIndex?: number;
  displayName: string;
  onPlay?: (channel: EpgChannel) => void;
}

function ChannelHeader({ channel, channelIndex, displayName, onPlay }: ChannelHeaderProps) {
  const { t } = useTranslation();
  const [imageFailed, setImageFailed] = React.useState(false);
  const logo = channel?.logoUrl || channel?.logoUrl || channel?.logo;
  const isPlayable = Boolean(onPlay);
  const fallbackLabel = buildChannelFallback(displayName, channel);

  const triggerPlay = (): void => {
    if (onPlay) {
      onPlay(channel);
    }
  };

  return (
    <div
      className={[styles.channel, isPlayable ? styles.channelPlayable : null].filter(Boolean).join(' ')}
      onClick={isPlayable ? triggerPlay : undefined}
      onKeyDown={isPlayable ? (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          triggerPlay();
        }
      } : undefined}
      role={isPlayable ? 'button' : undefined}
      tabIndex={isPlayable ? 0 : undefined}
      aria-label={isPlayable ? t('epg.playStream') : undefined}
      data-xg2g-channel-focus="true"
      data-xg2g-channel-index={typeof channelIndex === 'number' ? channelIndex : undefined}
      data-xg2g-channel-number={channel.number || undefined}
    >
      <div className={styles.logo}>
        {logo && !imageFailed ? (
          <img
            src={logo}
            alt={displayName}
            onError={() => setImageFailed(true)}
          />
        ) : (
          <span className={styles.logoFallback}>{fallbackLabel}</span>
        )}
      </div>
      <div className={styles.channelMeta}>
        <div className={styles.channelName}>{displayName}</div>
        {channel?.group && <div className={styles.channelGroup}>{channel.group}</div>}
      </div>
      {onPlay && (
        <Button
          className={styles.play}
          onClick={(e) => {
            e.stopPropagation();
            triggerPlay();
          }}
          title={t('epg.playStream')}
          size="sm"
          active
        >
          <span aria-hidden="true">▶</span>
          <span className={styles.playLabel}>{t('epg.playCta', { defaultValue: 'Watch' })}</span>
        </Button>
      )}
    </div>
  );
}

// Single Channel Card (Main View)
interface ChannelCardProps {
  channelIndex: number;
  channel: EpgChannel;
  events: EpgEvent[];
  currentTime: number;
  timeRangeHours: number;
  isExpanded: boolean;
  onToggleExpand: () => void;
  onPlay?: (channel: EpgChannel) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

function ChannelCard({
  channelIndex,
  channel,
  events,
  currentTime,
  timeRangeHours,
  isExpanded,
  onToggleExpand,
  onPlay,
  onRecord,
  isRecorded,
}: ChannelCardProps) {
  const { t } = useTranslation();
  const displayName = channel
    ? `${channel.number ? `${channel.number} · ` : ''}${channel.name || channel.id || 'Unknown'}`
    : 'Unknown';

  const now = currentTime;
  const to = now + timeRangeHours * 3600;

  // CTO Predicate: overlapsNow || (start < to)
  const visibleEvents = events.filter(e => isEventVisible(e, now, to));

  const current = visibleEvents.find((e) => now >= e.start && now < e.end) || visibleEvents[0];
  const others = visibleEvents.filter((e) => e !== current);
  const todayLabel = getTodayLabel();

  return (
    <div
      className={styles.card}
      data-xg2g-channel-index={channelIndex}
      data-xg2g-channel-number={channel.number || undefined}
    >
      <ChannelHeader channel={channel} channelIndex={channelIndex} displayName={displayName} onPlay={onPlay} />

      <div className={styles.programmes}>
        {current && (
          <EpgEventRow
            key={`${current.serviceRef}-${current.start}-current`}
            event={current}
            currentTime={currentTime}
            highlight
            onRecord={onRecord}
            isRecorded={isRecorded ? isRecorded(current) : false}
          // Logic: Current event usually implies "Now/Today", so no date header unless we want to carry over
          // But usually current is obvious. User said "Today no date".
          />
        )}

        {others.length > 0 && (
          <div className={styles.dropdown}>
            <button className={styles.toggle} onClick={onToggleExpand}>
              {isExpanded
                ? t('epg.hideOtherShows')
                : t('epg.moreShows', { count: others.length })}
            </button>
            {isExpanded && (
              <div className={styles.programmesNoncurrent}>
                {others.map((event, index) => {
                  const eventDate = getDateLabel(event.start);
                  // Calculate if we need a header
                  let showHeader = false;

                  // 1. If it's the first item in "others"
                  if (index === 0) {
                    // Show if NOT today
                    if (eventDate !== todayLabel) showHeader = true;
                  } else {
                    // Compare with previous
                    const prev = others[index - 1];
                    if (prev) {
                      const prevDate = getDateLabel(prev.start);
                      if (eventDate !== prevDate) {
                        // Changed day
                        // Show if NOT today (unlikely to go back to today, but safe check)
                        if (eventDate !== todayLabel) showHeader = true;
                      }
                    }
                  }

                  return (
                    <EpgEventRow
                      key={`${event.serviceRef}-${event.start}`}
                      event={event}
                      currentTime={currentTime}
                      highlight={currentTime >= event.start && currentTime < event.end}
                      onRecord={onRecord}
                      isRecorded={isRecorded ? isRecorded(event) : false}
                      dateLabel={showHeader ? eventDate : undefined}
                    />
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// Search Results Group
interface SearchGroupProps {
  channel: EpgChannel;
  events: EpgEvent[];
  currentTime: number;
  isExpanded: boolean;
  onToggleExpand: () => void;
  onPlay?: (channel: EpgChannel) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

function SearchGroup({
  channel,
  events,
  currentTime,
  isExpanded,
  onToggleExpand,
  onPlay,
  onRecord,
  isRecorded,
}: SearchGroupProps) {
  const { t } = useTranslation();
  const serviceRef = channel.serviceRef || channel.id || '';
  const displayName = channel
    ? `${channel.number ? `${channel.number} · ` : ''}${channel.name || channel.id || serviceRef}`
    : serviceRef || 'Unknown';

  const top2 = events.slice(0, 2);
  const rest = events.slice(2);
  const todayLabel = getTodayLabel();

  return (
    <div className={styles.searchGroup}>
      <ChannelHeader channel={channel} displayName={displayName} onPlay={onPlay} />

      <div className={styles.programmes}>
        {top2.map((event, index) => {
          const eventDate = getDateLabel(event.start);
          let showHeader = false;
          if (index === 0) {
            if (eventDate !== todayLabel) showHeader = true;
          } else {
            const prev = top2[index - 1];
            if (prev) {
              const prevDate = getDateLabel(prev.start);
              if (eventDate !== prevDate && eventDate !== todayLabel) showHeader = true;
            }
          }
          return (
            <EpgEventRow
              key={`${event.serviceRef}-${event.start}`}
              event={event}
              currentTime={currentTime}
              highlight={currentTime >= event.start && currentTime < event.end}
              onRecord={onRecord}
              isRecorded={isRecorded ? isRecorded(event) : false}
              dateLabel={showHeader ? eventDate : undefined}
            />
          )
        })}

        {rest.length > 0 && (
          <div className={styles.dropdown}>
            <button className={styles.toggle} onClick={onToggleExpand}>
              {isExpanded ? t('epg.showLess') : t('epg.moreShows', { count: rest.length })}
            </button>
            {isExpanded && (
              <div className={styles.programmesNoncurrent}>
                {rest.map((event, index) => {
                  const eventDate = getDateLabel(event.start);
                  let showHeader = false;

                  if (index === 0) {
                    // check against last of top2
                    const lastTop = top2.length > 0 ? top2[top2.length - 1] : undefined;
                    if (lastTop) {
                      const lastTopDate = getDateLabel(lastTop.start);
                      if (eventDate !== lastTopDate && eventDate !== todayLabel) showHeader = true;
                    }
                  } else {
                    const prev = rest[index - 1];
                    if (prev) {
                      const prevDate = getDateLabel(prev.start);
                      if (eventDate !== prevDate && eventDate !== todayLabel) showHeader = true;
                    }
                  }

                  return (
                    <EpgEventRow
                      key={`${event.serviceRef}-${event.start}`}
                      event={event}
                      currentTime={currentTime}
                      highlight={currentTime >= event.start && currentTime < event.end}
                      onRecord={onRecord}
                      isRecorded={isRecorded ? isRecorded(event) : false}
                      dateLabel={showHeader ? eventDate : undefined}
                    />
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// Main Export: Channel List (both modes)
export interface EpgChannelListProps {
  mode: 'main' | 'search';
  channels: EpgChannel[];
  eventsByServiceRef: Map<string, EpgEvent[]>;
  currentTime: number;
  timeRangeHours: number;
  expandedChannels: Set<string>;
  onToggleExpand: (serviceRef: string) => void;
  onPlay?: (channel: EpgChannel) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

export function EpgChannelList({
  mode,
  channels,
  eventsByServiceRef,
  currentTime,
  timeRangeHours,
  expandedChannels,
  onToggleExpand,
  onPlay,
  onRecord,
  isRecorded,
}: EpgChannelListProps) {
  const isTvHost = React.useMemo(() => resolveHostEnvironment().isTv, []);
  const listRef = React.useRef<HTMLDivElement | null>(null);
  const [channelJumpBuffer, setChannelJumpBuffer] = React.useState('');
  const channelJumpResetRef = React.useRef<number | null>(null);
  const holdKeyRef = React.useRef<string | null>(null);
  const holdRepeatCountRef = React.useRef(0);
  const holdLastTsRef = React.useRef(0);

  // Sort channels by number (LCN) then name
  const sortedChannels = React.useMemo(() => {
    return [...channels].sort((a, b) => {
      const aNum = parseInt(a.number || '', 10);
      const bNum = parseInt(b.number || '', 10);
      const aNumValid = !Number.isNaN(aNum);
      const bNumValid = !Number.isNaN(bNum);

      if (aNumValid && bNumValid && aNum !== bNum) {
        return aNum - bNum;
      }
      if (aNumValid && !bNumValid) return -1;
      if (!aNumValid && bNumValid) return 1;

      const aName = a.name || a.id || '';
      const bName = b.name || b.id || '';
      return aName.localeCompare(bName, undefined, { numeric: true, sensitivity: 'base' });
    });
  }, [channels]);

  const channelNumberIndex = React.useMemo(() => {
    return sortedChannels.reduce<Map<string, number>>((acc, channel, index) => {
      const normalizedNumber = (channel.number || '').replace(/\D/g, '');
      if (normalizedNumber) {
        acc.set(normalizedNumber, index);
      }
      return acc;
    }, new Map());
  }, [sortedChannels]);

  // Search mode: group events by channel, show top 2 + expandable rest.
  const searchGroups = React.useMemo(() => {
    const groups: Array<[string, EpgEvent[]]> = [];

    for (const [serviceRef, events] of eventsByServiceRef.entries()) {
      if (events.length === 0) continue;

      // Sort events: NOW first, then chronological.
      const sorted = [...events].sort((a, b) => {
        const aNow = a.start <= currentTime && a.end > currentTime ? 0 : 1;
        const bNow = b.start <= currentTime && b.end > currentTime ? 0 : 1;
        if (aNow !== bNow) return aNow - bNow;
        return a.start - b.start;
      });

      groups.push([serviceRef, sorted]);
    }

    // Sort groups by channel number/name.
    groups.sort(([refA], [refB]) => {
      const chA = channels.find((c) => c.serviceRef === refA || c.id === refA);
      const chB = channels.find((c) => c.serviceRef === refB || c.id === refB);
      const numA = parseInt(chA?.number || '', 10);
      const numB = parseInt(chB?.number || '', 10);
      const validA = !Number.isNaN(numA);
      const validB = !Number.isNaN(numB);

      if (validA && validB && numA !== numB) return numA - numB;
      if (validA && !validB) return -1;
      if (!validA && validB) return 1;

      const nameA = chA?.name || refA || '';
      const nameB = chB?.name || refB || '';
      return nameA.localeCompare(nameB, undefined, { numeric: true, sensitivity: 'base' });
    });

    return groups;
  }, [eventsByServiceRef, currentTime, channels]);

  React.useEffect(() => {
    if (!isTvHost || mode !== 'main') {
      return;
    }

    const findFocusedChannelIndex = (): number => {
      const activeElement = document.activeElement as HTMLElement | null;
      const channelNode = activeElement?.closest<HTMLElement>('[data-xg2g-channel-index]');
      const value = channelNode?.dataset.xg2gChannelIndex;
      return value ? Number.parseInt(value, 10) : -1;
    };

    const focusChannelAt = (index: number): void => {
      const root = listRef.current;
      if (!root || sortedChannels.length === 0) {
        return;
      }

      const safeIndex = Math.max(0, Math.min(sortedChannels.length - 1, index));
      const card = root.querySelector<HTMLElement>(`[data-xg2g-channel-index="${safeIndex}"]`);
      const target = card?.querySelector<HTMLElement>('[data-xg2g-channel-focus="true"]');
      if (!target) {
        return;
      }

      target.focus({ preventScroll: true });
      card?.scrollIntoView?.({ block: 'center', inline: 'nearest', behavior: 'auto' });
    };

    const resetHoldState = () => {
      holdKeyRef.current = null;
      holdRepeatCountRef.current = 0;
      holdLastTsRef.current = 0;
    };

    const scheduleChannelJumpReset = () => {
      if (channelJumpResetRef.current !== null) {
        window.clearTimeout(channelJumpResetRef.current);
      }
      channelJumpResetRef.current = window.setTimeout(() => {
        setChannelJumpBuffer('');
      }, CHANNEL_JUMP_TIMEOUT_MS);
    };

    const focusChannelByNumber = (buffer: string) => {
      if (!buffer) {
        return;
      }

      const exactMatch = channelNumberIndex.get(buffer);
      if (typeof exactMatch === 'number') {
        focusChannelAt(exactMatch);
        return;
      }

      const prefixMatch = sortedChannels.findIndex((channel) =>
        (channel.number || '').replace(/\D/g, '').startsWith(buffer)
      );
      if (prefixMatch >= 0) {
        focusChannelAt(prefixMatch);
      }
    };

    const resolveHoldStep = (key: 'ArrowDown' | 'ArrowUp', event: KeyboardEvent): number => {
      const now = performance.now();
      const isContinuousHold =
        holdKeyRef.current === key &&
        (event.repeat || now - holdLastTsRef.current < 420);

      if (!isContinuousHold) {
        holdKeyRef.current = key;
        holdRepeatCountRef.current = 0;
        holdLastTsRef.current = now;
        return 1;
      }

      holdRepeatCountRef.current += 1;
      holdLastTsRef.current = now;

      if (holdRepeatCountRef.current >= 10) {
        return 48;
      }
      if (holdRepeatCountRef.current >= 6) {
        return 24;
      }
      if (holdRepeatCountRef.current >= 3) {
        return 12;
      }
      return 6;
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tagName = target?.tagName;
      const isTypingTarget = Boolean(
        target?.isContentEditable ||
        tagName === 'INPUT' ||
        tagName === 'TEXTAREA' ||
        tagName === 'SELECT'
      );
      if (isTypingTarget) {
        return;
      }

      const activeIndex = findFocusedChannelIndex();
      const isInsideList = activeIndex >= 0;
      const allowGlobalGuideNav = isInsideList || target === document.body;
      if (!allowGlobalGuideNav) {
        return;
      }

      if (/^\d$/.test(event.key)) {
        event.preventDefault();
        resetHoldState();
        setChannelJumpBuffer((current) => {
          const next = `${current}${event.key}`.slice(-4);
          focusChannelByNumber(next);
          return next;
        });
        scheduleChannelJumpReset();
        return;
      }

      let nextIndex = activeIndex >= 0 ? activeIndex : 0;

      switch (event.key) {
        case 'ArrowDown':
          nextIndex += resolveHoldStep('ArrowDown', event);
          break;
        case 'ArrowUp':
          nextIndex -= resolveHoldStep('ArrowUp', event);
          break;
        case 'PageDown':
        case 'ChannelDown':
        case 'MediaChannelDown':
          resetHoldState();
          nextIndex += 24;
          break;
        case 'PageUp':
        case 'ChannelUp':
        case 'MediaChannelUp':
          resetHoldState();
          nextIndex -= 24;
          break;
        case 'Home':
          resetHoldState();
          nextIndex = 0;
          break;
        case 'End':
          resetHoldState();
          nextIndex = sortedChannels.length - 1;
          break;
        default:
          if (event.key !== 'Tab') {
            resetHoldState();
          }
          return;
      }

      event.preventDefault();
      setChannelJumpBuffer('');
      focusChannelAt(nextIndex);
    };

    const handleKeyUp = (event: KeyboardEvent) => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        resetHoldState();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    return () => {
      if (channelJumpResetRef.current !== null) {
        window.clearTimeout(channelJumpResetRef.current);
      }
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [channelNumberIndex, isTvHost, mode, sortedChannels]);

  if (mode === 'main') {
    return (
      <div ref={listRef}>
        {isTvHost && channelJumpBuffer ? (
          <div className={styles.channelJumpHud} aria-live="polite">
            CH {channelJumpBuffer}
          </div>
        ) : null}
        {sortedChannels.map((channel, channelIndex) => {
          // Use channel.serviceRef to match EPG events (XMLTV channel IDs)
          const ref = channel.serviceRef || channel.id || '';
          const events = eventsByServiceRef.get(ref) || [];
          const isExpanded = expandedChannels.has(ref);

          return (
            <ChannelCard
              key={ref}
              channelIndex={channelIndex}
              channel={channel}
              events={events}
              currentTime={currentTime}
              timeRangeHours={timeRangeHours}
              isExpanded={isExpanded}
              onToggleExpand={() => onToggleExpand(ref)}
              onPlay={onPlay}
              onRecord={onRecord}
              isRecorded={isRecorded}
            />
          );
        })}
      </div>
    );
  }

  return (
    <div ref={listRef}>
      {searchGroups.map(([serviceRef, events]) => {
        const channel =
          channels.find((c) => c.serviceRef === serviceRef || c.id === serviceRef) ||
          ({ serviceRef: serviceRef, id: serviceRef, name: serviceRef } as EpgChannel);
        const isExpanded = expandedChannels.has(serviceRef);

        return (
          <SearchGroup
            key={serviceRef}
            channel={channel}
            events={events}
            currentTime={currentTime}
            isExpanded={isExpanded}
            onToggleExpand={() => onToggleExpand(serviceRef)}
            onPlay={onPlay}
            onRecord={onRecord}
            isRecorded={isRecorded}
          />
        );
      })}
    </div>
  );
}
