import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { useRef } from 'react';
import { usePlaybackEngine } from '../src/features/player/usePlaybackEngine';
import { createEngineTimerRegistry } from '../src/features/player/orchestrator/engineTimerRegistry';
import type { PlaybackAttemptToken, PlaybackEngineEvent } from '../src/features/player/playbackEngineContract';
import type { VideoElementRef, HlsInstanceRef } from '../src/types/v3-player';

function TestPlayerHarness({
  eventSink,
  onController,
}: {
  eventSink: (event: PlaybackEngineEvent) => void;
  onController?: (controller: ReturnType<typeof usePlaybackEngine>) => void;
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const hlsRef = useRef<any>(null);
  const sessionIdRef = useRef<string | null>('session-test-1');
  const isTeardownRef = useRef<boolean>(false);
  const lastDecodedRef = useRef<number>(0);
  const playbackEpochRef = useRef<number>(1);
  const attemptTokenRef = useRef<PlaybackAttemptToken>({ epoch: 1, attemptId: 'attempt-1' });
  const timerRegistry = useRef(createEngineTimerRegistry()).current;

  const controller = usePlaybackEngine({
    videoRef: videoRef as any,
    hlsRef: hlsRef as any,
    sessionIdRef,
    isTeardownRef,
    lastDecodedRef,
    playbackEpochRef,
    attemptTokenRef,
    eventSink,
    timerRegistry,
    t: (key: string) => key,
    reportError: vi.fn(),
    waitForSessionReady: vi.fn().mockResolvedValue({ playbackUrl: 'http://test/stream.m3u8' }),
    shouldPreferNativeHls: () => false,
    setStats: vi.fn(),
  });

  if (onController) {
    onController(controller);
  }

  return (
    <div>
      <video
        ref={(el) => {
          if (el) {
            el.canPlayType = vi.fn().mockReturnValue('probably');
            (videoRef as any).current = el;
          }
        }}
        data-testid="test-video"
      />
    </div>
  );
}

describe('Stage 1B: TimerRegistry & Cancellation Regression Tests', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('atomically suppresses all pending timers on resetPlaybackEngine: no phantom events fire on vi.runAllTimers()', () => {
    const receivedEvents: PlaybackEngineEvent[] = [];
    const eventSink = vi.fn((event: PlaybackEngineEvent) => {
      receivedEvents.push(event);
    });

    let controller: ReturnType<typeof usePlaybackEngine> | undefined;
    render(
      <TestPlayerHarness
        eventSink={eventSink}
        onController={(c) => {
          controller = c;
        }}
      />
    );

    expect(controller).toBeDefined();

    // Start playback to schedule startGate, revealHold etc.
    act(() => {
      controller!.playHls('http://example.com/live/stream.m3u8', 'native');
    });

    // Reset playback immediately (simulating fast channel zap)
    act(() => {
      controller!.resetPlaybackEngine();
    });

    const eventsBeforeTimers = [...receivedEvents];

    // Exhaust all timers in the fake runtime
    act(() => {
      vi.runAllTimers();
    });

    // Invariant: No additional events fired after resetPlaybackEngine()
    expect(receivedEvents.length).toBe(eventsBeforeTimers.length);
  });

  it('suppresses all engine timers and observations when player unmounts', () => {
    const receivedEvents: PlaybackEngineEvent[] = [];
    const eventSink = vi.fn((event: PlaybackEngineEvent) => {
      receivedEvents.push(event);
    });

    let controller: ReturnType<typeof usePlaybackEngine> | undefined;
    const { unmount } = render(
      <TestPlayerHarness
        eventSink={eventSink}
        onController={(c) => {
          controller = c;
        }}
      />
    );

    act(() => {
      controller!.playHls('http://example.com/live/stream.m3u8', 'native');
    });

    // Unmount player during startup
    unmount();

    const countAtUnmount = receivedEvents.length;

    act(() => {
      vi.runAllTimers();
    });

    // Invariant: Unmount flushed all timers atomically
    expect(receivedEvents.length).toBe(countAtUnmount);
  });

  it('rapid sequential zapping does not leak old callbacks into new attempt', () => {
    const receivedEvents: PlaybackEngineEvent[] = [];
    const eventSink = vi.fn((event: PlaybackEngineEvent) => {
      receivedEvents.push(event);
    });

    let controller: ReturnType<typeof usePlaybackEngine> | undefined;
    render(
      <TestPlayerHarness
        eventSink={eventSink}
        onController={(c) => {
          controller = c;
        }}
      />
    );

    // Zap 5 times rapidly
    for (let i = 1; i <= 5; i++) {
      act(() => {
        controller!.playHls(`http://example.com/channel_${i}.m3u8`, 'native');
        vi.advanceTimersByTime(20);
        controller!.resetPlaybackEngine();
      });
    }

    // Now start the final channel
    act(() => {
      controller!.playHls('http://example.com/channel_final.m3u8', 'native');
    });

    // Clear event history up to final start
    receivedEvents.length = 0;

    act(() => {
      vi.runAllTimers();
    });

    // Ensure all received events are only from channel_final or valid
    for (const evt of receivedEvents) {
      expect(evt.attempt.attemptId).toBe('attempt-1');
    }
  });
});
