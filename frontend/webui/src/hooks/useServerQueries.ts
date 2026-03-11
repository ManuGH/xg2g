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

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  deleteRecording,
  getSystemConfig,
  getSystemHealth,
  getSystemInfo,
  getSystemScanStatus,
  getReceiverCurrent,
  getStreams,
  getDvrStatus,
  getLogs,
  getTimers,
  getDvrCapabilities,
  deleteTimer,
  getRecordings,
  triggerSystemScan,
  type AppConfig,
  type SystemHealth,
  type SystemInfoData,
  type CurrentServiceInfo,
  type StreamSession,
  type LogEntry,
  type TimerList,
  type DvrCapabilities,
  type RecordingResponse,
  type ScanStatus
} from '../client-ts';

/**
 * Query Keys (versioniert, strukturiert)
 */
export const queryKeys = {
  systemConfig: ['v3', 'system', 'config'] as const,
  health: ['v3', 'system', 'health'] as const,
  systemInfo: ['v3', 'system', 'info'] as const,
  systemScanStatus: ['v3', 'system', 'scan'] as const,
  receiverCurrent: ['v3', 'receiver', 'current'] as const,
  streams: ['v3', 'streams'] as const,
  dvrStatus: ['v3', 'dvr', 'status'] as const,
  dvrCapabilities: ['v3', 'dvr', 'capabilities'] as const,
  timers: ['v3', 'timers'] as const,
  recordings: (root: string = '', path: string = '') => ['v3', 'recordings', { root, path }] as const,
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
 * useSystemConfig - persisted backend configuration
 *
 * Polling: disabled
 * staleTime: 30s
 */
export function useSystemConfig() {
  return useQuery({
    queryKey: queryKeys.systemConfig,
    queryFn: async () => {
      const result = await getSystemConfig();
      return unwrapQueryResult<AppConfig | null>(result, { silent: true }) ?? null;
    },
    staleTime: 30_000,
  });
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
 * useSystemInfo - detailed receiver/system information
 *
 * Polling: 10s
 * staleTime: 8s
 */
export function useSystemInfo() {
  return useQuery({
    queryKey: queryKeys.systemInfo,
    queryFn: async () => {
      const result = await getSystemInfo();
      return unwrapQueryResult<SystemInfoData>(result);
    },
    refetchInterval: 10_000,
    staleTime: 8_000,
  });
}

/**
 * useSystemScanStatus - channel scan state
 *
 * Polling: 2s
 * staleTime: 1s
 */
export function useSystemScanStatus() {
  return useQuery({
    queryKey: queryKeys.systemScanStatus,
    queryFn: async () => {
      const result = await getSystemScanStatus();
      return unwrapQueryResult<ScanStatus>(result);
    },
    refetchInterval: 2_000,
    staleTime: 1_000,
  });
}

/**
 * useTriggerSystemScanMutation - start a new system scan and refresh scan status
 */
export function useTriggerSystemScanMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async () => {
      const result = await triggerSystemScan();
      return unwrapQueryResult<{ status?: string }>(result);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.systemScanStatus });
    },
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
 * useDvrCapabilities - DVR feature capability flags
 *
 * Polling: disabled
 * staleTime: 5m
 */
export function useDvrCapabilities() {
  return useQuery({
    queryKey: queryKeys.dvrCapabilities,
    queryFn: async () => {
      const result = await getDvrCapabilities();
      return unwrapQueryResult<DvrCapabilities | null>(result, { silent: true }) ?? null;
    },
    staleTime: 5 * 60_000,
  });
}

/**
 * useTimers - Scheduled recording timers
 *
 * Polling: disabled
 * staleTime: 10s
 */
export function useTimers() {
  return useQuery({
    queryKey: queryKeys.timers,
    queryFn: async () => {
      const result = await getTimers();
      const data = unwrapQueryResult<TimerList>(result);
      return data.items ?? [];
    },
    staleTime: 10_000,
  });
}

/**
 * useDeleteTimerMutation - delete timer and refresh timer list
 */
export function useDeleteTimerMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (timerId: string) => {
      const result = await deleteTimer({ path: { timerId } });
      unwrapQueryResult<void>(result);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.timers });
    },
  });
}

/**
 * useRecordings - recordings browser payload for a root/path pair
 *
 * Polling: disabled
 * staleTime: 10s
 */
export function useRecordings(root: string, path: string) {
  return useQuery({
    queryKey: queryKeys.recordings(root, path),
    queryFn: async () => {
      const result = await getRecordings({ query: { root, path } });
      return unwrapQueryResult<RecordingResponse>(result);
    },
    placeholderData: previousData => previousData,
    staleTime: 10_000,
  });
}

/**
 * useDeleteRecordingsMutation - delete one or more recordings and refresh listings
 */
export function useDeleteRecordingsMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (recordingIds: string[]) => {
      await Promise.all(
        recordingIds.map(async (recordingId) => {
          const result = await deleteRecording({ path: { recordingId } });
          unwrapQueryResult<void>(result);
        })
      );
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['v3', 'recordings'] });
    },
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
