import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import ErrorPanel from '../src/components/ErrorPanel';

describe('ErrorPanel error catalog metadata', () => {
  it('renders structured severity, code, operator hint and runbook data', () => {
    render(
      <ErrorPanel
        error={{
          title: 'Transcode stalled - no progress detected',
          detail: 'The FFmpeg watchdog terminated the session after progress stopped.',
          status: 503,
          retryable: true,
          code: 'TRANSCODE_STALLED',
          severity: 'critical',
          operatorHint: 'Inspect the FFmpeg watchdog logs before retrying playback.',
          runbookUrl: 'docs/ops/OBSERVABILITY.md',
        }}
      />
    );

    expect(screen.getByText('Critical')).toBeInTheDocument();
    expect(screen.getByText('Error 503')).toBeInTheDocument();
    expect(screen.getByText(/Code: TRANSCODE_STALLED/)).toBeInTheDocument();
    expect(screen.getByText('Operator hint')).toBeInTheDocument();
    expect(screen.getByText('Inspect the FFmpeg watchdog logs before retrying playback.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Runbook' })).toHaveAttribute('href', 'docs/ops/OBSERVABILITY.md');
  });
});
