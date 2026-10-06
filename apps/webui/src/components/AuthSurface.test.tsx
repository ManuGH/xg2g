import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import AuthSurface from './AuthSurface';

vi.mock('../lib/insecureContext', () => ({ detectInsecureContext: () => true }));

describe('AuthSurface', () => {
  it('renders the insecure context banner above the authentication overlay backdrop', () => {
    window.sessionStorage.removeItem('xg2g.insecureContextBannerDismissed');
    const { container } = render(<AuthSurface title="Unlock" testId="auth-surface" />);
    const overlay = container.querySelector('[class*="overlay"]');
    const banner = screen.getByRole('alert');

    expect(overlay).toContainElement(banner);
    expect(banner.parentElement).toHaveStyle({ position: 'absolute', zIndex: '1' });
    expect(banner).toHaveStyle({ width: '100%' });
  });
});
