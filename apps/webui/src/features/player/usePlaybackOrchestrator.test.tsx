import { createRef, useRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

function Harness({ props }: { props: V3PlayerProps }) {
  const containerRef = createRef<HTMLDivElement>();
  const videoRef = createRef<VideoElementRef>();
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = createRef<HTMLButtonElement>();
  const { viewState, actions } = usePlaybackOrchestrator(props, {
    containerRef,
    videoRef,
    hlsRef,
    resumePrimaryActionRef,
  });

  return (
    <div>
      <div data-testid="show-service-input">{String(viewState.showServiceInput)}</div>
      <div data-testid="show-manual-start">{String(viewState.showManualStartButton)}</div>
      <div data-testid="service-ref">{viewState.serviceRef}</div>
      <div data-testid="duration-seconds">{String(viewState.playback.durationSeconds)}</div>
      <button onClick={() => actions.updateServiceRef('1:0:1:AA')} type="button">
        update-service-ref
      </button>
    </div>
  );
}

describe('usePlaybackOrchestrator', () => {
  it('derives the manual-start view state when no explicit playback source exists', () => {
    render(<Harness props={{ autoStart: false } as unknown as V3PlayerProps} />);

    expect(screen.getByTestId('show-service-input')).toHaveTextContent('true');
    expect(screen.getByTestId('show-manual-start')).toHaveTextContent('true');
    expect(screen.getByTestId('duration-seconds')).toHaveTextContent('null');
  });

  it('updates the exposed service-ref view state through explicit controller actions', () => {
    render(<Harness props={{ autoStart: false } as unknown as V3PlayerProps} />);

    fireEvent.click(screen.getByRole('button', { name: 'update-service-ref' }));

    expect(screen.getByTestId('service-ref')).toHaveTextContent('1:0:1:AA');
  });

  it('suppresses manual-start affordances for explicit recording playback props', () => {
    render(<Harness props={{ autoStart: false, recordingId: 'rec-123' }} />);

    expect(screen.getByTestId('show-service-input')).toHaveTextContent('false');
    expect(screen.getByTestId('show-manual-start')).toHaveTextContent('false');
  });
});
