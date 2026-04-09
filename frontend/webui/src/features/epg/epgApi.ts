// EPG Data Layer - API calls and DTO→Domain mapping
// Zero React dependencies, zero legacy client imports

import {
  getEpg,
  getServices,
  getServicesBouquets,
  getTimers,
  type Bouquet as ApiBouquet,
  type GetEpgResponse,
  type Service as ApiService,
  type TimerList,
} from '../../client-ts';
import { throwOnClientResultError, unwrapClientResultOrThrow } from '../../services/clientWrapper';
import type { EpgEvent, EpgChannel, EpgBouquet, Timer } from './types';
import { debugError } from '../../utils/logging';

type EpgDto = NonNullable<GetEpgResponse>[number];

/**
 * Fetch all bouquets from the API
 * Maps SDK DTOs to domain EpgBouquet types
 */
export async function fetchBouquets(): Promise<EpgBouquet[]> {
  const result = await getServicesBouquets();
  throwOnClientResultError(result, { source: 'EPG.fetchBouquets' });

  return (result.data || []).map(mapSdkBouquet);
}

/**
 * Fetch channels/services, optionally filtered by bouquet
 * Maps SDK DTOs to domain EpgChannel types
 */
export async function fetchChannels(bouquetName?: string): Promise<EpgChannel[]> {
  const result = await getServices({
    query: bouquetName ? { bouquet: bouquetName } : undefined
  });
  throwOnClientResultError(result, { source: 'EPG.fetchChannels' });

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
  throwOnClientResultError(result, { source: 'EPG.fetchEpgEvents' });


  const data = result.data;
  if (data == null) {
    return [];
  }

  if (!Array.isArray(data)) {
    throw new Error('Contract violation: EPG response must be a bare JSON array');
  }

  return data.map(mapSdkEvent);
}

/**
 * Fetch timers for recording feedback
 */
export async function fetchTimers(): Promise<Timer[]> {
  const result = await getTimers();
  const data = unwrapClientResultOrThrow<TimerList>(result, {
    source: 'EPG.fetchTimers',
    silent: true
  });
  return data?.items || [];
}

// ============================================================================
// DTO Mapping Functions (SDK → Domain)
// ============================================================================

function mapSdkBouquet(dto: unknown): EpgBouquet {
  if (typeof dto !== 'object' || dto === null) {
    debugError('Invalid bouquet DTO (legacy string?):', dto);
    throw new Error(`Contract violation: Bouquet must be object, got ${typeof dto}`);
  }
  const bouquet = dto as ApiBouquet;
  return {
    name: bouquet.name || '',
    services: typeof bouquet.services === 'number' ? bouquet.services : 0
  };
}

function mapSdkChannel(dto: ApiService): EpgChannel {
  return {
    id: dto.id || dto.serviceRef || '',
    serviceRef: dto.serviceRef || dto.id || '',
    name: dto.name || 'Unknown',
    number: dto.number,
    group: dto.group,
    logoUrl: dto.logoUrl,
    resolution: dto.resolution,
    codec: dto.codec,
  };
}

function mapSdkEvent(dto: EpgDto): EpgEvent {
  return {
    serviceRef: dto.serviceRef || '',
    start: dto.start || 0,
    end: dto.end || 0,
    title: dto.title || '',
    desc: dto.desc
  };
}
