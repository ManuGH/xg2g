import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import LoadingSkeleton from './LoadingSkeleton';


describe('LoadingSkeleton', () => {
  it('renders the gate variant as a fullscreen loading status', () => {
    render(<LoadingSkeleton variant="gate" label="Initializing..." />);

    const skeleton = screen.getByRole('status', { name: 'Initializing...' });
    expect(skeleton).toHaveAttribute('data-loading-variant', 'gate');
  });

  it('renders the page variant for shell-level loading', () => {
    render(<LoadingSkeleton variant="page" label="Loading page" />);

    const skeleton = screen.getByRole('status', { name: 'Loading page' });
    expect(skeleton).toHaveAttribute('data-loading-variant', 'page');
  });

  it('renders the section variant for inline loading', () => {
    render(<LoadingSkeleton variant="section" label="Loading section" />);

    const skeleton = screen.getByRole('status', { name: 'Loading section' });
    expect(skeleton).toHaveAttribute('data-loading-variant', 'section');
  });
});
