// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createPlaybackHeartbeatRuntime,
  HEARTBEAT_RETRY_INTERVAL_MS,
} from './playbackHeartbeatRuntime';
import type { LiveSessionTransport } from './liveSessionTransport';

function createMockTransport(overrides: Partial<LiveSessionTransport> = {}): LiveSessionTransport {
  return {
    fetchStreamInfo: vi.fn(),
    postStartIntent: vi.fn(),
    waitForReady: vi.fn(),
    postStopIntent: vi.fn(),
    postHeartbeat: vi.fn(),
    fetchSessionSnapshot: vi.fn(),
    ...overrides,
  };
}

describe('PlaybackHeartbeatRuntime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  describe('Activation and Cadence', () => {
    it('does not start heartbeat loop before start() is explicitly called', () => {
      const transport = createMockTransport();
      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure: vi.fn(),
      });

      vi.advanceTimersByTime(20_000);
      expect(transport.postHeartbeat).not.toHaveBeenCalled();
      expect(runtime.isSupervising()).toBe(false);
    });

    it('schedules the first heartbeat at the negotiated interval', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:00Z', sessionId: 'sess-1' },
          headers: new Headers(),
        }),
      });
      const onLeaseUpdated = vi.fn();
      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 15,
        initialLeaseExpiresAt: '2026-09-09T11:59:45Z',
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
      });

      runtime.start();
      expect(runtime.isSupervising()).toBe(true);

      // Advance right before 15s
      vi.advanceTimersByTime(14_900);
      expect(transport.postHeartbeat).not.toHaveBeenCalled();

      // Advance to 15s
      await vi.advanceTimersByTimeAsync(100);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);
      expect(onLeaseUpdated).toHaveBeenCalledWith({
        leaseExpiresAt: '2026-09-09T12:00:00Z',
        connectionLost: false,
      });
    });

    it('does not schedule an immediate loop on invalid, 0, or negative intervals', () => {
      const transport = createMockTransport();
      for (const badInterval of [0, -5, NaN, Infinity]) {
        const runtime = createPlaybackHeartbeatRuntime({
          sessionId: 'sess-1',
          heartbeatIntervalSeconds: badInterval,
          transport,
          playbackEpoch: 1,
          sessionEpoch: 1,
          onLeaseUpdated: vi.fn(),
          onFailure: vi.fn(),
        });
        runtime.start();
        vi.advanceTimersByTime(30_000);
        expect(transport.postHeartbeat).not.toHaveBeenCalled();
        expect(runtime.isSupervising()).toBe(false);
      }
    });

    it('prevents overlapping in-flight heartbeat requests', async () => {
      let resolveBeat!: (val: any) => void;
      const slowBeatPromise = new Promise((resolve) => {
        resolveBeat = resolve;
      });

      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(() => slowBeatPromise),
      });

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure: vi.fn(),
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);

      // Advancing time further while the first is pending does not trigger a second beat
      await vi.advanceTimersByTimeAsync(10_000);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);

      // Resolving the first allows next to schedule
      resolveBeat({
        status: 200,
        data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:10Z', sessionId: 'sess-1' },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(0);

      // Next interval
      await vi.advanceTimersByTimeAsync(10_000);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(2);
    });
  });

  describe('Acknowledgement and Lease Extension', () => {
    it('accepts valid 200 acknowledgement with matching sessionId and non-empty leaseExpiresAt', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:30Z', sessionId: 'sess-1' },
          headers: new Headers(),
        }),
      });
      const onLeaseUpdated = vi.fn();
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onLeaseUpdated).toHaveBeenCalledWith({
        leaseExpiresAt: '2026-09-09T12:00:30Z',
        connectionLost: false,
      });
      expect(onFailure).not.toHaveBeenCalled();
      expect(runtime.getLeaseExpiresAt()).toBe('2026-09-09T12:00:30Z');
      expect(runtime.isConnectionLost()).toBe(false);
    });

    it('does not require expiry timestamp to strictly increase on every response', async () => {
      const sameExpiry = '2026-09-09T12:00:30Z';
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: sameExpiry, sessionId: 'sess-1' },
          headers: new Headers(),
        }),
      });
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        initialLeaseExpiresAt: sameExpiry,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onLeaseUpdated).toHaveBeenCalledWith({
        leaseExpiresAt: sameExpiry,
        connectionLost: false,
      });
    });

    it('rejects HTTP 200 with acknowledged=false as INVALID_HEARTBEAT_CONTRACT', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: false, leaseExpiresAt: '2026-09-09T12:00:30Z', sessionId: 'sess-1' },
          headers: new Headers(),
        }),
      });
      const onLeaseUpdated = vi.fn();
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(runtime.isSupervising()).toBe(false);
      expect(onLeaseUpdated).toHaveBeenCalledWith({ leaseExpiresAt: null, connectionLost: false });
      expect(onFailure).toHaveBeenCalledWith(
        expect.objectContaining({
          code: 'INVALID_HEARTBEAT_CONTRACT',
          status: 502,
          failureClass: 'session',
          retryable: true,
          recoverable: true,
          terminal: false,
          pauseMedia: true,
        }),
      );
    });

    it('rejects HTTP 200 with missing leaseExpiresAt as INVALID_HEARTBEAT_CONTRACT', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, sessionId: 'sess-1' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onFailure).toHaveBeenCalledWith(
        expect.objectContaining({
          code: 'INVALID_HEARTBEAT_CONTRACT',
          status: 502,
        }),
      );
    });

    it('rejects HTTP 200 with mismatched sessionId as INVALID_HEARTBEAT_CONTRACT', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:30Z', sessionId: 'wrong-sess' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onFailure).toHaveBeenCalledWith(
        expect.objectContaining({
          code: 'INVALID_HEARTBEAT_CONTRACT',
          status: 502,
        }),
      );
    });
  });

  describe('Reachability and Errors', () => {
    it('handles first reachability failure by scheduling retry after 5s without setting connectionLost', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockRejectedValue(new Error('Network drop')),
      });
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        initialLeaseExpiresAt: '2026-09-09T12:00:00Z',
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(runtime.getConsecutiveFailures()).toBe(1);
      expect(runtime.isConnectionLost()).toBe(false);
      expect(onLeaseUpdated).toHaveBeenCalledWith({
        leaseExpiresAt: '2026-09-09T12:00:00Z',
        connectionLost: false,
      });

      // Advance 5s (retry interval)
      await vi.advanceTimersByTimeAsync(HEARTBEAT_RETRY_INTERVAL_MS);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(2);
    });

    it('sets connectionLost=true upon reaching 2 consecutive reachability failures', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockRejectedValue(new Error('Network drop')),
      });
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        initialLeaseExpiresAt: '2026-09-09T12:00:00Z',
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
      });

      runtime.start();
      // First failure at t=10s
      await vi.advanceTimersByTimeAsync(10_000);
      expect(runtime.getConsecutiveFailures()).toBe(1);
      expect(runtime.isConnectionLost()).toBe(false);

      // Second failure at t=15s (after 5s retry)
      await vi.advanceTimersByTimeAsync(HEARTBEAT_RETRY_INTERVAL_MS);
      expect(runtime.getConsecutiveFailures()).toBe(2);
      expect(runtime.isConnectionLost()).toBe(true);
      expect(onLeaseUpdated).toHaveBeenLastCalledWith({
        leaseExpiresAt: '2026-09-09T12:00:00Z',
        connectionLost: true,
      });
    });

    it('recovers from connectionLost when a subsequent beat returns 200 OK and restores cadence', async () => {
      let attempt = 0;
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(() => {
          attempt += 1;
          if (attempt <= 2) {
            return Promise.reject(new Error('Connection offline'));
          }
          return Promise.resolve({
            status: 200,
            data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:01:00Z', sessionId: 'sess-1' },
            headers: new Headers(),
          });
        }),
      });
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        initialLeaseExpiresAt: '2026-09-09T12:00:00Z',
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
      });

      runtime.start();
      // t=10s: failure 1
      await vi.advanceTimersByTimeAsync(10_000);
      // t=15s: failure 2 -> connectionLost
      await vi.advanceTimersByTimeAsync(HEARTBEAT_RETRY_INTERVAL_MS);
      expect(runtime.isConnectionLost()).toBe(true);

      // t=20s: attempt 3 succeeds!
      await vi.advanceTimersByTimeAsync(HEARTBEAT_RETRY_INTERVAL_MS);
      expect(runtime.getConsecutiveFailures()).toBe(0);
      expect(runtime.isConnectionLost()).toBe(false);
      expect(onLeaseUpdated).toHaveBeenLastCalledWith({
        leaseExpiresAt: '2026-09-09T12:01:00Z',
        connectionLost: false,
      });

      // Next schedule should be at negotiated interval (10s), not retry interval (5s)
      transport.postHeartbeat = vi.fn().mockResolvedValue({
        status: 200,
        data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:01:10Z', sessionId: 'sess-1' },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(5_000);
      expect(transport.postHeartbeat).not.toHaveBeenCalled();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);
    });

    it('treats real HTTP 502 as a reachability retryable error (unlike contract error)', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 502,
          data: { error: 'Bad Gateway' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      // Real 502 should NOT trigger INVALID_HEARTBEAT_CONTRACT failure; it is unexpected status -> retry
      expect(onFailure).not.toHaveBeenCalled();
      expect(runtime.getConsecutiveFailures()).toBe(1);
      expect(runtime.isSupervising()).toBe(true);
    });

    it('handles HTTP 401 as terminal auth failure (pauses media and clears lease)', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 401,
          data: { error: 'Unauthorized' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(runtime.isSupervising()).toBe(false);
      expect(onLeaseUpdated).toHaveBeenCalledWith({ leaseExpiresAt: null, connectionLost: false });
      expect(onFailure).toHaveBeenCalledWith({
        code: 'SESSION_UNAUTHORIZED',
        status: 401,
        message: 'Session unauthorized (401)',
        failureClass: 'auth',
        retryable: false,
        recoverable: false,
        terminal: true,
        pauseMedia: true,
      });
    });

    it('handles HTTP 403 as terminal auth failure', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 403,
          data: { error: 'Forbidden' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onFailure).toHaveBeenCalledWith({
        code: 'SESSION_FORBIDDEN',
        status: 403,
        message: 'Session forbidden (403)',
        failureClass: 'auth',
        retryable: false,
        recoverable: false,
        terminal: true,
        pauseMedia: true,
      });
    });

    it('handles HTTP 404 as recoverable session failure', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 404,
          data: { error: 'Not Found' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onFailure).toHaveBeenCalledWith({
        code: 'SESSION_NOT_FOUND',
        status: 404,
        message: 'Session no longer exists.',
        failureClass: 'session',
        retryable: true,
        recoverable: true,
        terminal: false,
        pauseMedia: true,
      });
    });

    it('handles HTTP 410 as recoverable session failure', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 410,
          data: { error: 'Gone' },
          headers: new Headers(),
        }),
      });
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(onFailure).toHaveBeenCalledWith({
        code: 'SESSION_EXPIRED',
        status: 410,
        message: 'Session expired. Please restart.',
        failureClass: 'session',
        retryable: true,
        recoverable: true,
        terminal: false,
        pauseMedia: true,
      });
    });
  });

  describe('Bounded Operations & Cancellation', () => {
    it('aborts hung transport request when deadline is reached', async () => {
      let abortedSignal: AbortSignal | undefined;
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(({ signal }: { signal: AbortSignal }) => {
          abortedSignal = signal;
          return new Promise(() => {}); // never resolves
        }),
      });

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure: vi.fn(),
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);
      expect(transport.postHeartbeat).toHaveBeenCalled();
      expect(abortedSignal?.aborted).toBe(false);

      // Advance past timeout (10s request timeout)
      await vi.advanceTimersByTimeAsync(10_000);
      expect(abortedSignal?.aborted).toBe(true);
      expect(runtime.getConsecutiveFailures()).toBe(1);
    });

    it('stop() cleanly clears timer and aborts pending request without late callbacks', async () => {
      let resolveBeat!: (val: any) => void;
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(() => new Promise((r) => { resolveBeat = r; })),
      });
      const onLeaseUpdated = vi.fn();
      const onFailure = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);
      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);

      // Stop while request is pending
      runtime.stop();
      expect(runtime.isSupervising()).toBe(false);

      // Resolve delayed promise late
      resolveBeat({
        status: 200,
        data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:30Z', sessionId: 'sess-1' },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(10_000);

      // No lease update or failure should be emitted after stop
      expect(onLeaseUpdated).not.toHaveBeenCalled();
      expect(onFailure).not.toHaveBeenCalled();
    });
  });

  describe('Snapshot Refresh Integration', () => {
    it('refreshes session snapshot upon successful heartbeat acknowledgement', async () => {
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:10Z', sessionId: 'sess-1' },
          headers: new Headers(),
        }),
        fetchSessionSnapshot: vi.fn().mockResolvedValue({
          status: 200,
          data: { sessionId: 'sess-1', state: 'READY', leaseExpiresAt: '2026-09-09T12:00:15Z' },
          headers: new Headers(),
        }),
      });
      const onSessionSnapshot = vi.fn();
      const onLeaseUpdated = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated,
        onFailure: vi.fn(),
        onSessionSnapshot,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);
      expect(transport.fetchSessionSnapshot).toHaveBeenCalledWith({
        sessionId: 'sess-1',
        signal: expect.any(AbortSignal),
      });
      expect(onSessionSnapshot).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: 'sess-1', state: 'READY' }),
      );
      // Lease updated with snapshot's updated value
      expect(onLeaseUpdated).toHaveBeenCalledWith({
        leaseExpiresAt: '2026-09-09T12:00:15Z',
        connectionLost: false,
      });
    });

    it('drops late snapshot response after stop() without mutating state', async () => {
      let resolveSnapshot!: (val: any) => void;
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, leaseExpiresAt: '2026-09-09T12:00:10Z', sessionId: 'sess-1' },
          headers: new Headers(),
        }),
        fetchSessionSnapshot: vi.fn().mockImplementation(() => new Promise((r) => { resolveSnapshot = r; })),
      });
      const onSessionSnapshot = vi.fn();

      const runtime = createPlaybackHeartbeatRuntime({
        sessionId: 'sess-1',
        heartbeatIntervalSeconds: 10,
        transport,
        playbackEpoch: 1,
        sessionEpoch: 1,
        onLeaseUpdated: vi.fn(),
        onFailure: vi.fn(),
        onSessionSnapshot,
      });

      runtime.start();
      await vi.advanceTimersByTimeAsync(10_000);

      // Stop while snapshot is in-flight
      runtime.stop();

      // Resolve snapshot
      resolveSnapshot({
        status: 200,
        data: { sessionId: 'sess-1', state: 'READY', leaseExpiresAt: '2026-09-09T12:00:20Z' },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(100);

      expect(onSessionSnapshot).not.toHaveBeenCalled();
    });
  });
});
