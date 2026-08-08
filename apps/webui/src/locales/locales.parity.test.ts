import { describe, expect, it } from 'vitest';
import de from './de.json';
import en from './en.json';
import { TRANSLATED_REASONS } from '../features/player/utils/sessionReason';

type Json = Record<string, unknown>;

function flatten(value: Json, prefix = ''): string[] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return child !== null && typeof child === 'object' && !Array.isArray(child)
      ? flatten(child as Json, path)
      : [path];
  });
}

function lookup(source: Json, path: string): unknown {
  return path.split('.').reduce<unknown>(
    (node, part) => (node && typeof node === 'object' ? (node as Json)[part] : undefined),
    source,
  );
}

// i18n is configured with `fallbackLng: false` (src/i18n.ts), which is deliberate
// — it keeps a half-translated language from silently reading as English. The
// consequence is that a key present in one locale and missing in the other does
// not degrade gracefully: i18next returns the key itself, so the user sees a raw
// string like "player.reason.R_DESCRAMBLER_DOWN" in the interface. Parity is
// therefore a correctness requirement here, not tidiness.
describe('locale parity', () => {
  const enKeys = flatten(en as Json);
  const deKeys = flatten(de as Json);

  it('has the same key set in every locale', () => {
    const missingInDe = enKeys.filter((k) => !deKeys.includes(k));
    const missingInEn = deKeys.filter((k) => !enKeys.includes(k));
    expect({ missingInDe, missingInEn }).toEqual({ missingInDe: [], missingInEn: [] });
  });

  it('has no empty strings, which render as blank UI', () => {
    const blank = enKeys.filter((k) => {
      const a = lookup(en as Json, k);
      const b = lookup(de as Json, k);
      return a === '' || b === '';
    });
    expect(blank).toEqual([]);
  });

  // The player decides which backend reason codes it translates from its own
  // TRANSLATED_REASONS set rather than from the API spec. A code listed there
  // without a string in every locale is exactly the raw-key failure above.
  it('translates every reason code it claims to handle, in every locale', () => {
    const missing = [...TRANSLATED_REASONS].flatMap((code) => {
      const path = `player.reason.${code}`;
      return [
        lookup(en as Json, path) ? [] : [`en:${path}`],
        lookup(de as Json, path) ? [] : [`de:${path}`],
      ].flat();
    });
    expect(missing).toEqual([]);
  });
});
