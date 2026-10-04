import { describe, expect, it } from 'vitest';
import i18n from '../../src/i18n';

describe('i18next type augmentation & strict key checks contract', () => {
  it('accepts valid statically defined keys without compiler errors', () => {
    const t = i18n.t;

    // 1. Valid keys across various namespaces must compile cleanly and return non-empty strings
    const saveLabel = t('common.save');
    const navDashboard = t('nav.dashboard');
    const unlockTitle = t('unlock.pageTitle');
    const adminHeader = t('admin.header.title');
    const adminDevices = t('admin.devices.title');
    const authBootstrap = t('auth.bootstrap.title');

    expect(typeof saveLabel).toBe('string');
    expect(saveLabel.length).toBeGreaterThan(0);
    expect(typeof navDashboard).toBe('string');
    expect(navDashboard.length).toBeGreaterThan(0);
    expect(typeof unlockTitle).toBe('string');
    expect(unlockTitle.length).toBeGreaterThan(0);
    expect(typeof adminHeader).toBe('string');
    expect(adminHeader.length).toBeGreaterThan(0);
    expect(typeof adminDevices).toBe('string');
    expect(adminDevices.length).toBeGreaterThan(0);
    expect(typeof authBootstrap).toBe('string');
    expect(authBootstrap.length).toBeGreaterThan(0);
  });

  it('verifies compiler regression checks at type-check time', () => {
    const t = i18n.t;

    // 2. Unknown key without defaultValue MUST be rejected by the compiler.
    // If strictKeyChecks or resource-typing is broken, this line becomes valid
    // and the @ts-expect-error directive will trigger TS2578 (Unused '@ts-expect-error' directive).
    // @ts-expect-error compiler regression: unknown key without defaultValue must be rejected
    t('unknown.nonexistent.key.without.default');

    // 3. Unknown key with defaultValue option MUST be rejected by the compiler.
    // Under standard i18next this would be silently accepted as a fallback;
    // strictKeyChecks: true ensures it is strictly rejected.
    // @ts-expect-error compiler regression: unknown key with defaultValue option must be rejected
    t('unknown.nonexistent.key.with.default.option', { defaultValue: 'Fallback' });

    // 4. Unknown key with 2nd-arg defaultValue string MUST also be rejected by the compiler.
    // @ts-expect-error compiler regression: unknown key with defaultValue string must be rejected
    t('unknown.nonexistent.key.with.default.string', 'Fallback');
  });
});
