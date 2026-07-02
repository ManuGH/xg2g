import { useQuery } from '@tanstack/react-query';
import { useAppContext } from '../../context/AppContext';
import { fetchContinueWatching } from './api';

export const continueWatchingQueryKey = ['recordings', 'continue'] as const;

/**
 * useContinueWatching - the principal's most recently updated, unfinished
 * recordings for the dashboard rail. Server-side data; identical across
 * devices by design.
 */
export function useContinueWatching(limit = 12) {
  const { auth } = useAppContext();
  const authReady = auth.isReady ?? true;

  return useQuery({
    queryKey: [...continueWatchingQueryKey, { limit }],
    queryFn: () => fetchContinueWatching(limit),
    enabled: authReady && auth.isAuthenticated,
    staleTime: 15_000,
    refetchOnWindowFocus: true,
  });
}
