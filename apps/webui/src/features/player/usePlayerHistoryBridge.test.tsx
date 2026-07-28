import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlayerHistoryBridge } from './usePlayerHistoryBridge';

function HistoryBridgeHarness({ onClose }: { onClose: () => void }) {
  const [isOpen, setIsOpen] = useState(false);
  const handlePlayerClose = usePlayerHistoryBridge(isOpen, () => {
    onClose();
    setIsOpen(false);
  });

  return (
    <div>
      <button onClick={() => setIsOpen(true)} type="button">
        Open player
      </button>
      <button onClick={() => setIsOpen(false)} type="button">
        Force close
      </button>
      <div data-testid="player-state">{isOpen ? 'open' : 'closed'}</div>
      {isOpen ? (
        <button onClick={handlePlayerClose} type="button">
          Close player
        </button>
      ) : null}
    </div>
  );
}

describe('usePlayerHistoryBridge', () => {
  beforeEach(() => {
    window.history.replaceState({ base: true }, '', '/ui/epg');
  });

  it('pushes a history state when the player opens and closes on popstate', async () => {
    const onClose = vi.fn();

    render(<HistoryBridgeHarness onClose={onClose} />);

    fireEvent.click(screen.getByRole('button', { name: 'Open player' }));

    expect(window.history.state).toMatchObject({
      __xg2gPlayerOverlay: true,
      base: true,
    });
    expect(screen.getByTestId('player-state')).toHaveTextContent('open');

    act(() => {
      window.history.replaceState({ base: true }, '', '/ui/epg');
      window.dispatchEvent(new PopStateEvent('popstate', { state: { base: true } }));
    });

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByTestId('player-state')).toHaveTextContent('closed');
  });

  it('uses history.back when the player is closed explicitly from the overlay state', async () => {
    const onClose = vi.fn();
    const historyBack = vi.spyOn(window.history, 'back').mockImplementation(() => {
      window.history.replaceState({ base: true }, '', '/ui/epg');
      window.dispatchEvent(new PopStateEvent('popstate', { state: { base: true } }));
    });

    render(<HistoryBridgeHarness onClose={onClose} />);

    fireEvent.click(screen.getByRole('button', { name: 'Open player' }));
    fireEvent.click(screen.getByRole('button', { name: 'Close player' }));

    expect(historyBack).toHaveBeenCalledTimes(1);

    await waitFor(() => {
      expect(onClose).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByTestId('player-state')).toHaveTextContent('closed');

    historyBack.mockRestore();
  });

  it('removes stale player history state when the player closes outside browser history', async () => {
    const onClose = vi.fn();

    render(<HistoryBridgeHarness onClose={onClose} />);

    fireEvent.click(screen.getByRole('button', { name: 'Open player' }));
    expect(window.history.state).toMatchObject({
      __xg2gPlayerOverlay: true,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Force close' }));

    await waitFor(() => {
      expect(screen.getByTestId('player-state')).toHaveTextContent('closed');
    });
    expect(window.history.state).toEqual({ base: true });
    expect(onClose).not.toHaveBeenCalled();
  });
});
