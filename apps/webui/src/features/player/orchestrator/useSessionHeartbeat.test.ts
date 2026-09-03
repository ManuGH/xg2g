import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useSessionHeartbeat } from './useSessionHeartbeat';

describe('useSessionHeartbeat', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('does nothing when sessionId or heartbeatInterval is null', () => {
    const fetchWithRecoveredSessionCookie = vi.fn();
    const sessionIdRef = { current: null };
    const videoRef = { current: null };

    const { result } = renderHook(() =>
      useSessionHeartbeat({
        sessionId: null,
        sessionIdRef,
        heartbeatInterval: null,
        apiBase: 'http://localhost/api',
        authHeaders: () => ({}),
        fetchWithRecoveredSessionCookie,
        refreshSessionSnapshot: vi.fn(),
        clearSessionLeaseState: vi.fn(),
        reportPlaybackFailure: vi.fn(),
        clearPlaybackFailure: vi.fn(),
        setPlaybackMode: vi.fn(),
        videoRef,
        t: ((k: string) => k) as any,
      })
    );

    expect(result.current.connectionLost).toBe(false);
    expect(result.current.leaseExpiresAt).toBeNull();
    expect(fetchWithRecoveredSessionCookie).not.toHaveBeenCalled();
  });

  it('sends heartbeats and extends lease on successful 200 response', async () => {
    const mockResponse = {
      status: 200,
      json: vi.fn().mockResolvedValue({
        acknowledged: true,
        leaseExpiresAt: '2026-09-03T23:00:00Z',
        sessionId: 'sess-123',
      }),
    };
    const fetchWithRecoveredSessionCookie = vi.fn().mockImplementation(() => {
      return Promise.resolve({ response: mockResponse, recovered: false });
    });
    const sessionIdRef = { current: 'sess-123' };
    const videoRef = { current: null };
    const refreshSessionSnapshot = vi.fn().mockResolvedValue(null);

    const { result } = renderHook(() =>
      useSessionHeartbeat({
        sessionId: 'sess-123',
        sessionIdRef,
        heartbeatInterval: 2, // 2 seconds
        apiBase: 'http://localhost/api',
        authHeaders: () => ({}),
        fetchWithRecoveredSessionCookie,
        refreshSessionSnapshot,
        clearSessionLeaseState: vi.fn(),
        reportPlaybackFailure: vi.fn(),
        clearPlaybackFailure: vi.fn(),
        setPlaybackMode: vi.fn(),
        videoRef,
        t: ((k: string) => k) as any,
      })
    );

    // Fast-forward to trigger heartbeat
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });

    expect(fetchWithRecoveredSessionCookie).toHaveBeenCalledTimes(1);
    expect(result.current.leaseExpiresAt).toBe('2026-09-03T23:00:00Z');
    expect(result.current.connectionLost).toBe(false);
  });

  it('flags connectionLost after consecutive network failures', async () => {
    const fetchWithRecoveredSessionCookie = vi.fn().mockRejectedValue(new Error('Network offline'));
    const sessionIdRef = { current: 'sess-123' };
    const videoRef = { current: null };
    const authHeaders = vi.fn().mockReturnValue({});
    const refreshSessionSnapshot = vi.fn();
    const clearSessionLeaseState = vi.fn();
    const reportPlaybackFailure = vi.fn();
    const clearPlaybackFailure = vi.fn();
    const setPlaybackMode = vi.fn();
    const t = ((k: string) => k) as any;

    const { result } = renderHook(() =>
      useSessionHeartbeat({
        sessionId: 'sess-123',
        sessionIdRef,
        heartbeatInterval: 1, // 1 second
        apiBase: 'http://localhost/api',
        authHeaders,
        fetchWithRecoveredSessionCookie,
        refreshSessionSnapshot,
        clearSessionLeaseState,
        reportPlaybackFailure,
        clearPlaybackFailure,
        setPlaybackMode,
        videoRef,
        t,
      })
    );

    // First failure
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(fetchWithRecoveredSessionCookie).toHaveBeenCalledTimes(1);
    expect(result.current.connectionLost).toBe(false);

    // Second failure (retry interval is 5000ms)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    await act(async () => {});
    expect(fetchWithRecoveredSessionCookie).toHaveBeenCalledTimes(2);
    expect(result.current.connectionLost).toBe(true);
  });
});
