import type { ErrorCatalogEntry } from '../client-ts';

const entriesByCode = new Map<string, ErrorCatalogEntry>();

export function setErrorCatalog(entries: readonly ErrorCatalogEntry[]): void {
  entriesByCode.clear();
  for (const entry of entries) {
    entriesByCode.set(entry.code, entry);
  }
}

export function getErrorCatalogEntry(code?: string | null): ErrorCatalogEntry | undefined {
  if (!code) {
    return undefined;
  }
  return entriesByCode.get(code);
}

export function resetErrorCatalog(): void {
  entriesByCode.clear();
}
