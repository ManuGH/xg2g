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
  createIntent,
  getSystemEntitlements,
  deleteRecording,
  getSystemConfig,
  getSystemConnectivity,
  getErrors,
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
  type ConnectivityContract,
  type CreateIntentResponse,
  type EntitlementStatus,
  type ErrorCatalogResponse,
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
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import { HOUSEHOLD_PROFILE_HEADER, throwOnClientResultError, unwrapClientResultOrThrow } from '../services/clientWrapper';
import { setErrorCatalog } from '../lib/errorCatalog';

/**
 * Query Keys (versioniert, strukturiert)
 */
export const queryKeys = {
  bootstrapConfig: ['v3', 'bootstrap', 'config'] as const,
  errorsCatalog: ['v3', 'system', 'errors-catalog'] as const,
  systemConfig: ['v3', 'system', 'config'] as const,
  systemConnectivity: ['v3', 'system', 'connectivity'] as const,
  systemEntitlements: ['v3', 'system', 'entitlements'] as const,
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

export async function fetchSystemConfigStrict(): Promise<AppConfig | null> {
  const result = await getSystemConfig({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: null,
    } as Record<string, unknown>,
  });
  throwOnClientResultError(result, { source: 'useBootstrapConfig' });
  return result.data ?? null;
}

/**
 * useSystemConfig - persisted backend configuration
 *
 * Polling: disabled
 * staleTime: 30s
 */
export function useSystemConfig() {
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.systemConfig, { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getSystemConfig();
      return unwrapClientResultOrThrow<AppConfig | null>(result, {
        source: 'useSystemConfig',
        silent: true
      }) ?? null;
    },
    enabled: isReady,
    staleTime: 30_000,
  });
}

export function useSystemConnectivity() {
  const { isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: queryKeys.systemConnectivity,
    queryFn: async () => {
      const result = await getSystemConnectivity();
      return unwrapClientResultOrThrow<ConnectivityContract>(result, {
        source: 'useSystemConnectivity',
        silent: true,
      });
    },
    enabled: isReady,
    staleTime: 5_000,
  });
}

export function useBootstrapConfig(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.bootstrapConfig,
    queryFn: fetchSystemConfigStrict,
    enabled,
    retry: false,
    staleTime: 0,
  });
}

export function useSystemEntitlements(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.systemEntitlements,
    queryFn: async () => {
      const result = await getSystemEntitlements();
      return unwrapClientResultOrThrow<EntitlementStatus>(result, {
        source: 'useSystemEntitlements',
      });
    },
    enabled,
    staleTime: 5_000,
  });
}

export function useErrorCatalog(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.errorsCatalog,
    queryFn: async () => {
      const result = await getErrors();
      const data = unwrapClientResultOrThrow<ErrorCatalogResponse | null>(result, {
        source: 'useErrorCatalog',
        silent: true
      });
      if (data?.items) {
        setErrorCatalog(data.items);
        return data.items;
      }
      return [];
    },
    enabled,
    retry: false,
    staleTime: 5 * 60_000,
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
      return unwrapClientResultOrThrow<SystemHealth>(result, { source: 'useSystemHealth' });
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
      return unwrapClientResultOrThrow<SystemInfoData>(result, { source: 'useSystemInfo' });
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
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.systemScanStatus, { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getSystemScanStatus();
      return unwrapClientResultOrThrow<ScanStatus>(result, { source: 'useSystemScanStatus' });
    },
    enabled: isReady,
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
      return unwrapClientResultOrThrow<{ status?: string }>(result, { source: 'useTriggerSystemScanMutation' });
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
      return unwrapClientResultOrThrow<CurrentServiceInfo | null>(result, {
        source: 'useReceiverCurrent',
        silent: true
      }) ?? null;
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
      return unwrapClientResultOrThrow<StreamSession[]>(result, {
        source: 'useStreams',
        silent: true
      }) ?? [];
    },
    refetchInterval: 5_000, // 5s polling
    staleTime: 4_000,
  });
}

/**
 * useStopStreamMutation - stop an active stream session and refresh stream list
 */
export function useStopStreamMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (sessionId: string) => {
      const result = await createIntent({
        body: {
          type: 'stream.stop',
          sessionId,
        },
      });

      return unwrapClientResultOrThrow<CreateIntentResponse | null>(result, {
        source: 'useStopStreamMutation',
        silent: true,
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.streams });
    },
  });
}

/**
 * useDvrStatus - Recording Status
 *
 * Polling: 30s (Dashboard Recording Badge)
 * staleTime: 25s
 */
export function useDvrStatus() {
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.dvrStatus, { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getDvrStatus();
      return unwrapClientResultOrThrow<{ isRecording?: boolean; serviceName?: string } | null>(result, {
        source: 'useDvrStatus',
        silent: true
      }) ?? null;
    },
    enabled: isReady,
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
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.dvrCapabilities, { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getDvrCapabilities();
      return unwrapClientResultOrThrow<DvrCapabilities | null>(result, {
        source: 'useDvrCapabilities',
        silent: true
      }) ?? null;
    },
    enabled: isReady,
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
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.timers, { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getTimers();
      const data = unwrapClientResultOrThrow<TimerList>(result, { source: 'useTimers' });
      return data.items ?? [];
    },
    enabled: isReady,
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
      unwrapClientResultOrThrow<void>(result, { source: 'useDeleteTimerMutation' });
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
  const { selectedProfileId, isReady } = useHouseholdProfiles();

  return useQuery({
    queryKey: [...queryKeys.recordings(root, path), { profileId: selectedProfileId }],
    queryFn: async () => {
      const result = await getRecordings({ query: { root, path } });
      return unwrapClientResultOrThrow<RecordingResponse>(result, { source: 'useRecordings' });
    },
    enabled: isReady,
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
          unwrapClientResultOrThrow<void>(result, { source: 'useDeleteRecordingsMutation' });
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
      const logs = unwrapClientResultOrThrow<LogEntry[]>(result, { source: 'useLogs' });
      return (logs || []).slice(0, limit);
    },
    refetchInterval: false, // kein auto-refetch
    staleTime: 30_000,
  });
}
