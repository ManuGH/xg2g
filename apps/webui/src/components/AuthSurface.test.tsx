import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import AuthSurface from './AuthSurface';

vi.mock('../lib/insecureContext', () => ({ detectInsecureContext: () => true }));

describe('AuthSurface', () => {
  it('renders the insecure context banner in flow above the dialog, not over it', () => {
    window.sessionStorage.removeItem('xg2g.insecureContextBannerDismissed');
    const { container } = render(<AuthSurface title="Unlock" testId="auth-surface" />);
    const overlay = container.querySelector('[class*="overlay"]');
    const banner = screen.getByRole('alert');
    const slot = banner.parentElement;

    expect(overlay).toContainElement(banner);
    // The banner slot precedes the dialog, so it takes its own space instead of covering it.
    expect(slot?.nextElementSibling).toBe(screen.getByRole('dialog'));
    expect(slot).not.toHaveStyle({ position: 'absolute' });
  });
});
