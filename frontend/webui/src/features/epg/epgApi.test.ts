import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchEpgEvents } from './epgApi';
import * as client from '../../client-ts';

vi.mock('../../client-ts', async () => ({
  getEpg: vi.fn(),
  getServices: vi.fn(),
  getServicesBouquets: vi.fn(),
  getTimers: vi.fn(),
}));

describe('fetchEpgEvents', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('treats a null payload as an empty result set', async () => {
    (client.getEpg as any).mockResolvedValue({
      data: null,
      error: null,
    });

    await expect(fetchEpgEvents({})).resolves.toEqual([]);
  });

  it('maps bare array payloads to EPG events', async () => {
    (client.getEpg as any).mockResolvedValue({
      data: [
        {
          serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
          start: 100,
          end: 200,
          title: 'News',
          desc: 'Morning news',
        },
      ],
      error: null,
    });

    await expect(fetchEpgEvents({})).resolves.toEqual([
      {
        serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
        start: 100,
        end: 200,
        title: 'News',
        desc: 'Morning news',
      },
    ]);
  });

  it('rejects legacy object-wrapped payloads', async () => {
    (client.getEpg as any).mockResolvedValue({
      data: {
        items: [
          {
            serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
            start: 100,
            end: 200,
            title: 'Wrapped',
          },
        ],
      },
      error: null,
    });

    await expect(fetchEpgEvents({})).rejects.toThrow(/EPG response must be a bare JSON array/i);
  });

  it('preserves HTTP status for auth failures', async () => {
    (client.getEpg as any).mockResolvedValue({
      data: null,
      error: {
        title: 'Unauthorized',
        status: 401,
        requestId: 'req-epg-auth'
      },
      response: { status: 401 }
    });

    await expect(fetchEpgEvents({})).rejects.toMatchObject({
      name: 'ClientRequestError',
      status: 401,
      requestId: 'req-epg-auth'
    });
  });
});
