import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { resolveHostEnvironment } from '../lib/hostBridge';
import {
  applyUiSurfaceToDocument,
  resolveUiSurface,
  type UiSurfaceState,
} from '../lib/uiSurface';

interface UiSurfaceContextValue extends UiSurfaceState {}

const fallbackUiSurface: UiSurfaceContextValue = {
  surface: 'large',
  orientation: 'landscape',
  inputMode: 'fine',
  heightClass: 'comfortable',
  navMode: 'rail',
  width: 1440,
  height: 900,
};

const UiSurfaceContext = createContext<UiSurfaceContextValue>(fallbackUiSurface);

function resolveInitialUiSurface(): UiSurfaceContextValue {
  if (typeof window === 'undefined') {
    return fallbackUiSurface;
  }

  return resolveUiSurface(window, resolveHostEnvironment());
}

function isSameSurface(left: UiSurfaceState, right: UiSurfaceState): boolean {
  return left.surface === right.surface
    && left.orientation === right.orientation
    && left.inputMode === right.inputMode
    && left.heightClass === right.heightClass
    && left.navMode === right.navMode
    && left.width === right.width
    && left.height === right.height;
}

function bindMediaQueryListener(mediaQuery: MediaQueryList, listener: () => void): () => void {
  if (typeof mediaQuery.addEventListener === 'function') {
    mediaQuery.addEventListener('change', listener);
    return () => mediaQuery.removeEventListener('change', listener);
  }

  mediaQuery.addListener(listener);
  return () => mediaQuery.removeListener(listener);
}

export function useUiSurface(): UiSurfaceContextValue {
  return useContext(UiSurfaceContext);
}

export function UiSurfaceProvider({ children }: { children: ReactNode }) {
  const [surface, setSurface] = useState<UiSurfaceContextValue>(resolveInitialUiSurface);

  useEffect(() => {
    applyUiSurfaceToDocument(surface, document.documentElement);
  }, [surface]);

  useEffect(() => {
    const environment = resolveHostEnvironment();

    const updateSurface = () => {
      const next = resolveUiSurface(window, environment);
      setSurface((current) => (isSameSurface(current, next) ? current : next));
    };

    const visualViewport = window.visualViewport;
    const mediaQueries = typeof window.matchMedia === 'function'
      ? [
          window.matchMedia('(pointer: coarse)'),
          window.matchMedia('(any-pointer: coarse)'),
          window.matchMedia('(hover: none)'),
          window.matchMedia('(any-hover: none)'),
        ]
      : [];

    window.addEventListener('resize', updateSurface);
    window.addEventListener('orientationchange', updateSurface);
    visualViewport?.addEventListener('resize', updateSurface);
    visualViewport?.addEventListener('scroll', updateSurface);
    const detachMediaListeners = mediaQueries.map((query) => bindMediaQueryListener(query, updateSurface));

    return () => {
      window.removeEventListener('resize', updateSurface);
      window.removeEventListener('orientationchange', updateSurface);
      visualViewport?.removeEventListener('resize', updateSurface);
      visualViewport?.removeEventListener('scroll', updateSurface);
      detachMediaListeners.forEach((detach) => detach());
    };
  }, []);

  useEffect(() => () => {
    const root = document.documentElement;
    root.removeAttribute('data-ui-surface');
    root.removeAttribute('data-ui-orientation');
    root.removeAttribute('data-ui-input');
    root.removeAttribute('data-ui-height');
    root.removeAttribute('data-ui-nav-mode');
  }, []);

  const value = useMemo<UiSurfaceContextValue>(() => surface, [surface]);

  return (
    <UiSurfaceContext.Provider value={value}>
      {children}
    </UiSurfaceContext.Provider>
  );
}
