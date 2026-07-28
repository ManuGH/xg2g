import { describe, expect, it } from 'vitest';
import { shouldWarnInsecureContext } from './insecureContext';

const https = { isSecureContext: true, protocol: 'https:', hostname: 'tv.example.com' };

describe('shouldWarnInsecureContext', () => {
  it('warns on a LAN IP over plain HTTP (the self-host trap)', () => {
    expect(
      shouldWarnInsecureContext({ isSecureContext: false, protocol: 'http:', hostname: '10.10.55.14' }),
    ).toBe(true);
  });

  // Negative control: a real HTTPS deployment must NEVER show the banner.
  it('does not warn over HTTPS', () => {
    expect(shouldWarnInsecureContext(https)).toBe(false);
  });

  // Negative control: localhost is a secure context even over http — no banner,
  // so local dev is never nagged.
  it('does not warn on http://localhost or 127.0.0.1', () => {
    expect(
      shouldWarnInsecureContext({ isSecureContext: true, protocol: 'http:', hostname: 'localhost' }),
    ).toBe(false);
    expect(
      shouldWarnInsecureContext({ isSecureContext: false, protocol: 'http:', hostname: '127.0.0.1' }),
    ).toBe(false);
    expect(
      shouldWarnInsecureContext({ isSecureContext: false, protocol: 'http:', hostname: 'app.localhost' }),
    ).toBe(false);
  });

  it('trusts the browser isSecureContext verdict over the protocol when present', () => {
    // Behind a TLS-terminating setup the browser may report secure even if a
    // naive protocol check would not — defer to the browser.
    expect(
      shouldWarnInsecureContext({ isSecureContext: true, protocol: 'http:', hostname: 'tv.example.com' }),
    ).toBe(false);
  });

  it('falls back to the protocol when isSecureContext is unavailable', () => {
    expect(
      shouldWarnInsecureContext({ isSecureContext: undefined, protocol: 'http:', hostname: 'tv.example.com' }),
    ).toBe(true);
    expect(
      shouldWarnInsecureContext({ isSecureContext: undefined, protocol: 'https:', hostname: 'tv.example.com' }),
    ).toBe(false);
  });
});
