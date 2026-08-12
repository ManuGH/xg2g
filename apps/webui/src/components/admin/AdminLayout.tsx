import React, { useState } from 'react';
import SecuritySettingsSection from '../settings/SecuritySettingsSection';

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
    <div style={{ display: 'flex', minHeight: '80vh', backgroundColor: '#0f172a', color: '#f8fafc', borderRadius: '16px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)' }}>
      {/* Material 3 Sidebar Navigation */}
      <div style={{ width: '280px', backgroundColor: '#1e293b', padding: '24px 16px', borderRight: '1px solid rgba(255,255,255,0.08)', flexShrink: 0 }}>
        <div style={{ paddingBottom: '16px', marginBottom: '16px', borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
          <h2 style={{ fontSize: '18px', fontWeight: 600, margin: 0, color: '#38bdf8' }}>Haushalt & Administration</h2>
          <p style={{ fontSize: '12px', color: '#94a3b8', margin: '4px 0 0 0' }}>Material 3 Management Center</p>
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
                  color: isActive ? '#38bdf8' : '#cbd5e1',
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
        <div style={{ marginBottom: '24px' }}>
          <h1 style={{ fontSize: '24px', fontWeight: 700, margin: 0 }}>
            {sections.find((s) => s.key === activeSection)?.label}
          </h1>
          <p style={{ fontSize: '14px', color: '#94a3b8', margin: '4px 0 0 0' }}>
            {sections.find((s) => s.key === activeSection)?.description}
          </p>
        </div>

        {/* Section Router */}
        {activeSection === 'security' && <SecuritySettingsSection />}

        {activeSection === 'account' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Hauptkontodaten</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Verwaltung von Benutzername, Passwort und Notfall-Wiederherstellungsschlüsseln.</p>
            <div style={{ display: 'inline-block', padding: '4px 12px', borderRadius: '20px', backgroundColor: 'rgba(34,197,94,0.15)', color: '#4ade80', fontSize: '12px', fontWeight: 600 }}>
              Konto-Status: Aktiv (Admin)
            </div>
          </div>
        )}

        {activeSection === 'family' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Familienmitglieder</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Übersicht der Personen in Ihrem Haushalt.</p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', marginTop: '16px' }}>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <div style={{ fontWeight: 600, color: '#f8fafc' }}>Papa</div>
                  <div style={{ fontSize: '12px', color: '#94a3b8' }}>Administrator · Hauptkonto</div>
                </div>
                <span style={{ padding: '4px 10px', borderRadius: '12px', backgroundColor: 'rgba(56,189,248,0.2)', color: '#38bdf8', fontSize: '12px', fontWeight: 600 }}>Admin</span>
              </div>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <div style={{ fontWeight: 600, color: '#f8fafc' }}>Cousine</div>
                  <div style={{ fontSize: '12px', color: '#94a3b8' }}>Gast · Befristeter Zugriff</div>
                </div>
                <span style={{ padding: '4px 10px', borderRadius: '12px', backgroundColor: 'rgba(234,179,8,0.2)', color: '#facc15', fontSize: '12px', fontWeight: 600 }}>Gast</span>
              </div>
            </div>
          </div>
        )}

        {activeSection === 'profiles' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Sehprofile</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Bildschirmprofile für Fernseher und Mobilgeräte im Haushalt.</p>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '16px', marginTop: '16px' }}>
              <div style={{ padding: '20px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                <div style={{ fontSize: '32px', marginBottom: '8px' }}>👦</div>
                <div style={{ fontWeight: 600, color: '#f8fafc' }}>Max</div>
                <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>Kinderprofil · 9 Jahre</div>
                <div style={{ marginTop: '12px', fontSize: '11px', color: '#38bdf8', fontWeight: 600 }}>FSK bis 12 · Freigabe bei Unbekannt</div>
              </div>
            </div>
          </div>
        )}

        {activeSection === 'access_times' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Zugriffszeiten & Befristungen</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Regeln für tägliche Nutzungsfenster und Ablaufdaten.</p>
            <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', marginTop: '16px' }}>
              <div style={{ fontWeight: 600, color: '#f8fafc' }}>Cousine (Gast)</div>
              <div style={{ fontSize: '13px', color: '#cbd5e1', marginTop: '8px' }}>Gültig: Heute bis 23:00</div>
              <div style={{ fontSize: '13px', color: '#cbd5e1', marginTop: '4px' }}>Täglich: 07:00–19:00 (Europe/Vienna)</div>
            </div>
          </div>
        )}

        {activeSection === 'concurrency' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Gleichzeitige Nutzung & Tuner-Auslastung</h3>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '16px', marginTop: '16px' }}>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                <div style={{ fontSize: '28px', fontWeight: 700, color: '#38bdf8' }}>1 / 3</div>
                <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>Gleichzeitige Sender</div>
              </div>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                <div style={{ fontSize: '28px', fontWeight: 700, color: '#4ade80' }}>2 / 5</div>
                <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>Zuschauer im Haushalt</div>
              </div>
            </div>
          </div>
        )}

        {activeSection === 'audit' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Benachrichtigungen & Protokoll</h3>
            <div style={{ padding: '12px 16px', borderRadius: '8px', backgroundColor: 'rgba(34,197,94,0.1)', color: '#4ade80', fontSize: '13px', fontWeight: 600, marginBottom: '16px' }}>
              ✓ Audit Log Hash-Kette: SHA-256 Integrität verifiziert
            </div>
            <div style={{ fontSize: '12px', color: '#94a3b8' }}>Unveränderliches Protokoll aller administrativen Aktionen im Haushalt.</div>
          </div>
        )}

        {['devices', 'parental', 'recordings'].includes(activeSection) && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>{sections.find((s) => s.key === activeSection)?.label}</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Bereich wird geladen...</p>
          </div>
        )}
      </div>
    </div>
  );
};
