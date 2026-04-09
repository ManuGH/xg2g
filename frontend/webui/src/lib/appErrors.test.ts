import { describe, expect, it } from 'vitest';
import { toAppError } from './appErrors';

describe('appErrors live problem mapping', () => {
  it('maps missing live truth to stable user-facing copy', () => {
    const appError = toAppError({
      type: '/problems/live/missing_scan_truth',
      title: 'Live media truth missing',
      status: 503,
      requestId: 'req-live-missing',
      code: 'UNAVAILABLE',
      retryAfterSeconds: 5,
      truthState: 'unverified',
      truthReason: 'missing_scan_truth',
      truthOrigin: 'live_unverified'
    });

    expect(appError).toEqual(expect.objectContaining({
      title: 'Live stream is still being checked',
      detail: 'The receiver has not published verified media details for this channel yet. Try again in about 5 seconds.',
      status: 503,
      code: 'UNAVAILABLE',
      requestId: 'req-live-missing',
      retryable: true,
      severity: 'warning'
    }));
  });

  it('maps inactive event feed to degraded live copy', () => {
    const appError = toAppError({
      type: '/problems/live/inactive_event_feed',
      title: 'Live event feed inactive',
      status: 503,
      requestId: 'req-live-inactive',
      code: 'UNAVAILABLE',
      truthState: 'inactive_event_feed',
      truthReason: 'inactive_event_feed',
      truthOrigin: 'live_unverified'
    });

    expect(appError).toEqual(expect.objectContaining({
      title: 'Live stream is unavailable',
      detail: 'The receiver is not publishing active media details for this channel right now. Please try again shortly.',
      retryable: true,
      severity: 'warning'
    }));
  });
});
