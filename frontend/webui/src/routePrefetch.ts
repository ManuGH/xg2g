import type { AppView } from './routes';

const prefetchMap: Record<AppView, () => Promise<unknown>> = {
  dashboard: () => import('./components/Dashboard'),
  epg: () => import('./features/epg/EPG'),
  timers: () => import('./features/epg/EPG'),
  recordings: () => import('./components/RecordingsList'),
  series: () => import('./components/RecordingsList'),
  files: () => import('./components/Settings'),
  logs: () => import('./components/Settings'),
  settings: () => import('./components/Settings'),
  system: () => import('./features/system/SystemInfo'),
};

const prefetched = new Set<AppView>();

export function prefetchRoute(view: AppView): void {
  if (prefetched.has(view)) return;
  prefetched.add(view);
  prefetchMap[view]().catch(() => {
    prefetched.delete(view);
  });
}
