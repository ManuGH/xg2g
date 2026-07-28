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

  it('maps stale live truth to a refresh message even when the problem type is generic', () => {
    const appError = toAppError({
      type: '/problems/live/unverified',
      title: 'Live media truth stale',
      detail: 'Raw backend detail should not leak into the UI',
      status: 503,
      requestId: 'req-live-stale',
      code: 'UNAVAILABLE',
      retryAfterSeconds: 5,
      truthState: 'unverified',
      truthReason: 'stale_scan_truth',
      truthOrigin: 'live_unverified'
    });

    expect(appError).toEqual(expect.objectContaining({
      title: 'Live stream is being refreshed',
      detail: 'xg2g has older media details for this channel and is waiting for a fresh confirmation from the receiver. Try again in about 5 seconds.',
      status: 503,
      code: 'UNAVAILABLE',
      requestId: 'req-live-stale',
      retryable: true,
      severity: 'warning'
    }));
  });

  it('maps generic live unverified problems to stable user-facing copy', () => {
    const appError = toAppError({
      type: '/problems/live/unverified',
      title: 'Live media truth unavailable',
      status: 503,
      requestId: 'req-live-generic',
      code: 'UNAVAILABLE'
    });

    expect(appError).toEqual(expect.objectContaining({
      title: 'Live stream is being verified',
      detail: 'xg2g is waiting for verified media details for this channel. Please try again shortly.',
      status: 503,
      code: 'UNAVAILABLE',
      requestId: 'req-live-generic',
      retryable: true,
      severity: 'warning'
    }));
  });
});
