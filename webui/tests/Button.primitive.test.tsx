import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { Button, ButtonLink } from '../src/components/ui';

describe('Button primitive', () => {
  it('renders a <button> with default type="button"', () => {
    render(<Button>Click</Button>);
    const btn = screen.getByRole('button', { name: /click/i });
    expect(btn).toHaveAttribute('type', 'button');
  });

  it('renders an <a> when href is provided', () => {
    render(<Button href="/download">Download</Button>);
    const link = screen.getByRole('link', { name: /download/i });
    expect(link).toHaveAttribute('href', '/download');
  });

  it('ButtonLink is a thin wrapper around Button anchor mode', () => {
    render(<ButtonLink href="/x" download>Export</ButtonLink>);
    const link = screen.getByRole('link', { name: /export/i });
    expect(link).toHaveAttribute('href', '/x');
    expect(link).toHaveAttribute('download');
  });
});

