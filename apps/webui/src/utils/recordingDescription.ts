// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

/**
 * Strips internal xg2g rule tags (e.g. "[xg2g-rule:323a63b0-f4df-4fa3-b1d3-ea2eb3fae73e] ")
 * from user-facing recording descriptions.
 * The backend persists this tag on timer/recording descriptions to track series-rule
 * ownership, but it must never be shown to household users in the UI.
 */
export function formatRecordingDescription(description?: string | null): string {
  if (!description) return '';
  return description.replace(/^\[xg2g-rule:[^\]]+\]\s*/i, '').trim();
}
