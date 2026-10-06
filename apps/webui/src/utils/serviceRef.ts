// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

/**
 * Normalizes an Enigma2 / DVB service reference for robust comparison:
 * - Trims whitespace
 * - Strips trailing colons (e.g. "1:0:19:AAAA:BBB:1:C00000:0:0:0:" -> "1:0:19:AAAA:BBB:1:C00000:0:0:0")
 * - Normalizes hex segments to uppercase if segments match standard DVB ref syntax
 */
export function canonicalServiceRef(ref?: string | null): string {
  if (!ref) return '';
  const trimmed = ref.trim().replace(/:+$/, '');
  return /^[0-9a-fA-F:]+$/.test(trimmed) ? trimmed.toUpperCase() : trimmed;
}

/**
 * Determines whether two service references identify the same channel/service.
 */
export function matchServiceRef(a?: string | null, b?: string | null): boolean {
  const ca = canonicalServiceRef(a);
  const cb = canonicalServiceRef(b);
  return Boolean(ca && cb && ca === cb);
}
