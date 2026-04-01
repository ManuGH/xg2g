import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

interface PendingChangesGuard {
  isDirty: boolean;
  confirmDiscard: () => Promise<boolean>;
}

interface PendingChangesContextValue {
  hasPendingChanges: boolean;
  setPendingChangesGuard: (guard: PendingChangesGuard | null) => void;
  confirmPendingChanges: () => Promise<boolean>;
}

const PendingChangesContext = createContext<PendingChangesContextValue | undefined>(undefined);

export function usePendingChanges(): PendingChangesContextValue {
  const context = useContext(PendingChangesContext);
  if (!context) {
    throw new Error('usePendingChanges must be used within PendingChangesProvider');
  }
  return context;
}

export function PendingChangesProvider({ children }: { children: ReactNode }) {
  const [guard, setGuard] = useState<PendingChangesGuard | null>(null);
  const guardRef = useRef<PendingChangesGuard | null>(null);
  const confirmInFlightRef = useRef<Promise<boolean> | null>(null);

  const setPendingChangesGuard = useCallback((nextGuard: PendingChangesGuard | null) => {
    guardRef.current = nextGuard;
    setGuard(nextGuard);
  }, []);

  const confirmPendingChanges = useCallback(() => {
    const activeGuard = guardRef.current;
    if (!activeGuard?.isDirty) {
      return Promise.resolve(true);
    }

    if (confirmInFlightRef.current) {
      return confirmInFlightRef.current;
    }

    const pendingConfirmation = (async () => {
      try {
        const ok = await activeGuard.confirmDiscard();
        if (ok) {
          setPendingChangesGuard(null);
        }
        return ok;
      } finally {
        confirmInFlightRef.current = null;
      }
    })();

    confirmInFlightRef.current = pendingConfirmation;
    return pendingConfirmation;
  }, [setPendingChangesGuard]);

  useEffect(() => {
    if (!guard?.isDirty || typeof window === 'undefined') {
      return;
    }

    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [guard]);

  const value = useMemo<PendingChangesContextValue>(() => ({
    hasPendingChanges: Boolean(guard?.isDirty),
    setPendingChangesGuard,
    confirmPendingChanges,
  }), [confirmPendingChanges, guard, setPendingChangesGuard]);

  return (
    <PendingChangesContext.Provider value={value}>
      {children}
    </PendingChangesContext.Provider>
  );
}
