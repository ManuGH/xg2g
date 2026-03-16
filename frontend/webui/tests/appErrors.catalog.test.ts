import { afterEach, describe, expect, it } from 'vitest';
import { ClientRequestError } from '../src/lib/clientWrapper';
import { resetErrorCatalog, setErrorCatalog } from '../src/lib/errorCatalog';
import { toAppError } from '../src/lib/appErrors';

describe('appErrors error catalog wiring', () => {
  afterEach(() => {
    resetErrorCatalog();
  });

  it('enriches coded API errors from the catalog', () => {
    setErrorCatalog([
      {
        code: 'TRANSCODE_STALLED',
        problemType: 'https://xg2g.dev/problems/transcode-stalled',
        title: 'Transcode stalled - no progress detected',
        description: 'The FFmpeg watchdog terminated the session after progress stopped.',
        operatorHint: 'Inspect the FFmpeg watchdog logs before retrying playback.',
        severity: 'critical',
        retryable: true,
        runbookUrl: 'docs/ops/OBSERVABILITY.md',
      },
    ]);

    const error = toAppError(new ClientRequestError({
      status: 503,
      code: 'TRANSCODE_STALLED',
      requestId: 'req-stall-1',
    }));

    expect(error).toMatchObject({
      code: 'TRANSCODE_STALLED',
      requestId: 'req-stall-1',
      title: 'Transcode stalled - no progress detected',
      detail: 'The FFmpeg watchdog terminated the session after progress stopped.',
      operatorHint: 'Inspect the FFmpeg watchdog logs before retrying playback.',
      severity: 'critical',
      retryable: true,
      runbookUrl: 'docs/ops/OBSERVABILITY.md',
    });
  });

  it('keeps explicit fallback detail over the catalog description', () => {
    setErrorCatalog([
      {
        code: 'HTTPS_REQUIRED',
        problemType: 'https://xg2g.dev/problems/https-required',
        title: 'HTTPS required',
        description: 'Catalog detail',
        operatorHint: 'Retry over HTTPS.',
        severity: 'warning',
        retryable: false,
      },
    ]);

    const error = toAppError(
      new ClientRequestError({
        status: 400,
        code: 'HTTPS_REQUIRED',
      }),
      {
        fallbackDetail: 'Session exchange requires HTTPS in this environment.',
      }
    );

    expect(error.detail).toBe('Session exchange requires HTTPS in this environment.');
    expect(error.operatorHint).toBe('Retry over HTTPS.');
    expect(error.severity).toBe('warning');
  });
});
