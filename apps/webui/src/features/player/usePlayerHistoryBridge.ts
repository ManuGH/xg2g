import { useCallback, useEffect, useRef } from 'react';

const PLAYER_HISTORY_STATE_KEY = '__xg2gPlayerOverlay';

function isStateRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function hasPlayerHistoryState(state: unknown): boolean {
  return isStateRecord(state) && state[PLAYER_HISTORY_STATE_KEY] === true;
}

function addPlayerHistoryState(state: unknown): Record<string, unknown> {
  return {
    ...(isStateRecord(state) ? state : {}),
    [PLAYER_HISTORY_STATE_KEY]: true,
  };
}

function removePlayerHistoryState(state: unknown): Record<string, unknown> | null {
  if (!isStateRecord(state)) {
    return null;
  }

  const { [PLAYER_HISTORY_STATE_KEY]: _discard, ...rest } = state;
  return Object.keys(rest).length > 0 ? rest : null;
}

export function usePlayerHistoryBridge(isOpen: boolean, closePlayer: () => void): () => void {
  const closePlayerRef = useRef(closePlayer);
  const closingFromHistoryRef = useRef(false);

  useEffect(() => {
    closePlayerRef.current = closePlayer;
  }, [closePlayer]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    if (!isOpen) {
      if (hasPlayerHistoryState(window.history.state)) {
        window.history.replaceState(
          removePlayerHistoryState(window.history.state),
          '',
          window.location.href,
        );
      }
      closingFromHistoryRef.current = false;
      return;
    }

    if (!hasPlayerHistoryState(window.history.state)) {
      window.history.pushState(
        addPlayerHistoryState(window.history.state),
        '',
        window.location.href,
      );
    }

    closingFromHistoryRef.current = false;

    const handlePopState = (event: PopStateEvent) => {
      if (hasPlayerHistoryState(event.state)) {
        return;
      }

      closingFromHistoryRef.current = true;
      closePlayerRef.current();
    };

    window.addEventListener('popstate', handlePopState);
    return () => {
      window.removeEventListener('popstate', handlePopState);
    };
  }, [isOpen]);

  return useCallback(() => {
    if (typeof window === 'undefined') {
      closePlayer();
      return;
    }

    if (closingFromHistoryRef.current) {
      closePlayer();
      return;
    }

    if (hasPlayerHistoryState(window.history.state)) {
      window.history.back();
      return;
    }

    closePlayer();
  }, [closePlayer]);
}
