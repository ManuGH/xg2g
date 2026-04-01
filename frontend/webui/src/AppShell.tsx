import { Suspense, useEffect, useMemo, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { useAppContext } from './context/AppContext';
import { useHouseholdProfiles } from './context/HouseholdProfilesContext';
import Navigation from './components/Navigation';
import ErrorBoundary from './components/ErrorBoundary';
import LoadingSkeleton from './components/LoadingSkeleton';
import { resetErrorCatalog } from './lib/errorCatalog';
import { resolveHostEnvironment } from './lib/hostBridge';
import { useErrorCatalog } from './hooks/useServerQueries';
import { normalizePathname, ROUTE_MAP, UNLOCK_ROUTE } from './routes';

interface AppShellProps {
  onLogout?: () => Promise<void> | void;
}

export default function AppShell({ onLogout }: AppShellProps) {
  const { auth, channels, dataLoaded, loadBouquetsAndChannels } = useAppContext();
  const household = useHouseholdProfiles();
  const { pathname } = useLocation();
  const hostEnvironment = useMemo(() => resolveHostEnvironment(), []);
  const usesNativeTvNavigation = hostEnvironment.platform === 'android-tv';
  const normalizedPathname = normalizePathname(pathname);
  const isBootstrapBypassRoute = normalizedPathname === ROUTE_MAP.settings || normalizedPathname === UNLOCK_ROUTE;
  const isHydratingShell = !isBootstrapBypassRoute && channels.loading && !dataLoaded;
  const previousProfileIdRef = useRef<string | null>(null);
  useErrorCatalog(auth.isAuthenticated);

  useEffect(() => {
    if (!auth.isAuthenticated || !household.isReady || channels.loading) {
      return;
    }

    if (isBootstrapBypassRoute) {
      return;
    }

    const profileChanged = previousProfileIdRef.current !== null && previousProfileIdRef.current !== household.selectedProfileId;
    previousProfileIdRef.current = household.selectedProfileId;

    if (!dataLoaded || profileChanged) {
      void loadBouquetsAndChannels();
    }
  }, [
    auth.isAuthenticated,
    channels.loading,
    dataLoaded,
    household.isReady,
    household.selectedProfileId,
    isBootstrapBypassRoute,
    loadBouquetsAndChannels,
  ]);

  useEffect(() => {
    if (auth.isAuthenticated) {
      return;
    }
    resetErrorCatalog();
  }, [auth.isAuthenticated]);

  return (
    <>
      {!usesNativeTvNavigation && <Navigation onLogout={onLogout} />}
      <main className="content-area">
        <ErrorBoundary
          homeHref={ROUTE_MAP.dashboard}
          resetKey={normalizedPathname}
          titleAs="h3"
        >
          {isHydratingShell ? (
            <LoadingSkeleton variant="page" />
          ) : (
            <Suspense fallback={<LoadingSkeleton variant="page" />}>
              <Outlet />
            </Suspense>
          )}
        </ErrorBoundary>
      </main>
    </>
  );
}
