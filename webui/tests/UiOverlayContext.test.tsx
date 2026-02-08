import React from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { UiOverlayProvider, useUiOverlay } from '../src/context/UiOverlayContext';

function Harness() {
  const { toast, confirm } = useUiOverlay();
  const [result, setResult] = React.useState<string>('');

  return (
    <div>
      <button
        type="button"
        onClick={() => toast({ kind: 'success', message: 'Hello', timeoutMs: 1000 })}
      >
        Toast
      </button>

      <button
        type="button"
        onClick={async () => {
          const ok = await confirm({
            title: 'Confirm',
            message: 'Proceed?',
            confirmLabel: 'Yes',
            cancelLabel: 'No',
          });
          setResult(ok ? 'yes' : 'no');
        }}
      >
        Confirm
      </button>

      <div data-testid="result">{result}</div>
    </div>
  );
}

describe('UiOverlayProvider', () => {
  it('renders toast and allows manual dismiss', async () => {
    render(
      <UiOverlayProvider>
        <Harness />
      </UiOverlayProvider>
    );

    fireEvent.click(screen.getByText('Toast'));
    expect(await screen.findByText('Hello')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /dismiss notification/i }));

    await waitFor(() => {
      expect(screen.queryByText('Hello')).not.toBeInTheDocument();
    });
  });

  it('auto-dismisses toast after timeout', async () => {
    vi.useFakeTimers();
    try {
      render(
        <UiOverlayProvider>
          <Harness />
        </UiOverlayProvider>
      );

      fireEvent.click(screen.getByText('Toast'));
      expect(screen.getByText('Hello')).toBeInTheDocument();

      act(() => {
        vi.advanceTimersByTime(1000);
      });

      expect(screen.queryByText('Hello')).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('confirm resolves true on confirm click', async () => {
    render(
      <UiOverlayProvider>
        <Harness />
      </UiOverlayProvider>
    );

    fireEvent.click(screen.getByText('Confirm'));
    expect(await screen.findByRole('dialog', { name: 'Confirm' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Yes' }));

    await waitFor(() => {
      expect(screen.getByTestId('result').textContent).toBe('yes');
    });
  });

  it('confirm resolves false on Escape', async () => {
    render(
      <UiOverlayProvider>
        <Harness />
      </UiOverlayProvider>
    );

    fireEvent.click(screen.getByText('Confirm'));
    expect(await screen.findByRole('dialog', { name: 'Confirm' })).toBeInTheDocument();

    fireEvent.keyDown(window, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.getByTestId('result').textContent).toBe('no');
    });
  });
});
