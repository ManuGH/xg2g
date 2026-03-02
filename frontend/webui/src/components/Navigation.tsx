// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useTranslation } from 'react-i18next';
import { type AppView } from '../types/app-context';
import styles from './Navigation.module.css';

// Simple Unicode Icons for vertical rail
const NavIcons: Record<AppView, string> = {
  dashboard: '⌂',
  epg: '◷',
  recordings: '⏺',
  timers: '⏰',
  series: '≡',
  files: '📁',
  logs: '📋',
  settings: '⚙',
  system: '○'
};

interface NavigationProps {
  activeView: AppView;
  onViewChange: (view: AppView) => void;
}

interface NavItem {
  id: AppView;
  label: string;
  section: 'quick' | 'main' | 'footer';
}

export default function Navigation({ activeView, onViewChange }: NavigationProps) {
  const { t } = useTranslation();

  const navItems: NavItem[] = [
    { id: 'dashboard', label: t('nav.dashboard'), section: 'quick' },
    { id: 'epg', label: t('nav.epg'), section: 'main' },
    { id: 'recordings', label: t('nav.recordings'), section: 'main' },
    { id: 'timers', label: t('nav.timers'), section: 'main' },
    { id: 'series', label: t('nav.series'), section: 'main' },
    { id: 'files', label: t('nav.files'), section: 'main' },
    { id: 'logs', label: t('nav.logs'), section: 'main' },
    { id: 'settings', label: t('nav.playerSettings'), section: 'footer' },
    { id: 'system', label: 'System', section: 'footer' },
  ];

  const renderNavItem = (item: NavItem) => (
    <button
      key={item.id}
      type="button"
      className={styles.item}
      aria-current={activeView === item.id ? 'page' : undefined}
      onClick={() => onViewChange(item.id)}
    >
      <span className={styles.icon}>{NavIcons[item.id]}</span>
      <span className={styles.label}>{item.label}</span>
      <span className={styles.tooltip}>{item.label}</span>
    </button>
  );

  return (
    <div className={styles.railContainer}>
      <nav className={styles.rail} role="navigation" aria-label="Main navigation">
        <div className={styles.quickActions}>
          {navItems.filter(item => item.section === 'quick').map(renderNavItem)}
        </div>
        
        <div className={styles.main}>
          {navItems.filter(item => item.section === 'main').map(renderNavItem)}
        </div>
        
        <div className={styles.footer}>
          {navItems.filter(item => item.section === 'footer').map(renderNavItem)}
        </div>
      </nav>
    </div>
  );
}
