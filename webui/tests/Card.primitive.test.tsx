import React from 'react';
import { render, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { Card } from '../src/components/ui';

describe('Card primitive', () => {
  it('is keyboard-operable when onClick is provided (Enter/Space)', () => {
    const onClick = vi.fn();
    const { container } = render(<Card onClick={onClick}>Body</Card>);
    const el = container.querySelector('[data-ui="card"]') as HTMLElement;
    expect(el).toBeTruthy();
    expect(el).toHaveAttribute('role', 'button');
    expect(el).toHaveAttribute('tabindex', '0');

    fireEvent.keyDown(el, { key: 'Enter' });
    fireEvent.keyDown(el, { key: ' ' });
    expect(onClick).toHaveBeenCalledTimes(2);
  });

  it('does not expose button semantics when interactive but not clickable', () => {
    const { container } = render(<Card interactive>Body</Card>);
    const el = container.querySelector('[data-ui="card"]') as HTMLElement;
    expect(el).toBeTruthy();
    expect(el).not.toHaveAttribute('role');
    expect(el).not.toHaveAttribute('tabindex');
    expect(el).toHaveAttribute('data-interactive', 'true');
  });
});

