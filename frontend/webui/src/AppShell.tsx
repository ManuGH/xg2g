import { Suspense, useEffect } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { useAppContext } from './context/AppContext';
import Navigation from './components/Navigation';
import ErrorBoundary from './components/ErrorBoundary';
import LoadingSkeleton from './components/LoadingSkeleton';
import { resetErrorCatalog } from './lib/errorCatalog';
import { useErrorCatalog } from './hooks/useServerQueries';
import { normalizePathname, ROUTE_MAP } from './routes';

interface AppShellProps {
  onLogout?: () => void;
}

export default function AppShell({ onLogout }: AppShellProps) {
  const { auth, channels, dataLoaded, loadBouquetsAndChannels } = useAppContext();
  const { pathname } = useLocation();
  const normalizedPathname = normalizePathname(pathname);
  const isSettingsRoute = normalizedPathname === ROUTE_MAP.settings;
  const isHydratingShell = !isSettingsRoute && channels.loading && !dataLoaded;
  useErrorCatalog(auth.isAuthenticated);

  useEffect(() => {
    if (!auth.isAuthenticated || dataLoaded || channels.loading) {
      return;
    }

    if (isSettingsRoute) {
      return;
    }

    void loadBouquetsAndChannels();
  }, [auth.isAuthenticated, channels.loading, dataLoaded, isSettingsRoute, loadBouquetsAndChannels]);

  useEffect(() => {
    if (auth.isAuthenticated) {
      return;
    }
    resetErrorCatalog();
  }, [auth.isAuthenticated]);

  return (
    <>
      <Navigation onLogout={onLogout} />
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
