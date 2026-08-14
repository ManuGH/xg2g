import React, { useState } from 'react';
import SecuritySettingsSection from '../settings/SecuritySettingsSection';
import ProfileManagementSection from './ProfileManagementSection';
import FamilyManagementSection from './FamilyManagementSection';
import ConcurrencySettingsSection from './ConcurrencySettingsSection';
import AccessTimesSection from './AccessTimesSection';
import ParentalControlSection from './ParentalControlSection';
import AuditNotificationsSection from './AuditNotificationsSection';
import DevicesManagementSection from './DevicesManagementSection';
import { NotificationCenter } from '../notifications/NotificationCenter';

export type AdminSectionKey =
  | 'account'
  | 'family'
  | 'profiles'
  | 'devices'
  | 'security'
  | 'access_times'
  | 'parental'
  | 'recordings'
  | 'concurrency'
  | 'audit';

export interface HouseholdMember {
  id: string;
  username: string;
  role: string;
  displayName?: string;
  createdAt?: string;
}

export interface HouseholdProfile {
  id: string;
  name: string;
  avatarUrl?: string;
  isChild: boolean;
  maxParentalRating: number;
  unknownRatingPolicy: string;
}

export interface ResourcePolicyData {
  householdId: string;
  maxConcurrentLiveServices: number;
  maxConcurrentViewers: number;
  maxParallelRecordings: number;
  maxParallelTranscodes: number;
  preemptionEnabled: boolean;
  preemptionPriorityRanks?: string[];
}

export interface AuditLogData {
  id: number;
  actorUserId: string;
  action: string;
  targetResource: string;
  prevHash: string;
  hash: string;
  createdAt: string;
}

interface AdminLayoutProps {
  initialSection?: AdminSectionKey;
}

export const AdminLayout: React.FC<AdminLayoutProps> = ({ initialSection = 'account' }) => {
  const [activeSection, setActiveSection] = useState<AdminSectionKey>(initialSection);

  const sections: { key: AdminSectionKey; label: string; icon: string; description: string }[] = [
    { key: 'account', label: 'Konto', icon: '👤', description: 'Persönliche Kontodaten & Hauptrolle' },
    { key: 'family', label: 'Familie', icon: '👨‍👩‍👧‍👦', description: 'Haushaltsmitglieder & Einladungen' },
    { key: 'profiles', label: 'Profile', icon: '🎭', description: 'Sehprofile, Altersgrenzen & PINs' },
    { key: 'devices', label: 'Geräte', icon: '📱', description: 'Verbundene Geräte & 30-Tage-Vertrauen' },
    { key: 'security', label: 'Sicherheit', icon: '🛡️', description: 'Web-Sitzungen, Passkeys & Widerruf' },
    { key: 'access_times', label: 'Zugriffszeiten', icon: '🕒', description: 'Tägliche Sehzeiten & Ablaufdaten' },
    { key: 'parental', label: 'Jugendschutz', icon: '🔒', description: 'FSK-Freigaben & EPG-Korrekturen' },
    { key: 'recordings', label: 'Aufnahmen', icon: '📼', description: 'Speicherkontingente & Bibliotheken' },
    { key: 'concurrency', label: 'Gleichzeitige Nutzung', icon: '📡', description: 'Tuner-Auslastung & Prioritäten' },
    { key: 'audit', label: 'Benachrichtigungen & Audit', icon: '📜', description: 'Änderungsprotokoll & Push-Regeln' },
  ];

  return (
    <div style={{ display: 'flex', minHeight: '80vh', backgroundColor: 'var(--bg-base)', color: 'var(--text-primary)', borderRadius: '16px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)' }}>
      {/* Material 3 Sidebar Navigation */}
      <div style={{ width: '280px', backgroundColor: 'var(--surface-panel-strong)', padding: '24px 16px', borderRight: '1px solid rgba(255,255,255,0.08)', flexShrink: 0 }}>
        <div style={{ paddingBottom: '16px', marginBottom: '16px', borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
          <h2 style={{ fontSize: '18px', fontWeight: 600, margin: 0, color: 'var(--accent-action)' }}>Haushalt & Administration</h2>
          <p style={{ fontSize: '12px', color: 'var(--text-tertiary)', margin: '4px 0 0 0' }}>Material 3 Management Center</p>
        </div>

        <nav style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
          {sections.map((sec) => {
            const isActive = activeSection === sec.key;
            return (
              <button
                key={sec.key}
                onClick={() => setActiveSection(sec.key)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '12px',
                  padding: '10px 14px',
                  borderRadius: '12px',
                  border: 'none',
                  backgroundColor: isActive ? 'rgba(56, 189, 248, 0.15)' : 'transparent',
                  color: isActive ? 'var(--accent-action)' : 'var(--text-secondary)',
                  fontWeight: isActive ? 600 : 400,
                  fontSize: '14px',
                  cursor: 'pointer',
                  textAlign: 'left',
                  transition: 'all 0.15s ease-in-out',
                }}
              >
                <span style={{ fontSize: '16px' }}>{sec.icon}</span>
                <span>{sec.label}</span>
              </button>
            );
          })}
        </nav>
      </div>

      {/* Main Content Area */}
      <div style={{ flex: 1, padding: '32px', overflowY: 'auto' }}>
        <div style={{ marginBottom: '24px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <h1 style={{ fontSize: '24px', fontWeight: 700, margin: 0 }}>
              {sections.find((s) => s.key === activeSection)?.label}
            </h1>
            <p style={{ fontSize: '14px', color: 'var(--text-tertiary)', margin: '4px 0 0 0' }}>
              {sections.find((s) => s.key === activeSection)?.description}
            </p>
          </div>
          <NotificationCenter />
        </div>

        {/* Section Router */}
        {activeSection === 'security' && <SecuritySettingsSection />}
        {activeSection === 'profiles' && <ProfileManagementSection />}
        {activeSection === 'family' && <FamilyManagementSection />}
        {activeSection === 'concurrency' && <ConcurrencySettingsSection />}
        {activeSection === 'access_times' && <AccessTimesSection />}
        {activeSection === 'parental' && <ParentalControlSection />}
        {activeSection === 'audit' && <AuditNotificationsSection />}
        {activeSection === 'devices' && <DevicesManagementSection />}

        {activeSection === 'account' && (
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: 'var(--text-primary)' }}>Hauptkontodaten</h3>
            <p style={{ color: 'var(--text-tertiary)', fontSize: '14px' }}>Verwaltung von Benutzername, Passwort und Notfall-Wiederherstellungsschlüsseln.</p>
            <div style={{ display: 'inline-block', padding: '4px 12px', borderRadius: '20px', backgroundColor: 'rgba(34,197,94,0.15)', color: 'var(--status-success)', fontSize: '12px', fontWeight: 600 }}>
              Konto-Status: Aktiv (Admin)
            </div>
          </div>
        )}

        {activeSection === 'recordings' && (
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: 'var(--text-primary)' }}>Aufnahmen & Speicherkontingente</h3>
            <p style={{ color: 'var(--text-tertiary)', fontSize: '14px' }}>Verwaltung von DVR-Aufnahmepfad und automatischem Quota-Management.</p>
          </div>
        )}
      </div>
    </div>
  );
};
