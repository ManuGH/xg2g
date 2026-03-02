const ENTITY_MAP: Record<string, string> = {
  '&quot;': '"',
  '&amp;': '&',
  '&lt;': '<',
  '&gt;': '>',
  '&apos;': "'",
  '&nbsp;': ' ',
  '&mdash;': '—',
  '&ndash;': '–',
};

/**
 * Normalizes EPG text by unescaping literals and decoding HTML entities.
 * This is primarily used for program descriptions which may contain encoded characters.
 */
export function normalizeEpgText(text?: string): string {
  if (!text) return '';

  return text
    // 1. Unescape literal newlines and carriage returns
    .replace(/\\n/g, '\n')
    .replace(/\\r/g, '')
    // 2. Decode HTML entities (named, decimal, and hex)
    .replace(/&(#(?:x[0-9a-fA-F]+|[0-9]+)|[a-z0-9]+);/gi, (match, entity) => {
      const lowerMatch = match.toLowerCase();
      if (ENTITY_MAP[lowerMatch]) return ENTITY_MAP[lowerMatch];

      if (entity.startsWith('#x') || entity.startsWith('#X')) {
        return String.fromCharCode(parseInt(entity.slice(2), 16));
      }
      if (entity.startsWith('#')) {
        return String.fromCharCode(parseInt(entity.slice(1), 10));
      }

      return match;
    });
}
