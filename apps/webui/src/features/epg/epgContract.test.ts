
import { describe, it, expect, vi } from 'vitest';
import { fetchBouquets } from './epgApi';
import * as client from '../../client-ts';

// Mock the client SDK
vi.mock('../../client-ts', async () => ({
  getServicesBouquets: vi.fn(),
}));

describe('EPG Contract Enforcement', () => {
  it('should reject legacy string[] bouquets', async () => {
    const legacyResponse = {
      data: ["Bouquet A", "Bouquet B"] as any, // Simulate legacy backend response
      error: null
    };

    (client.getServicesBouquets as any).mockResolvedValue(legacyResponse);

    await expect(fetchBouquets()).rejects.toThrow(/Contract violation/);
  });

  it('should accept valid Bouquet objects', async () => {
    const validResponse = {
      data: [{ name: "Bouquet A", services: 10 }],
      error: null
    };

    (client.getServicesBouquets as any).mockResolvedValue(validResponse);

    const result = await fetchBouquets();
    expect(result).toHaveLength(1);
    expect(result[0]?.name).toBe("Bouquet A");
  });
});
