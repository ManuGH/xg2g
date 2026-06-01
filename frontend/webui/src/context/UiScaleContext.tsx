import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { UI_SCALE_STORAGE_KEY, applyUiScaleToDocument, getDefaultUiScale, parseUiScale, readStoredUiScale, type UiScale } from '../lib/uiScale';
import { safeLocalStorage, writeLocalStorageItem } from '../lib/safeStorage';

interface UiScaleContextValue {
  scale: UiScale;
  setScale: (next: UiScale) => void;
}

const fallbackUiScaleContext: UiScaleContextValue = {
  scale: getDefaultUiScale(),
  setScale: () => {},
};

const UiScaleContext = createContext<UiScaleContextValue>(fallbackUiScaleContext);

function resolveInitialUiScale(): UiScale {
  if (typeof window === 'undefined') {
    return getDefaultUiScale();
  }

  return readStoredUiScale(safeLocalStorage());
}

export function useUiScale(): UiScaleContextValue {
  return useContext(UiScaleContext);
}

export function UiScaleProvider({ children }: { children: ReactNode }) {
  const [scale, setScale] = useState<UiScale>(resolveInitialUiScale);

  useEffect(() => {
    applyUiScaleToDocument(scale, document.documentElement);
    writeLocalStorageItem(UI_SCALE_STORAGE_KEY, scale);
  }, [scale]);

  useEffect(() => {
    const handleStorage = (event: StorageEvent) => {
      if (event.storageArea && event.storageArea !== safeLocalStorage()) {
        return;
      }

      if (event.key !== UI_SCALE_STORAGE_KEY) {
        return;
      }

      setScale(parseUiScale(event.newValue) ?? getDefaultUiScale());
    };

    window.addEventListener('storage', handleStorage);
    return () => {
      window.removeEventListener('storage', handleStorage);
    };
  }, []);

  useEffect(() => () => {
    document.documentElement.removeAttribute('data-ui-scale');
  }, []);

  const value = useMemo<UiScaleContextValue>(() => ({
    scale,
    setScale,
  }), [scale]);

  return (
    <UiScaleContext.Provider value={value}>
      {children}
    </UiScaleContext.Provider>
  );
}
