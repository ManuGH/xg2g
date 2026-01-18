// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useTranslation } from 'react-i18next';
import './Navigation.css';
import { type AppView } from '../types/app-context';

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
    <a
      key={item.id}
      href={`#${item.id}`}
      className="nav-item"
      aria-current={activeView === item.id ? 'page' : undefined}
      onClick={(e) => {
        e.preventDefault();
        onViewChange(item.id);
      }}
    >
      <span className="nav-item__icon">{NavIcons[item.id]}</span>
      <span className="nav-item__label">{item.label}</span>
      <span className="nav-item__tooltip">{item.label}</span>
    </a>
  );

  return (
    <div className="nav-rail-container">
      <nav className="nav-rail" role="navigation" aria-label="Main navigation">
        <div className="nav-rail__quick-actions">
          {navItems.filter(item => item.section === 'quick').map(renderNavItem)}
        </div>
        
        <div className="nav-rail__main">
          {navItems.filter(item => item.section === 'main').map(renderNavItem)}
        </div>
        
        <div className="nav-rail__footer">
          {navItems.filter(item => item.section === 'footer').map(renderNavItem)}
        </div>
      </nav>
    </div>
  );
}
