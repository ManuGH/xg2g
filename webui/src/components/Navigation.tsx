// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React from 'react';
import { useTranslation } from 'react-i18next';
import './Navigation.css';
import { type AppView } from '../types/app-context';
import { LanguageSwitcher } from './LanguageSwitcher';

// Simple SVG Icons
const Icons = {
  Dashboard: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z" />
    </svg>
  ),
  EPG: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M21 3H3c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h5v2h8v-2h5c1.1 0 1.99-.9 1.99-2L23 5c0-1.1-.9-2-2-2zm0 14H3V5h18v12z" />
    </svg>
  ),
  Files: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z" />
    </svg>
  ),
  Logs: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z" />
    </svg>
  ),
  Config: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.95s.04.65.09.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.04.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.58 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z" />
    </svg>
  ),
  Series: () => (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="currentColor">
      <path d="M4 6h16v2H4zm0 5h16v2H4zm0 5h16v2H4z" />
    </svg>
  )
};

interface NavigationProps {
  activeView: AppView;
  onViewChange: (view: AppView) => void;
}

interface Tab {
  id: AppView;
  label: string;
  icon: React.ComponentType;
}

export default function Navigation({ activeView, onViewChange }: NavigationProps) {
  const { t } = useTranslation();

  const tabs: Tab[] = [
    { id: 'dashboard', label: t('nav.dashboard'), icon: Icons.Dashboard },
    { id: 'epg', label: t('nav.epg'), icon: Icons.EPG },
    { id: 'recordings', label: t('nav.recordings'), icon: Icons.Files },
    { id: 'timers', label: t('nav.timers'), icon: Icons.Dashboard },
    { id: 'series', label: t('nav.series'), icon: Icons.Series },
    { id: 'files', label: t('nav.files'), icon: Icons.Files },
    { id: 'logs', label: t('nav.logs'), icon: Icons.Logs },
    { id: 'config', label: t('nav.settings'), icon: Icons.Config },
  ];

  return (
    <nav className="nav-container glass">
      <div className="nav-logo">xg2g v3.0.0</div>
      <div className="nav-links">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          return (
            <button
              key={tab.id}
              className={`nav-link interactive ${activeView === tab.id ? 'active' : ''}`}
              onClick={() => onViewChange(tab.id)}
            >
              <Icon />
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>
      <div style={{ marginLeft: 'auto', paddingRight: '1rem' }}>
        <LanguageSwitcher />
      </div>
    </nav>
  );
}
