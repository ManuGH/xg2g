// EPG Channel List - Main view and search results rendering
// Zero API imports

import React from 'react';
import type { EpgEvent, EpgChannel } from '../types';
import { EpgEventRow } from './EpgEventList';

// Channel Header Component
interface ChannelHeaderProps {
  channel: EpgChannel;
  displayName: string;
  onPlay?: (channel: EpgChannel) => void;
}

function ChannelHeader({ channel, displayName, onPlay }: ChannelHeaderProps) {
  const logo = channel?.logo_url || channel?.logoUrl || channel?.logo;

  return (
    <div className="epg-channel">
      <div className="epg-logo">
        {logo ? (
          <img
            src={logo}
            alt={displayName}
            onError={(e) => {
              e.currentTarget.style.display = 'none';
              const parent = e.currentTarget.parentNode as HTMLElement;
              if (parent) parent.innerHTML = '<span>🎬</span>';
            }}
          />
        ) : (
          <span>🎬</span>
        )}
      </div>
      <div className="epg-channel-meta">
        <div className="epg-channel-name">{displayName}</div>
        {channel?.group && <div className="epg-channel-group">{channel.group}</div>}
      </div>
      {onPlay && (
        <button
          className="btn-play header-play"
          onClick={(e) => {
            e.stopPropagation();
            onPlay(channel);
          }}
          title="Play Stream"
        >
          <span>▶</span> Play
        </button>
      )}
    </div>
  );
}

// Single Channel Card (Main View)
interface ChannelCardProps {
  channel: EpgChannel;
  events: EpgEvent[];
  currentTime: number;
  isExpanded: boolean;
  onToggleExpand: () => void;
  onPlay?: (channel: EpgChannel) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

function ChannelCard({
  channel,
  events,
  currentTime,
  isExpanded,
  onToggleExpand,
  onPlay,
  onRecord,
  isRecorded,
}: ChannelCardProps) {
  const displayName = channel
    ? `${channel.number ? `${channel.number} · ` : ''}${channel.name || channel.id || 'Unknown'}`
    : 'Unknown';

  const current = events.find((e) => currentTime >= e.start && currentTime < e.end) || events[0];
  const others = events.filter((e) => e !== current);

  return (
    <div className="epg-card">
      <ChannelHeader channel={channel} displayName={displayName} onPlay={onPlay} />

      <div className="epg-programmes">
        {current && (
          <EpgEventRow
            key={`${current.service_ref}-${current.start}-current`}
            event={current}
            currentTime={currentTime}
            highlight
            onRecord={onRecord}
            isRecorded={isRecorded ? isRecorded(current) : false}
          />
        )}

        {others.length > 0 && (
          <div className="epg-dropdown">
            <button className="epg-toggle" onClick={onToggleExpand}>
              {isExpanded
                ? 'Andere Sendungen ausblenden'
                : `Weitere Sendungen (${others.length})`}
            </button>
            {isExpanded && (
              <div className="epg-programmes-noncurrent">
                {others.map((event) => (
                  <EpgEventRow
                    key={`${event.service_ref}-${event.start}`}
                    event={event}
                    currentTime={currentTime}
                    highlight={currentTime >= event.start && currentTime < event.end}
                    onRecord={onRecord}
                    isRecorded={isRecorded ? isRecorded(event) : false}
                  />
                ))}
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
  const serviceRef = channel.service_ref || channel.id || '';
  const displayName = channel
    ? `${channel.number ? `${channel.number} · ` : ''}${channel.name || channel.id || serviceRef}`
    : serviceRef || 'Unknown';

  const top2 = events.slice(0, 2);
  const rest = events.slice(2);

  return (
    <div className="epg-search-group">
      <ChannelHeader channel={channel} displayName={displayName} onPlay={onPlay} />

      <div className="epg-programmes">
        {top2.map((event) => (
          <EpgEventRow
            key={`${event.service_ref}-${event.start}`}
            event={event}
            currentTime={currentTime}
            highlight={currentTime >= event.start && currentTime < event.end}
            onRecord={onRecord}
            isRecorded={isRecorded ? isRecorded(event) : false}
          />
        ))}

        {rest.length > 0 && (
          <div className="epg-dropdown">
            <button className="epg-toggle" onClick={onToggleExpand}>
              {isExpanded ? 'Weniger anzeigen' : `Weitere Sendungen (${rest.length})`}
            </button>
            {isExpanded && (
              <div className="epg-programmes-noncurrent">
                {rest.map((event) => (
                  <EpgEventRow
                    key={`${event.service_ref}-${event.start}`}
                    event={event}
                    currentTime={currentTime}
                    highlight={currentTime >= event.start && currentTime < event.end}
                    onRecord={onRecord}
                    isRecorded={isRecorded ? isRecorded(event) : false}
                  />
                ))}
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
  expandedChannels,
  onToggleExpand,
  onPlay,
  onRecord,
  isRecorded,
}: EpgChannelListProps) {
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

  if (mode === 'main') {
    return (
      <>
        {sortedChannels.map((channel) => {
          const ref = channel.service_ref || channel.id || '';
          const events = eventsByServiceRef.get(ref) || [];
          const isExpanded = expandedChannels.has(ref);

          return (
            <ChannelCard
              key={ref}
              channel={channel}
              events={events}
              currentTime={currentTime}
              isExpanded={isExpanded}
              onToggleExpand={() => onToggleExpand(ref)}
              onPlay={onPlay}
              onRecord={onRecord}
              isRecorded={isRecorded}
            />
          );
        })}
      </>
    );
  }

  // Search mode: group events by channel, show top 2 + expandable rest
  const searchGroups = React.useMemo(() => {
    const groups: Array<[string, EpgEvent[]]> = [];

    for (const [serviceRef, events] of eventsByServiceRef.entries()) {
      if (events.length === 0) continue;

      // Sort events: NOW first, then chronological
      const sorted = [...events].sort((a, b) => {
        const aNow = a.start <= currentTime && a.end > currentTime ? 0 : 1;
        const bNow = b.start <= currentTime && b.end > currentTime ? 0 : 1;
        if (aNow !== bNow) return aNow - bNow;
        return a.start - b.start;
      });

      groups.push([serviceRef, sorted]);
    }

    // Sort groups by channel number/name
    groups.sort(([refA], [refB]) => {
      const chA = channels.find((c) => c.service_ref === refA || c.id === refA);
      const chB = channels.find((c) => c.service_ref === refB || c.id === refB);
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

  return (
    <>
      {searchGroups.map(([serviceRef, events]) => {
        const channel =
          channels.find((c) => c.service_ref === serviceRef || c.id === serviceRef) ||
          ({ service_ref: serviceRef, id: serviceRef, name: serviceRef } as EpgChannel);
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
    </>
  );
}
