import { describe, expect, it } from 'vitest';
import {
  buildPlaybackAdvisorySignal,
  classifyNormalizedContractFailure,
  classifyPlaybackFailure,
} from './playbackFailureSemantics';

describe('playbackFailureSemantics', () => {
  it('classifies authentication failures as blocked and terminal', () => {
    const failure = classifyPlaybackFailure({
      appError: {
        title: 'Authentication required',
        status: 401,
        retryable: false,
      },
      source: 'backend',
    });

    expect(failure.class).toBe('auth');
    expect(failure.code).toBe('AUTH_REQUIRED');
    expect(failure.policyImpact).toBe('blocked');
    expect(failure.retryable).toBe(false);
    expect(failure.terminal).toBe(true);
  });

  it('classifies session failures as recoverable without collapsing them into media', () => {
    const failure = classifyPlaybackFailure({
      appError: {
        title: 'Session expired',
        status: 410,
        retryable: true,
      },
      source: 'native-host',
      code: 'SESSION_EXPIRED',
    });

    expect(failure.class).toBe('session');
    expect(failure.code).toBe('SESSION_EXPIRED');
    expect(failure.recoverable).toBe(true);
    expect(failure.policyImpact).toBe('blocked');
    expect(failure.terminal).toBe(false);
  });

  it('keeps contract failures fail-closed', () => {
    const failure = classifyNormalizedContractFailure({
      kind: 'contract',
      code: 'missing_output_url',
      message: 'Backend decision missing selectedOutputUrl.',
      retryable: false,
      terminal: true,
    });

    expect(failure.class).toBe('contract');
    expect(failure.code).toBe('missing_output_url');
    expect(failure.retryable).toBe(false);
    expect(failure.policyImpact).toBe('blocked');
    expect(failure.terminal).toBe(true);
  });

  it('keeps media failures in the media class', () => {
    const failure = classifyPlaybackFailure({
      appError: {
        title: 'Decode failed',
        retryable: true,
        code: 'MEDIA_DECODE_ERROR',
      },
      source: 'media-element',
    });

    expect(failure.class).toBe('media');
    expect(failure.code).toBe('MEDIA_DECODE_ERROR');
    expect(failure.retryable).toBe(true);
    expect(failure.policyImpact).toBe('blocked');
  });

  it('keeps advisory warnings non-terminal and policy-free', () => {
    const advisory = buildPlaybackAdvisorySignal({
      code: 'legacy_seekable_field',
      message: 'Using deprecated seekable field as the seekability source of truth.',
      source: 'normalizer',
    });

    expect(advisory.class).toBe('advisory');
    expect(advisory.policyImpact).toBe('none');
    expect(advisory.terminal).toBe(false);
    expect(advisory.retryable).toBe(false);
  });
});
