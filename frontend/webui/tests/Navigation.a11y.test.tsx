import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import Navigation from '../src/components/Navigation';

describe('Navigation semantics', () => {
  it('renders nav items as <button type="button"> and sets aria-current on active view', () => {
    const onViewChange = vi.fn();
    render(<Navigation activeView="epg" onViewChange={onViewChange} />);

    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBeGreaterThan(0);
    for (const b of buttons) {
      expect(b).toHaveAttribute('type', 'button');
    }

    const current = buttons.filter(b => b.getAttribute('aria-current') === 'page');
    expect(current).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: /nav\.dashboard/i }));
    expect(onViewChange).toHaveBeenCalledWith('dashboard');
  });
});
