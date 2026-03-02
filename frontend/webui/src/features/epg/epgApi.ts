// EPG Data Layer - API calls and DTO→Domain mapping
// Zero React dependencies, zero legacy client imports

import { getEpg, getServices, getServicesBouquets, getTimers } from '../../client-ts';
import type { EpgEvent, EpgChannel, EpgBouquet, Timer } from './types';
import { debugError } from '../../utils/logging';

// ============================================================================
// API Fetch Functions (client-ts SDK only)
// ============================================================================

/**
 * Fetch all bouquets from the API
 * Maps SDK DTOs to domain EpgBouquet types
 */
export async function fetchBouquets(): Promise<EpgBouquet[]> {
  const result = await getServicesBouquets();

  if (result.error || !result.data) {
    throw new Error('Failed to fetch bouquets');
  }

  return result.data.map(mapSdkBouquet);
}

/**
 * Fetch channels/services, optionally filtered by bouquet
 * Maps SDK DTOs to domain EpgChannel types
 */
export async function fetchChannels(bouquetName?: string): Promise<EpgChannel[]> {
  const result = await getServices({
    query: bouquetName ? { bouquet: bouquetName } : undefined
  });

  if (result.error || !result.data) {
    throw new Error('Failed to fetch channels');
  }

  return (result.data || []).map(mapSdkChannel);
}

/**
 * Fetch EPG events with filters
 * Maps SDK DTOs to domain EpgEvent types
 */
export async function fetchEpgEvents(params: {
  from?: number;
  to?: number;
  bouquet?: string;
  query?: string;
  signal?: AbortSignal;
}): Promise<EpgEvent[]> {
  const result = await getEpg({
    query: {
      from: params.from,
      to: params.to,
      bouquet: params.bouquet,
      q: params.query
    },
    signal: params.signal
  });

  if (result.error || !result.data) {
    throw new Error('Failed to fetch EPG events');
  }


  const data = result.data;
  // API returns direct array for EPG, unlike TimerList.
  // We handle both legacy object-wrapped and new direct array for robustness,
  // but typed strictly to avoid unchecked casting.
  let items: any[] = [];
  if (Array.isArray(data)) {
    items = data;
  } else if (data && typeof data === 'object' && 'items' in data) {
    items = (data as { items: any[] }).items || [];
  }

  return items.map(mapSdkEvent);
}

/**
 * Fetch timers for recording feedback
 */
export async function fetchTimers(): Promise<Timer[]> {
  const result = await getTimers();

  if (result.error || !result.data) {
    return [];
  }

  return result.data.items || [];
}

// ============================================================================
// DTO Mapping Functions (SDK → Domain)
// ============================================================================

function mapSdkBouquet(dto: any): EpgBouquet {
  if (typeof dto !== 'object' || dto === null) {
    debugError('Invalid bouquet DTO (legacy string?):', dto);
    // Strict contract: Ignore invalid items or throw?
    // Throwing here might fail the whole fetch. Returning a dummy might hide it.
    // User wants "rejects it". If filtered out, it's rejected.
    // But map expects EpgBouquet.
    // Let's return a "Invalid" marker or throw.
    // Throwing ensures we don't silently accept.
    throw new Error(`Contract violation: Bouquet must be object, got ${typeof dto}`);
  }
  return {
    name: dto.name || '',
    services: typeof dto.services === 'number' ? dto.services : 0
  };
}

function mapSdkChannel(dto: any): EpgChannel {
  return {
    id: dto.id || dto.serviceRef || '',
    name: dto.name || 'Unknown',
    number: dto.number,
    group: dto.group,
    logoUrl: dto.logoUrl,
    logo: dto.logo
  };
}

function mapSdkEvent(dto: any): EpgEvent {
  return {
    serviceRef: dto.serviceRef || '',
    start: dto.start || 0,
    end: dto.end || 0,
    title: dto.title || '',
    desc: dto.desc
  };
}
