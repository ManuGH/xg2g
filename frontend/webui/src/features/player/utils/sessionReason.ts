import type { TFunction } from 'i18next';

// Backend session-failure `reason` codes the player may surface. Some are NOT in
// the OpenAPI reason enum (e.g. R_UPSTREAM_SCRAMBLED is passed through from the
// domain ReasonCode set), so this set — not the spec — is the source of truth for
// what we translate. Codes outside it fall back gracefully (see below).
//
// Keep this in sync with `backend/internal/domain/session/model/enums.go`
// ReasonCode constants that can reach a terminal session response.
const TRANSLATED_REASONS = new Set<string>([
  'R_BAD_REQUEST',
  'R_CANCELLED',
  'R_CLIENT_STOP',
  'R_DEADLINE_EXCEEDED',
  'R_FFMPEG_START_FAILED',
  'R_IDLE_TIMEOUT',
  'R_INTERNAL_INVARIANT_BREACH',
  'R_INVARIANT_VIOLATION',
  'R_LEASE_BUSY',
  'R_LEASE_EXPIRED',
  'R_NOT_FOUND',
  'R_PACKAGER_FAILED',
  'R_PIPELINE_START_FAILED',
  'R_PROCESS_ENDED',
  'R_RECORDING_NOT_READY',
  'R_TUNE_FAILED',
  'R_TUNE_TIMEOUT',
  'R_UNKNOWN',
  'R_UPSTREAM_CORRUPT',
  'R_UPSTREAM_SCRAMBLED',
]);

/**
 * Translate a backend session failure into human-readable, localized text.
 *
 * Maps known `reason` codes to `player.reason.<CODE>` i18n strings; for codes we
 * don't translate, falls back to the server-supplied `reasonDetail` (English
 * free-text), then the raw code, then a generic message — so the user never sees
 * a bare machine token like "R_UPSTREAM_SCRAMBLED".
 */
export function translatePlaybackReason(
  reason: string | undefined | null,
  reasonDetail: string | undefined | null,
  t: TFunction,
): string {
  const code = (reason ?? '').trim();
  if (code && TRANSLATED_REASONS.has(code)) {
    return t(`player.reason.${code}`);
  }
  const detail = (reasonDetail ?? '').trim();
  if (detail) {
    return detail;
  }
  if (code) {
    return code;
  }
  return t('player.reason.unknown');
}
