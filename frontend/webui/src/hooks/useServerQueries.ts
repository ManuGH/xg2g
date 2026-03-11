// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

/**
 * TanStack Query Hooks for Server-State Management
 *
 * Phase 1: State-of-the-Art 2026 Server-State Layer
 *
 * Design Principles:
 * - Backend = Single Source of Truth
 * - UI only caches/refetches (kein Client-Side Matching)
 * - Query Keys: ['v3', resource, ...params]
 * - Polling intervals gemäß xg2g-Semantik
 */

import { useQuery } from '@tanstack/react-query';
import {
  getSystemHealth,
  getReceiverCurrent,
  getStreams,
  getDvrStatus,
  getLogs,
  type SystemHealth,
  type CurrentServiceInfo,
  type StreamSession,
  type LogEntry
} from '../client-ts';

/**
 * Query Keys (versioniert, strukturiert)
 */
export const queryKeys = {
  health: ['v3', 'system', 'health'] as const,
  receiverCurrent: ['v3', 'receiver', 'current'] as const,
  streams: ['v3', 'streams'] as const,
  dvrStatus: ['v3', 'dvr', 'status'] as const,
  logs: (limit?: number) => ['v3', 'logs', { limit }] as const,
};

/**
 * Typed result unwrapper for generated API client results.
 *
 * Replaces per-hook `@ts-ignore` patterns with a single, type-safe helper.
 * Dispatches 'auth-required' for 401 responses to trigger the auth overlay.
 */
function unwrapQueryResult<T>(
  result: { data?: T; error?: unknown; response?: { status?: number } },
  { silent = false }: { silent?: boolean } = {}
): T {
  if (result.error) {
    const status = (result.response as { status?: number } | undefined)?.status;
    if (status === 401) {
      window.dispatchEvent(new Event('auth-required'));
      throw new Error('Authentication required');
    }

    // Extract message from error object (may be ApiError or ProblemDetails)
    const errObj = result.error as Record<string, unknown> | undefined;
    const message =
      (errObj && typeof errObj.message === 'string' ? errObj.message : null) ??
      (errObj && typeof errObj.title === 'string' ? errObj.title : null) ??
      'Request failed';

    if (silent) {
      return undefined as unknown as T;
    }

    throw new Error(message);
  }

  return result.data as T;
}

/**
 * useSystemHealth - System Health Status
 *
 * Polling: 10s (Dashboard Banner, Receiver/EPG Status)
 * staleTime: 8s (frisch genug für UI, aber refetch bei unmount/mount)
 */
export function useSystemHealth() {
  return useQuery({
    queryKey: queryKeys.health,
    queryFn: async () => {
      const result = await getSystemHealth();
      return unwrapQueryResult<SystemHealth>(result);
    },
    refetchInterval: 10_000, // 10s polling
    staleTime: 8_000, // 8s frisch
  });
}

/**
 * useReceiverCurrent - Live TV Info (HDMI Output)
 *
 * Polling: 10s (Dashboard Live TV Card)
 * staleTime: 8s
 */
export function useReceiverCurrent() {
  return useQuery({
    queryKey: queryKeys.receiverCurrent,
    queryFn: async () => {
      const result = await getReceiverCurrent();
      return unwrapQueryResult<CurrentServiceInfo | null>(result, { silent: true }) ?? null;
    },
    refetchInterval: 10_000, // 10s polling
    staleTime: 8_000,
  });
}

/**
 * useStreams - Active Stream Sessions
 *
 * Polling: 5s (Dashboard Stream Cards)
 * staleTime: 4s
 */
export function useStreams() {
  return useQuery({
    queryKey: queryKeys.streams,
    queryFn: async () => {
      const result = await getStreams();
      return unwrapQueryResult<StreamSession[]>(result, { silent: true }) ?? [];
    },
    refetchInterval: 5_000, // 5s polling
    staleTime: 4_000,
  });
}

/**
 * useDvrStatus - Recording Status
 *
 * Polling: 30s (Dashboard Recording Badge)
 * staleTime: 25s
 */
export function useDvrStatus() {
  return useQuery({
    queryKey: queryKeys.dvrStatus,
    queryFn: async () => {
      const result = await getDvrStatus();
      return unwrapQueryResult<{ isRecording?: boolean; serviceName?: string } | null>(result, { silent: true }) ?? null;
    },
    refetchInterval: 30_000, // 30s polling
    staleTime: 25_000,
  });
}

/**
 * useLogs - Recent Log Entries
 *
 * Polling: disabled (nur on-demand via refetch)
 * staleTime: 30s
 */
export function useLogs(limit: number = 5) {
  return useQuery({
    queryKey: queryKeys.logs(limit),
    queryFn: async () => {
      const result = await getLogs();
      const logs = unwrapQueryResult<LogEntry[]>(result);
      return (logs || []).slice(0, limit);
    },
    refetchInterval: false, // kein auto-refetch
    staleTime: 30_000,
  });
}
