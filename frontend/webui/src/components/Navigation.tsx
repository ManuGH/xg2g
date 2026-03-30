import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { resolveHostEnvironment } from '../lib/hostBridge';
import { ROUTE_MAP, normalizePathname, type AppView } from '../routes';
import styles from './Navigation.module.css';

type NavSection = 'quick' | 'main' | 'footer';
type IconName = AppView | 'more' | 'logout';

interface NavigationProps {
  onLogout?: () => void;
}

interface NavItem {
  id: AppView;
  label: string;
  section: NavSection;
}

const mobilePrimaryViews: AppView[] = ['dashboard', 'epg', 'recordings'];

function NavIcon({ name, className = '' }: { name: IconName; className?: string }) {
  const commonProps = {
    className,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true
  };

  switch (name) {
    case 'dashboard':
      return (
        <svg {...commonProps}>
          <path d="M4 4h7v7H4zM13 4h7v4h-7zM13 10h7v10h-7zM4 13h7v7H4z" />
        </svg>
      );
    case 'epg':
      return (
        <svg {...commonProps}>
          <rect x="3" y="5" width="18" height="15" rx="3" />
          <path d="M8 3v4M16 3v4M3 10h18M8 13h3M8 17h8" />
        </svg>
      );
    case 'recordings':
      return (
        <svg {...commonProps}>
          <circle cx="12" cy="12" r="7" />
          <circle cx="12" cy="12" r="2.5" fill="currentColor" stroke="none" />
        </svg>
      );
    case 'timers':
      return (
        <svg {...commonProps}>
          <circle cx="12" cy="13" r="7" />
          <path d="M12 9v4l2.5 2.5M9 3h6" />
        </svg>
      );
    case 'series':
      return (
        <svg {...commonProps}>
          <path d="M6 6h12M6 12h12M6 18h8" />
          <circle cx="18" cy="18" r="2" />
        </svg>
      );
    case 'files':
      return (
        <svg {...commonProps}>
          <path d="M4 8a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z" />
        </svg>
      );
    case 'logs':
      return (
        <svg {...commonProps}>
          <path d="M7 5h10M7 10h10M7 15h6M5 5h.01M5 10h.01M5 15h.01M5 19h.01M7 19h10" />
        </svg>
      );
    case 'settings':
      return (
        <svg {...commonProps}>
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1 1 0 0 0 .2 1.1l.1.1a2 2 0 0 1 0 2.8 2 2 0 0 1-2.8 0l-.1-.1a1 1 0 0 0-1.1-.2 1 1 0 0 0-.6.9V20a2 2 0 0 1-4 0v-.2a1 1 0 0 0-.7-.9 1 1 0 0 0-1.1.2l-.1.1a2 2 0 0 1-2.8 0 2 2 0 0 1 0-2.8l.1-.1a1 1 0 0 0 .2-1.1 1 1 0 0 0-.9-.6H4a2 2 0 0 1 0-4h.2a1 1 0 0 0 .9-.7 1 1 0 0 0-.2-1.1l-.1-.1a2 2 0 0 1 0-2.8 2 2 0 0 1 2.8 0l.1.1a1 1 0 0 0 1.1.2h.1a1 1 0 0 0 .6-.9V4a2 2 0 0 1 4 0v.2a1 1 0 0 0 .7.9 1 1 0 0 0 1.1-.2l.1-.1a2 2 0 0 1 2.8 0 2 2 0 0 1 0 2.8l-.1.1a1 1 0 0 0-.2 1.1v.1a1 1 0 0 0 .9.6H20a2 2 0 0 1 0 4h-.2a1 1 0 0 0-.9.7z" />
        </svg>
      );
    case 'system':
      return (
        <svg {...commonProps}>
          <rect x="4" y="5" width="16" height="12" rx="2" />
          <path d="M9 20h6M12 17v3" />
        </svg>
      );
    case 'logout':
      return (
        <svg {...commonProps}>
          <path d="M10 17l5-5-5-5M15 12H4" />
          <path d="M20 20V4" />
        </svg>
      );
    case 'more':
      return (
        <svg {...commonProps}>
          <circle cx="6" cy="12" r="1.5" fill="currentColor" stroke="none" />
          <circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none" />
          <circle cx="18" cy="12" r="1.5" fill="currentColor" stroke="none" />
        </svg>
      );
  }
}

export default function Navigation({ onLogout }: NavigationProps) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const [showMoreMenu, setShowMoreMenu] = useState(false);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const showMoreMenuRef = useRef(false);
  const previousShowMoreMenuRef = useRef(false);
  const restoreFocusRef = useRef(false);
  const sheetTitleId = useId();
  const sheetId = useId();
  const sectionLabels: Record<NavSection, string> = {
    quick: t('nav.sectionControl', { defaultValue: 'Control' }),
    main: t('nav.sectionBrowse', { defaultValue: 'Browse' }),
    footer: t('nav.sectionSystem', { defaultValue: 'System' })
  };

  const navItems = useMemo<NavItem[]>(() => [
    { id: 'dashboard', label: t('nav.dashboard'), section: 'quick' },
    { id: 'epg', label: t('nav.epg'), section: 'main' },
    { id: 'recordings', label: t('nav.recordings'), section: 'main' },
    { id: 'timers', label: t('nav.timers'), section: 'main' },
    { id: 'series', label: t('nav.series'), section: 'main' },
    { id: 'files', label: t('nav.files'), section: 'main' },
    { id: 'logs', label: t('nav.logs'), section: 'main' },
    { id: 'settings', label: t('nav.playerSettings'), section: 'footer' },
    { id: 'system', label: t('nav.system', { defaultValue: 'System' }), section: 'footer' }
  ], [t]);

  const closeMoreMenu = useCallback((restoreFocus: boolean) => {
    restoreFocusRef.current = restoreFocus;
    setShowMoreMenu(false);
  }, []);

  useEffect(() => {
    showMoreMenuRef.current = showMoreMenu;
  }, [showMoreMenu]);

  useEffect(() => {
    if (showMoreMenuRef.current) {
      closeMoreMenu(false);
    }
  }, [closeMoreMenu, pathname]);

  useEffect(() => {
    if (!showMoreMenu) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        closeMoreMenu(true);
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [closeMoreMenu, showMoreMenu]);

  useEffect(() => {
    if (!showMoreMenu) {
      return;
    }

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [showMoreMenu]);

  useEffect(() => {
    const wasOpen = previousShowMoreMenuRef.current;

    if (showMoreMenu) {
      closeButtonRef.current?.focus();
    } else if (wasOpen && restoreFocusRef.current) {
      moreButtonRef.current?.focus();
      restoreFocusRef.current = false;
    }

    previousShowMoreMenuRef.current = showMoreMenu;
  }, [showMoreMenu]);

  useEffect(() => {
    if (showMoreMenu || !resolveHostEnvironment().isTv) {
      return;
    }

    const activeElement = document.activeElement as HTMLElement | null;
    if (activeElement && activeElement !== document.body && activeElement !== document.documentElement) {
      return;
    }

    const activeNavItem = document.querySelector<HTMLElement>('a[aria-current="page"]');
    activeNavItem?.focus();
  }, [pathname, showMoreMenu]);

  const primaryMobileItems = navItems.filter((item) => mobilePrimaryViews.includes(item.id));
  const overflowItems = navItems.filter((item) => !mobilePrimaryViews.includes(item.id));
  const overflowSections = (['quick', 'main', 'footer'] as NavSection[])
    .map((section) => ({
      id: section,
      label: sectionLabels[section],
      items: overflowItems.filter((item) => item.section === section),
    }))
    .filter((section) => section.items.length > 0);
  const activePath = normalizePathname(pathname);
  const overflowActive = overflowItems.some((item) => ROUTE_MAP[item.id] === activePath);

  const renderNavItem = (item: NavItem, appearance: 'desktop' | 'mobile' | 'sheet') => (
    <NavLink
      key={`${appearance}-${item.id}`}
      to={ROUTE_MAP[item.id]}
      className={[
        styles.navItem,
        appearance === 'mobile' ? styles.mobileItem : null,
        appearance === 'sheet' ? styles.sheetItem : null
      ].filter(Boolean).join(' ')}
    >
      <span className={styles.iconShell}>
        <NavIcon name={item.id} className={styles.icon} />
      </span>
      <span className={styles.itemText}>
        <span className={styles.label}>{item.label}</span>
      </span>
    </NavLink>
  );

  return (
    <>
      <aside className={styles.desktopShell}>
        <nav
          className={styles.desktopNav}
          role="navigation"
          aria-label={t('nav.mainNavigationLabel', { defaultValue: 'Main navigation' })}
        >
          <div className={styles.brand}>
            <div className={styles.brandMark}>
              <span className={styles.brandPulse}></span>
            </div>
            <div className={styles.brandCopy}>
              <span className={styles.brandTitle}>xg2g</span>
              <span className={styles.brandSubtitle}>Bridge Deck</span>
            </div>
          </div>

          <div className={styles.desktopSection}>
            <span className={styles.sectionTitle}>{sectionLabels.quick}</span>
            <div className={styles.navList}>
              {navItems.filter((item) => item.section === 'quick').map((item) => renderNavItem(item, 'desktop'))}
            </div>
          </div>

          <div className={styles.desktopSection}>
            <span className={styles.sectionTitle}>{sectionLabels.main}</span>
            <div className={styles.navList}>
              {navItems.filter((item) => item.section === 'main').map((item) => renderNavItem(item, 'desktop'))}
            </div>
          </div>

          <div className={styles.desktopFooter}>
            <span className={styles.sectionTitle}>{sectionLabels.footer}</span>
            <div className={styles.navList}>
              {navItems.filter((item) => item.section === 'footer').map((item) => renderNavItem(item, 'desktop'))}
              {onLogout && (
                <button type="button" className={styles.navItem} onClick={onLogout}>
                  <span className={styles.iconShell}>
                    <NavIcon name="logout" className={styles.icon} />
                  </span>
                  <span className={styles.itemText}>
                    <span className={styles.label}>{t('nav.logout')}</span>
                  </span>
                </button>
              )}
            </div>
          </div>
        </nav>
      </aside>

      <div className={styles.mobileShell}>
        <nav
          className={styles.mobileNav}
          role="navigation"
          aria-label={t('nav.mobileNavigationLabel', { defaultValue: 'Mobile navigation' })}
        >
          {primaryMobileItems.map((item) => renderNavItem(item, 'mobile'))}
          <button
            ref={moreButtonRef}
            type="button"
            className={[styles.navItem, styles.mobileItem].join(' ')}
            aria-current={overflowActive ? 'page' : undefined}
            aria-expanded={showMoreMenu ? 'true' : 'false'}
            aria-haspopup="dialog"
            aria-controls={sheetId}
            onClick={() => {
              if (showMoreMenu) {
                restoreFocusRef.current = false;
                setShowMoreMenu(false);
                return;
              }
              setShowMoreMenu(true);
            }}
          >
            <span className={styles.iconShell}>
              <NavIcon name="more" className={styles.icon} />
            </span>
            <span className={styles.itemText}>
              <span className={styles.label}>{t('nav.more', { defaultValue: 'More' })}</span>
            </span>
          </button>
        </nav>

        {showMoreMenu && (
          <>
            <button
              type="button"
              className={styles.sheetBackdrop}
              aria-label={t('nav.closeNavigationLabel', { defaultValue: 'Close navigation' })}
              onClick={() => closeMoreMenu(true)}
            />
            <div
              id={sheetId}
              className={styles.mobileSheet}
              role="dialog"
              aria-modal="true"
              aria-labelledby={sheetTitleId}
            >
              <div className={styles.sheetHeader}>
                <div>
                  <p className={styles.sheetEyebrow}>{t('nav.sheetEyebrow', { defaultValue: 'Navigation' })}</p>
                  <h2 id={sheetTitleId} className={styles.sheetTitle}>{t('nav.sheetTitle', { defaultValue: 'Control surfaces' })}</h2>
                </div>
                <button
                  ref={closeButtonRef}
                  type="button"
                  className={styles.sheetClose}
                  onClick={() => closeMoreMenu(true)}
                >
                  {t('common.close')}
                </button>
              </div>

              <div className={styles.sheetSections}>
                {overflowSections.map((section) => (
                  <section key={section.id} className={styles.sheetSection} aria-label={section.label}>
                    <p className={styles.sheetSectionTitle}>{section.label}</p>
                    <div className={styles.sheetList}>
                      {section.items.map((item) => renderNavItem(item, 'sheet'))}
                    </div>
                  </section>
                ))}
              </div>

              {onLogout && (
                <div className={styles.sheetFooter}>
                  <button
                    type="button"
                    className={styles.sheetAction}
                    onClick={() => {
                      closeMoreMenu(false);
                      onLogout();
                    }}
                  >
                    <NavIcon name="logout" className={styles.icon} />
                    <span>{t('nav.logout')}</span>
                  </button>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </>
  );
}
