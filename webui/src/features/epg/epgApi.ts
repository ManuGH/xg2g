// EPG Data Layer - API calls and DTO→Domain mapping
// Zero React dependencies, zero legacy client imports

import { getEpg, getServices, getServicesBouquets, getTimers } from '../../client-ts';
import type { EpgEvent, EpgChannel, EpgBouquet, Timer } from './types';

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
}): Promise<EpgEvent[]> {
  const result = await getEpg({
    query: {
      from: params.from,
      to: params.to,
      bouquet: params.bouquet,
      q: params.query
    }
  });

  if (result.error || !result.data) {
    throw new Error('Failed to fetch EPG events');
  }

  return (result.data.items || []).map(mapSdkEvent);
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
  return {
    name: dto.name || '',
    services: dto.services || []
  };
}

function mapSdkChannel(dto: any): EpgChannel {
  return {
    id: dto.id || dto.service_ref || '',
    service_ref: dto.service_ref,
    serviceRef: dto.serviceRef,
    name: dto.name || 'Unknown',
    number: dto.number,
    group: dto.group,
    logo_url: dto.logo_url,
    logoUrl: dto.logoUrl,
    logo: dto.logo
  };
}

function mapSdkEvent(dto: any): EpgEvent {
  return {
    service_ref: dto.service_ref || '',
    start: dto.start || 0,
    end: dto.end || 0,
    title: dto.title || '',
    desc: dto.desc
  };
}
