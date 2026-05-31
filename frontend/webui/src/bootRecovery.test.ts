import { describe, expect, it } from 'vitest';
import { buildCanonicalPath } from './bootRecovery';

describe('buildCanonicalPath', () => {
  it('prefixes a bare path that is missing the basename', () => {
    expect(buildCanonicalPath('/ui', '/epg')).toBe('/ui/epg');
    expect(buildCanonicalPath('/ui', '/dashboard', '?x=1', '#h')).toBe('/ui/dashboard?x=1#h');
  });

  it('rewrites the site root to the basename root', () => {
    expect(buildCanonicalPath('/ui', '/')).toBe('/ui/');
  });

  it('leaves paths already under the basename untouched', () => {
    expect(buildCanonicalPath('/ui', '/ui/dashboard')).toBeNull();
    expect(buildCanonicalPath('/ui', '/ui')).toBeNull();
    expect(buildCanonicalPath('/ui', '/ui/')).toBeNull();
  });

  it('does not treat a different prefix as the basename', () => {
    // "/uixyz" must NOT be considered under "/ui"
    expect(buildCanonicalPath('/ui', '/uixyz')).toBe('/ui/uixyz');
  });

  it('returns null when no basename is configured (served at root)', () => {
    expect(buildCanonicalPath(undefined, '/epg')).toBeNull();
    expect(buildCanonicalPath('', '/epg')).toBeNull();
  });
});
