// EPG Channel List - Main view and search results rendering
// Zero API imports

import React from 'react';
import { useTranslation } from 'react-i18next';
import type { EpgEvent, EpgChannel } from '../types';
import type { PlaybackInfo, PlaybackSourceProfile } from '../../../client-ts';
import { isEventVisible } from '../epgModel';
import { EpgEventRow } from './EpgEventList';
import { Button } from '../../../components/ui';
import { requestAuthRequired } from '../../player/sessionEvents';
import { gatherPlaybackCapabilities, type CapabilitySnapshot } from '../../player/utils/playbackCapabilities';
import {
  buildPlaybackProfileHeaders,
  gatherPlaybackClientContext,
  resolvePlaybackRequestProfile,
  type PlaybackRequestProfile,
} from '../../player/utils/playbackRequestProfile';
import { resolveHostEnvironment } from '../../../lib/hostBridge';
import { getApiBaseUrl } from '../../../services/clientWrapper';
import { getStoredToken } from '../../../utils/tokenStorage';
import styles from '../EPG.module.css';

const CHANNEL_JUMP_TIMEOUT_MS = 900;
const CHANNEL_HOLD_START_DELAY_MS = 180;
const CHANNEL_HOLD_REPEAT_INTERVAL_MS = 90;
const CHANNEL_BADGE_INITIAL_PREFETCH = 12;
const CHANNEL_BADGE_FOCUS_BEHIND = 3;
const CHANNEL_BADGE_FOCUS_AHEAD = 8;

type ChannelPlaybackMode = 'direct_play' | 'direct_stream' | 'transcode' | 'deny';

type ChannelPlaybackBadge = {
  mode: ChannelPlaybackMode;
  label: string;
  detail: string | null;
  title: string;
};

const channelPlaybackBadgeCache = new Map<string, ChannelPlaybackBadge>();
const channelPlaybackBadgeInflight = new Map<string, Promise<ChannelPlaybackBadge | null>>();

function buildCapabilityCacheKey(capabilities: CapabilitySnapshot, requestProfile?: PlaybackRequestProfile): string {
  return JSON.stringify({
    deviceType: capabilities.deviceType || 'unknown',
    container: [...(capabilities.container || [])].sort(),
    videoCodecs: [...(capabilities.videoCodecs || [])].sort(),
    audioCodecs: [...(capabilities.audioCodecs || [])].sort(),
    hlsEngines: [...(capabilities.hlsEngines || [])].sort(),
    preferredHlsEngine: capabilities.preferredHlsEngine || '',
    maxVideo: capabilities.maxVideo || null,
    supportsRange: capabilities.supportsRange === true,
    supportsHls: capabilities.supportsHls === true,
    runtimeProbeUsed: capabilities.runtimeProbeUsed === true,
    clientFamilyFallback: capabilities.clientFamilyFallback || '',
    requestProfile: requestProfile || '',
  });
}

function formatResolutionLabel(
  resolution?: string | null,
  source?: PlaybackSourceProfile | null
): string | null {
  const sourceHeight = source?.height;
  const sourceWidth = source?.width;
  if (typeof sourceWidth === 'number' && typeof sourceHeight === 'number' && sourceWidth > 0 && sourceHeight > 0) {
    return `${sourceHeight}p`;
  }

  if (!resolution) {
    return null;
  }

  const match = resolution.match(/(\d+)\s*x\s*(\d+)/i);
  if (!match) {
    return resolution;
  }

  const height = Number.parseInt(match[2] || '', 10);
  return Number.isNaN(height) ? resolution : `${height}p`;
}

function buildChannelPlaybackDetail(channel: EpgChannel, info: PlaybackInfo): string | null {
  const source = info.decision?.trace?.source;
  const resolution = formatResolutionLabel(channel.resolution, source);
  const videoCodec = source?.videoCodec || info.videoCodec || channel.codec || null;
  const audioCodec = source?.audioCodec || info.audioCodec || null;
  const codecSummary = [videoCodec, audioCodec].filter(Boolean).join('/');
  const detail = [resolution, codecSummary || null].filter(Boolean).join(' · ');
  return detail || null;
}

function buildChannelPlaybackBadge(channel: EpgChannel, info: PlaybackInfo): ChannelPlaybackBadge | null {
  const rawMode = typeof info.decision?.mode === 'string'
    ? info.decision.mode
    : typeof info.mode === 'string'
      ? info.mode
      : null;

  if (rawMode !== 'direct_play' && rawMode !== 'direct_stream' && rawMode !== 'transcode' && rawMode !== 'deny') {
    return null;
  }

  const detail = buildChannelPlaybackDetail(channel, info);
  switch (rawMode) {
    case 'direct_play':
      return {
        mode: rawMode,
        label: 'Direct',
        detail,
        title: 'Runs on this device without remux or re-encode',
      };
    case 'direct_stream':
      return {
        mode: rawMode,
        label: 'Remux',
        detail,
        title: 'Runs on this device without re-encode, packaged as HLS',
      };
    case 'transcode':
      return {
        mode: rawMode,
        label: 'Encode',
        detail,
        title: 'Needs video or audio transcoding on this device',
      };
    case 'deny':
      return {
        mode: rawMode,
        label: 'Blocked',
        detail,
        title: 'Cannot be played on this device with the current constraints',
      };
  }
}

async function fetchChannelPlaybackBadge(
  apiBase: string,
  channel: EpgChannel,
  capabilities: CapabilitySnapshot,
  capabilityCacheKey: string,
  requestProfile?: PlaybackRequestProfile
): Promise<ChannelPlaybackBadge | null> {
  const serviceRef = channel.serviceRef || channel.id || '';
  if (!serviceRef) {
    return null;
  }

  const cacheKey = `${capabilityCacheKey}::${serviceRef}`;
  const cached = channelPlaybackBadgeCache.get(cacheKey);
  if (cached) {
    return cached;
  }

  const inflight = channelPlaybackBadgeInflight.get(cacheKey);
  if (inflight) {
    return inflight;
  }

  const request = (async () => {
    const authToken = getStoredToken().trim();
    const response = await fetch(`${apiBase}/live/stream-info`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}),
        ...buildPlaybackProfileHeaders(requestProfile),
      },
      body: JSON.stringify({
        serviceRef,
        capabilities,
      }),
    });

    let payload: PlaybackInfo | null = null;
    try {
      payload = await response.json() as PlaybackInfo;
    } catch {
      payload = null;
    }

    if (response.status === 401) {
      requestAuthRequired({ source: 'EPG.channelPlaybackBadge', status: 401 });
      return null;
    }

    if (!response.ok || !payload) {
      return null;
    }

    const badge = buildChannelPlaybackBadge(channel, payload);
    if (badge) {
      channelPlaybackBadgeCache.set(cacheKey, badge);
    }
    return badge;
  })();

  channelPlaybackBadgeInflight.set(cacheKey, request);
  try {
    return await request;
  } finally {
    channelPlaybackBadgeInflight.delete(cacheKey);
  }
}

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
  playbackBadge?: ChannelPlaybackBadge | null;
  isFavorite?: boolean;
  onPlay?: (channel: EpgChannel) => void;
  onToggleFavorite?: (serviceRef: string) => void;
}

function ChannelHeader({
  channel,
  channelIndex,
  displayName,
  playbackBadge,
  isFavorite = false,
  onPlay,
  onToggleFavorite,
}: ChannelHeaderProps) {
  const { t } = useTranslation();
  const [imageFailed, setImageFailed] = React.useState(false);
  const logo = channel?.logoUrl || channel?.logoUrl || channel?.logo;
  const isPlayable = Boolean(onPlay);
  const fallbackLabel = buildChannelFallback(displayName, channel);
  const favoriteServiceRef = channel.serviceRef || channel.id || '';

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
        {(channel?.group || playbackBadge) && (
          <div className={styles.channelAux}>
            {channel?.group && <div className={styles.channelGroup}>{channel.group}</div>}
            {playbackBadge && (
              <div className={styles.channelPlaybackMeta}>
                <span
                  className={[
                    styles.channelPlaybackBadge,
                    playbackBadge.mode === 'direct_play'
                      ? styles.channelPlaybackBadgeDirect
                      : playbackBadge.mode === 'direct_stream'
                        ? styles.channelPlaybackBadgeRemux
                        : playbackBadge.mode === 'transcode'
                          ? styles.channelPlaybackBadgeEncode
                          : styles.channelPlaybackBadgeBlocked,
                  ].join(' ')}
                  title={playbackBadge.title}
                >
                  {playbackBadge.label}
                </span>
                {playbackBadge.detail && (
                  <span className={styles.channelPlaybackDetail}>{playbackBadge.detail}</span>
                )}
              </div>
            )}
          </div>
        )}
      </div>
      {favoriteServiceRef && onToggleFavorite && (
        <button
          type="button"
          className={[styles.favoriteButton, isFavorite ? styles.favoriteButtonActive : null].filter(Boolean).join(' ')}
          aria-pressed={isFavorite}
          onClick={(event) => {
            event.stopPropagation();
            onToggleFavorite(favoriteServiceRef);
          }}
          title={isFavorite
            ? t('epg.removeFavorite', { defaultValue: 'Favorit entfernen' })
            : t('epg.addFavorite', { defaultValue: 'Zu Favoriten' })}
        >
          <span aria-hidden="true">{isFavorite ? '★' : '☆'}</span>
          <span className={styles.favoriteLabel}>
            {isFavorite
              ? t('epg.favoriteOn', { defaultValue: 'Favorit' })
              : t('epg.favoriteOff', { defaultValue: 'Merken' })}
          </span>
        </button>
      )}
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
  playbackBadge?: ChannelPlaybackBadge | null;
  isFavorite?: boolean;
  events: EpgEvent[];
  currentTime: number;
  timeRangeHours: number;
  isExpanded: boolean;
  onToggleExpand: () => void;
  onPlay?: (channel: EpgChannel) => void;
  onToggleFavorite?: (serviceRef: string) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

function ChannelCard({
  channelIndex,
  channel,
  playbackBadge,
  isFavorite,
  events,
  currentTime,
  timeRangeHours,
  isExpanded,
  onToggleExpand,
  onPlay,
  onToggleFavorite,
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
      <ChannelHeader
        channel={channel}
        channelIndex={channelIndex}
        displayName={displayName}
        playbackBadge={playbackBadge}
        isFavorite={isFavorite}
        onPlay={onPlay}
        onToggleFavorite={onToggleFavorite}
      />

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
  playbackBadge?: ChannelPlaybackBadge | null;
  isFavorite?: boolean;
  events: EpgEvent[];
  currentTime: number;
  isExpanded: boolean;
  onToggleExpand: () => void;
  onPlay?: (channel: EpgChannel) => void;
  onToggleFavorite?: (serviceRef: string) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

function SearchGroup({
  channel,
  playbackBadge,
  isFavorite,
  events,
  currentTime,
  isExpanded,
  onToggleExpand,
  onPlay,
  onToggleFavorite,
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
      <ChannelHeader
        channel={channel}
        displayName={displayName}
        playbackBadge={playbackBadge}
        isFavorite={isFavorite}
        onPlay={onPlay}
        onToggleFavorite={onToggleFavorite}
      />

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
  favoriteServiceRefs?: Set<string>;
  currentTime: number;
  timeRangeHours: number;
  expandedChannels: Set<string>;
  onToggleExpand: (serviceRef: string) => void;
  onPlay?: (channel: EpgChannel) => void;
  onToggleFavorite?: (serviceRef: string) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

export function EpgChannelList({
  mode,
  channels,
  eventsByServiceRef,
  favoriteServiceRefs,
  currentTime,
  timeRangeHours,
  expandedChannels,
  onToggleExpand,
  onPlay,
  onToggleFavorite,
  onRecord,
  isRecorded,
}: EpgChannelListProps) {
  const isTvHost = React.useMemo(() => resolveHostEnvironment().isTv, []);
  const apiBase = React.useMemo(() => getApiBaseUrl('/api/v3'), []);
  const listRef = React.useRef<HTMLDivElement | null>(null);
  const [channelJumpBuffer, setChannelJumpBuffer] = React.useState('');
  const [focusedChannelIndex, setFocusedChannelIndex] = React.useState(0);
  const [capabilitySnapshot, setCapabilitySnapshot] = React.useState<CapabilitySnapshot | null>(null);
  const [playbackBadges, setPlaybackBadges] = React.useState<Record<string, ChannelPlaybackBadge>>({});
  const channelJumpResetRef = React.useRef<number | null>(null);
  const holdKeyRef = React.useRef<string | null>(null);
  const holdRepeatCountRef = React.useRef(0);
  const holdStartTimeoutRef = React.useRef<number | null>(null);
  const holdRepeatTimeoutRef = React.useRef<number | null>(null);

  // Sort channels by number (LCN) then name
  const sortedChannels = React.useMemo(() => {
    return [...channels].sort((a, b) => {
      const aRef = a.serviceRef || a.id || '';
      const bRef = b.serviceRef || b.id || '';
      const aFavorite = favoriteServiceRefs?.has(aRef.toLowerCase()) ?? false;
      const bFavorite = favoriteServiceRefs?.has(bRef.toLowerCase()) ?? false;

      if (aFavorite !== bFavorite) {
        return aFavorite ? -1 : 1;
      }

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
  }, [channels, favoriteServiceRefs]);

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
      const favoriteA = favoriteServiceRefs?.has(refA.toLowerCase()) ?? false;
      const favoriteB = favoriteServiceRefs?.has(refB.toLowerCase()) ?? false;
      if (favoriteA !== favoriteB) {
        return favoriteA ? -1 : 1;
      }

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
  }, [channels, currentTime, eventsByServiceRef, favoriteServiceRefs]);

  const searchChannels = React.useMemo(() => {
    return searchGroups.map(([serviceRef]) =>
      channels.find((channel) => channel.serviceRef === serviceRef || channel.id === serviceRef) ||
      ({ serviceRef, id: serviceRef, name: serviceRef } as EpgChannel)
    );
  }, [channels, searchGroups]);

  const orderedDisplayChannels = mode === 'main' ? sortedChannels : searchChannels;
  const playbackRequestProfile = React.useMemo(
    () => (capabilitySnapshot
      ? resolvePlaybackRequestProfile(gatherPlaybackClientContext(), capabilitySnapshot, 'live')
      : undefined),
    [capabilitySnapshot]
  );
  const capabilityCacheKey = React.useMemo(
    () => (capabilitySnapshot ? buildCapabilityCacheKey(capabilitySnapshot, playbackRequestProfile) : null),
    [capabilitySnapshot, playbackRequestProfile]
  );

  React.useEffect(() => {
    let cancelled = false;

    gatherPlaybackCapabilities('live')
      .then((snapshot) => {
        if (!cancelled) {
          setCapabilitySnapshot(snapshot);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCapabilitySnapshot(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const primePlaybackBadges = React.useCallback((indices: number[]) => {
    if (!capabilitySnapshot || !capabilityCacheKey) {
      return;
    }

    const uniqueIndices = Array.from(new Set(indices))
      .filter((index) => index >= 0 && index < orderedDisplayChannels.length);

    uniqueIndices.forEach((index) => {
      const channel = orderedDisplayChannels[index];
      const serviceRef = channel?.serviceRef || channel?.id || '';
      if (!channel || !serviceRef) {
        return;
      }

      const cacheKey = `${capabilityCacheKey}::${serviceRef}`;
      const cached = channelPlaybackBadgeCache.get(cacheKey);
      if (cached) {
        setPlaybackBadges((current) =>
          current[serviceRef] === cached ? current : { ...current, [serviceRef]: cached }
        );
        return;
      }

      void fetchChannelPlaybackBadge(apiBase, channel, capabilitySnapshot, capabilityCacheKey, playbackRequestProfile)
        .then((badge) => {
          if (!badge) {
            return;
          }
          setPlaybackBadges((current) =>
            current[serviceRef] === badge ? current : { ...current, [serviceRef]: badge }
          );
        })
        .catch(() => {});
    });
  }, [apiBase, capabilityCacheKey, capabilitySnapshot, orderedDisplayChannels, playbackRequestProfile]);

  React.useEffect(() => {
    if (orderedDisplayChannels.length === 0) {
      return;
    }

    const indices = Array.from(
      { length: Math.min(CHANNEL_BADGE_INITIAL_PREFETCH, orderedDisplayChannels.length) },
      (_, index) => index
    );

    primePlaybackBadges(indices);
  }, [orderedDisplayChannels, primePlaybackBadges]);

  React.useEffect(() => {
    if (mode !== 'main') {
      return;
    }

    const indices: number[] = [];
    for (
      let index = Math.max(0, focusedChannelIndex - CHANNEL_BADGE_FOCUS_BEHIND);
      index <= Math.min(sortedChannels.length - 1, focusedChannelIndex + CHANNEL_BADGE_FOCUS_AHEAD);
      index += 1
    ) {
      indices.push(index);
    }

    primePlaybackBadges(indices);
  }, [focusedChannelIndex, mode, primePlaybackBadges, sortedChannels.length]);

  React.useEffect(() => {
    const root = listRef.current;
    if (!root) {
      return;
    }

    const handleFocusIn = (event: FocusEvent) => {
      const target = event.target as HTMLElement | null;
      const channelNode = target?.closest<HTMLElement>('[data-xg2g-channel-index]');
      const value = channelNode?.dataset.xg2gChannelIndex;
      if (!value) {
        return;
      }

      const nextIndex = Number.parseInt(value, 10);
      if (!Number.isNaN(nextIndex)) {
        setFocusedChannelIndex(nextIndex);
      }
    };

    root.addEventListener('focusin', handleFocusIn);
    return () => {
      root.removeEventListener('focusin', handleFocusIn);
    };
  }, []);

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
      setFocusedChannelIndex(safeIndex);
      const card = root.querySelector<HTMLElement>(`[data-xg2g-channel-index="${safeIndex}"]`);
      const target = card?.querySelector<HTMLElement>('[data-xg2g-channel-focus="true"]');
      if (!target) {
        return;
      }

      target.focus({ preventScroll: true });
      card?.scrollIntoView?.({ block: 'center', inline: 'nearest', behavior: 'auto' });
    };

    const resetHoldState = () => {
      if (holdStartTimeoutRef.current !== null) {
        window.clearTimeout(holdStartTimeoutRef.current);
        holdStartTimeoutRef.current = null;
      }
      if (holdRepeatTimeoutRef.current !== null) {
        window.clearTimeout(holdRepeatTimeoutRef.current);
        holdRepeatTimeoutRef.current = null;
      }
      holdKeyRef.current = null;
      holdRepeatCountRef.current = 0;
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

    const resolveHoldStep = (repeatCount: number): number => {
      if (repeatCount >= 10) {
        return 12;
      }
      if (repeatCount >= 6) {
        return 8;
      }
      if (repeatCount >= 3) {
        return 4;
      }
      return 2;
    };

    const moveFocusByStep = (key: 'ArrowDown' | 'ArrowUp', step: number) => {
      const currentIndex = findFocusedChannelIndex();
      const baseIndex = currentIndex >= 0 ? currentIndex : 0;
      const direction = key === 'ArrowDown' ? 1 : -1;
      focusChannelAt(baseIndex + (step * direction));
    };

    const startHoldLoop = (key: 'ArrowDown' | 'ArrowUp') => {
      if (holdStartTimeoutRef.current !== null) {
        window.clearTimeout(holdStartTimeoutRef.current);
      }
      if (holdRepeatTimeoutRef.current !== null) {
        window.clearTimeout(holdRepeatTimeoutRef.current);
      }

      const runRepeat = () => {
        if (holdKeyRef.current !== key) {
          return;
        }

        holdRepeatCountRef.current += 1;
        moveFocusByStep(key, resolveHoldStep(holdRepeatCountRef.current));
        holdRepeatTimeoutRef.current = window.setTimeout(runRepeat, CHANNEL_HOLD_REPEAT_INTERVAL_MS);
      };

      holdStartTimeoutRef.current = window.setTimeout(() => {
        holdStartTimeoutRef.current = null;
        runRepeat();
      }, CHANNEL_HOLD_START_DELAY_MS);
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

      switch (event.key) {
        case 'ArrowDown':
        case 'ArrowUp': {
          event.preventDefault();
          setChannelJumpBuffer('');
          const key = event.key as 'ArrowDown' | 'ArrowUp';
          if (holdKeyRef.current === key) {
            return;
          }
          resetHoldState();
          holdKeyRef.current = key;
          moveFocusByStep(key, 1);
          startHoldLoop(key);
          return;
        }
        case 'PageDown':
        case 'ChannelDown':
        case 'MediaChannelDown':
          resetHoldState();
          event.preventDefault();
          setChannelJumpBuffer('');
          focusChannelAt((activeIndex >= 0 ? activeIndex : 0) + 24);
          break;
        case 'PageUp':
        case 'ChannelUp':
        case 'MediaChannelUp':
          resetHoldState();
          event.preventDefault();
          setChannelJumpBuffer('');
          focusChannelAt((activeIndex >= 0 ? activeIndex : 0) - 24);
          break;
        case 'Home':
          resetHoldState();
          event.preventDefault();
          setChannelJumpBuffer('');
          focusChannelAt(0);
          break;
        case 'End':
          resetHoldState();
          event.preventDefault();
          setChannelJumpBuffer('');
          focusChannelAt(sortedChannels.length - 1);
          break;
        default:
          if (event.key !== 'Tab') {
            resetHoldState();
          }
          return;
      }
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
      if (holdStartTimeoutRef.current !== null) {
        window.clearTimeout(holdStartTimeoutRef.current);
      }
      if (holdRepeatTimeoutRef.current !== null) {
        window.clearTimeout(holdRepeatTimeoutRef.current);
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
              playbackBadge={playbackBadges[ref] || null}
              isFavorite={favoriteServiceRefs?.has(ref.toLowerCase()) ?? false}
              events={events}
              currentTime={currentTime}
              timeRangeHours={timeRangeHours}
              isExpanded={isExpanded}
              onToggleExpand={() => onToggleExpand(ref)}
              onPlay={onPlay}
              onToggleFavorite={onToggleFavorite}
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
            playbackBadge={playbackBadges[serviceRef] || null}
            isFavorite={favoriteServiceRefs?.has(serviceRef.toLowerCase()) ?? false}
            events={events}
            currentTime={currentTime}
            isExpanded={isExpanded}
            onToggleExpand={() => onToggleExpand(serviceRef)}
            onPlay={onPlay}
            onToggleFavorite={onToggleFavorite}
            onRecord={onRecord}
            isRecorded={isRecorded}
          />
        );
      })}
    </div>
  );
}
