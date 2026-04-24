export const UI_SCALE_STORAGE_KEY = 'xg2g-ui-scale';

export const UI_SCALE_VALUES = ['compact', 'normal', 'large'] as const;

export type UiScale = (typeof UI_SCALE_VALUES)[number];

const DEFAULT_UI_SCALE: UiScale = 'normal';

export function parseUiScale(value: string | null | undefined): UiScale | null {
  if (!value) {
    return null;
  }

  return UI_SCALE_VALUES.includes(value as UiScale) ? (value as UiScale) : null;
}

export function getDefaultUiScale(): UiScale {
  return DEFAULT_UI_SCALE;
}

export function readStoredUiScale(storage: Pick<Storage, 'getItem'> | null | undefined): UiScale {
  const stored = parseUiScale(storage?.getItem(UI_SCALE_STORAGE_KEY));
  return stored ?? DEFAULT_UI_SCALE;
}

export function applyUiScaleToDocument(scale: UiScale, root: HTMLElement | null | undefined): void {
  if (!root) {
    return;
  }

  root.dataset.uiScale = scale;
}
