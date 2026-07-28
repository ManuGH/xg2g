import type { HostEnvironment } from './hostBridge';

export const UI_SURFACE_VALUES = ['small', 'medium', 'large', 'xlarge'] as const;
export const UI_ORIENTATION_VALUES = ['portrait', 'landscape'] as const;
export const UI_INPUT_VALUES = ['coarse', 'fine'] as const;
export const UI_HEIGHT_VALUES = ['comfortable', 'short', 'compact'] as const;
export const UI_NAV_MODE_VALUES = ['bottom', 'rail'] as const;

export type UiSurface = (typeof UI_SURFACE_VALUES)[number];
export type UiOrientation = (typeof UI_ORIENTATION_VALUES)[number];
export type UiInputMode = (typeof UI_INPUT_VALUES)[number];
export type UiHeightClass = (typeof UI_HEIGHT_VALUES)[number];
export type UiNavMode = (typeof UI_NAV_MODE_VALUES)[number];

export interface UiViewportMetrics {
  width: number;
  height: number;
  coarsePointer: boolean;
  hoverNone: boolean;
  hostIsTv: boolean;
}

export interface UiSurfaceState {
  surface: UiSurface;
  orientation: UiOrientation;
  inputMode: UiInputMode;
  heightClass: UiHeightClass;
  navMode: UiNavMode;
  width: number;
  height: number;
}

export const UI_SURFACE_BREAKPOINTS = Object.freeze({
  small: 600,
  medium: 960,
  large: 1440,
});

const SHORT_HEIGHT_MAX = 500;
const COMPACT_HEIGHT_MAX = 430;

function normalizeViewportSize(value: number | undefined): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return 0;
  }

  return Math.max(0, Math.round(value));
}

export function classifyUiSurface(width: number): UiSurface {
  if (width < UI_SURFACE_BREAKPOINTS.small) {
    return 'small';
  }

  if (width < UI_SURFACE_BREAKPOINTS.medium) {
    return 'medium';
  }

  if (width < UI_SURFACE_BREAKPOINTS.large) {
    return 'large';
  }

  return 'xlarge';
}

export function classifyUiHeight(height: number): UiHeightClass {
  if (height <= COMPACT_HEIGHT_MAX) {
    return 'compact';
  }

  if (height <= SHORT_HEIGHT_MAX) {
    return 'short';
  }

  return 'comfortable';
}

export function resolveUiSurfaceState(metrics: UiViewportMetrics): UiSurfaceState {
  const width = normalizeViewportSize(metrics.width);
  const height = normalizeViewportSize(metrics.height);
  const orientation: UiOrientation = width > 0 && width >= height ? 'landscape' : 'portrait';
  const inputMode: UiInputMode = metrics.coarsePointer || metrics.hoverNone ? 'coarse' : 'fine';
  const surface = classifyUiSurface(width);
  const heightClass = classifyUiHeight(height);

  const navMode: UiNavMode = metrics.hostIsTv
    ? 'rail'
    : surface === 'small' || surface === 'medium'
      ? 'bottom'
      : 'rail';

  return {
    surface,
    orientation,
    inputMode,
    heightClass,
    navMode,
    width,
    height,
  };
}

function matches(win: Window, query: string): boolean {
  if (typeof win.matchMedia !== 'function') {
    return false;
  }

  return win.matchMedia(query).matches;
}

export function readUiViewportMetrics(win: Window, environment?: Pick<HostEnvironment, 'isTv'>): UiViewportMetrics {
  const visualViewport = win.visualViewport;
  const width = normalizeViewportSize(visualViewport?.width ?? win.innerWidth);
  const height = normalizeViewportSize(visualViewport?.height ?? win.innerHeight);

  return {
    width,
    height,
    coarsePointer: matches(win, '(pointer: coarse)') || matches(win, '(any-pointer: coarse)'),
    hoverNone: matches(win, '(hover: none)') || matches(win, '(any-hover: none)'),
    hostIsTv: environment?.isTv === true,
  };
}

export function resolveUiSurface(win: Window, environment?: Pick<HostEnvironment, 'isTv'>): UiSurfaceState {
  return resolveUiSurfaceState(readUiViewportMetrics(win, environment));
}

export function applyUiSurfaceToDocument(state: UiSurfaceState, root: HTMLElement | null | undefined): void {
  if (!root) {
    return;
  }

  root.dataset.uiSurface = state.surface;
  root.dataset.uiOrientation = state.orientation;
  root.dataset.uiInput = state.inputMode;
  root.dataset.uiHeight = state.heightClass;
  root.dataset.uiNavMode = state.navMode;
}
