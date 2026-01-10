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

      if (result.error) {
        // @ts-ignore - response.status check ist valid zur Runtime
        if (result.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          throw new Error('Authentication required');
        }
        // @ts-ignore - error.message kann zur Runtime existieren
        throw new Error(result.error.message || 'Failed to fetch health');
      }

      return result.data as SystemHealth;
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

      if (result.error) {
        // Silent fail - UI zeigt "unavailable"
        return null;
      }

      return result.data as CurrentServiceInfo | null;
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

      if (result.error) {
        // Silent fail - UI zeigt [] (keine streams)
        return [];
      }

      return (result.data || []) as StreamSession[];
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

      if (result.error) {
        return null;
      }

      return result.data as { isRecording?: boolean; serviceName?: string } | null;
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

      if (result.error) {
        throw new Error('Failed to load logs');
      }

      return ((result.data || []) as LogEntry[]).slice(0, limit);
    },
    refetchInterval: false, // kein auto-refetch
    staleTime: 30_000,
  });
}
