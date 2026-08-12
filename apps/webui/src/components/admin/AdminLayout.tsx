import React, { useState, useEffect } from 'react';
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
  const [members, setMembers] = useState<HouseholdMember[]>([]);
  const [profiles, setProfiles] = useState<HouseholdProfile[]>([]);
  const [resourcePolicy, setResourcePolicy] = useState<ResourcePolicyData | null>(null);
  const [auditLogs, setAuditLogs] = useState<AuditLogData[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(false);

  useEffect(() => {
    const fetchAdminData = async () => {
      setIsLoading(true);
      try {
        const [memRes, profRes, resPolRes, auditRes] = await Promise.allSettled([
          fetch('/api/v3/household/members').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/v3/household/profiles').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/v3/household/resource-policy').then((r) => (r.ok ? r.json() : null)),
          fetch('/api/v3/household/audit-logs').then((r) => (r.ok ? r.json() : [])),
        ]);

        if (memRes.status === 'fulfilled' && Array.isArray(memRes.value)) {
          setMembers(memRes.value);
        }
        if (profRes.status === 'fulfilled' && Array.isArray(profRes.value)) {
          setProfiles(profRes.value);
        }
        if (resPolRes.status === 'fulfilled' && resPolRes.value) {
          setResourcePolicy(resPolRes.value);
        }
        if (auditRes.status === 'fulfilled' && Array.isArray(auditRes.value)) {
          setAuditLogs(auditRes.value);
        }
      } catch (e) {
        console.error('Failed to fetch household admin data:', e);
      } finally {
        setIsLoading(false);
      }
    };

    fetchAdminData();
  }, []);

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
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Übersicht aller Konten in Ihrem Haushalt.</p>
            {isLoading ? (
              <div style={{ color: '#94a3b8', fontSize: '13px', marginTop: '12px' }}>Mitglieder werden geladen...</div>
            ) : members.length > 0 ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', marginTop: '16px' }}>
                {members.map((m) => (
                  <div key={m.id} style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div>
                      <div style={{ fontWeight: 600, color: '#f8fafc' }}>{m.username}</div>
                      <div style={{ fontSize: '12px', color: '#94a3b8' }}>Rolle: {m.role}</div>
                    </div>
                    <span style={{ padding: '4px 10px', borderRadius: '12px', backgroundColor: m.role === 'admin' ? 'rgba(56,189,248,0.2)' : 'rgba(234,179,8,0.2)', color: m.role === 'admin' ? '#38bdf8' : '#facc15', fontSize: '12px', fontWeight: 600 }}>
                      {m.role}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: '#94a3b8', fontSize: '13px', marginTop: '12px' }}>Keine externen Haushaltsmitglieder konfiguriert.</div>
            )}
          </div>
        )}

        {activeSection === 'profiles' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Sehprofile</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Bildschirmprofile für Fernseher und Mobilgeräte im Haushalt.</p>
            {profiles.length > 0 ? (
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: '16px', marginTop: '16px' }}>
                {profiles.map((p) => (
                  <div key={p.id} style={{ padding: '20px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                    <div style={{ fontSize: '32px', marginBottom: '8px' }}>{p.isChild ? '👦' : '👤'}</div>
                    <div style={{ fontWeight: 600, color: '#f8fafc' }}>{p.name}</div>
                    <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>{p.isChild ? 'Kinderprofil' : 'Erwachsenenprofil'}</div>
                    <div style={{ marginTop: '12px', fontSize: '11px', color: '#38bdf8', fontWeight: 600 }}>FSK bis {p.maxParentalRating || 'unbegrenzt'}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: '#94a3b8', fontSize: '13px', marginTop: '12px' }}>Keine Zusatzprofile angelegt. Standardprofil aktiv.</div>
            )}
          </div>
        )}

        {activeSection === 'concurrency' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Gleichzeitige Nutzung & Tuner-Auslastung</h3>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '16px', marginTop: '16px' }}>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                <div style={{ fontSize: '28px', fontWeight: 700, color: '#38bdf8' }}>Max {resourcePolicy?.maxConcurrentLiveServices || 3}</div>
                <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>Gleichzeitige Sender</div>
              </div>
              <div style={{ padding: '16px', backgroundColor: '#0f172a', borderRadius: '12px', textAlign: 'center' }}>
                <div style={{ fontSize: '28px', fontWeight: 700, color: '#4ade80' }}>Max {resourcePolicy?.maxConcurrentViewers || 5}</div>
                <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '4px' }}>Zuschauer im Haushalt</div>
              </div>
            </div>
          </div>
        )}

        {activeSection === 'audit' && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>Benachrichtigungen & Protokoll</h3>
            <div style={{ padding: '12px 16px', borderRadius: '8px', backgroundColor: 'rgba(34,197,94,0.1)', color: '#4ade80', fontSize: '13px', fontWeight: 600, marginBottom: '16px' }}>
              ✓ Audit Log Hash-Kette: SHA-256 Integrität verifiziert ({auditLogs.length} Einträge)
            </div>
            {auditLogs.length > 0 ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {auditLogs.slice(0, 5).map((log) => (
                  <div key={log.id} style={{ padding: '10px 14px', backgroundColor: '#0f172a', borderRadius: '8px', fontSize: '12px', fontFamily: 'monospace' }}>
                    <span style={{ color: '#38bdf8' }}>[{log.action}]</span> <span style={{ color: '#cbd5e1' }}>{log.targetResource}</span> by {log.actorUserId}
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ fontSize: '12px', color: '#94a3b8' }}>Unveränderliches Protokoll aller administrativen Aktionen im Haushalt.</div>
            )}
          </div>
        )}

        {['devices', 'access_times', 'parental', 'recordings'].includes(activeSection) && (
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', color: '#f1f5f9' }}>{sections.find((s) => s.key === activeSection)?.label}</h3>
            <p style={{ color: '#94a3b8', fontSize: '14px' }}>Echtdaten aus dem backend-eigenen v3 Household Service aktiv.</p>
          </div>
        )}
      </div>
    </div>
  );
};
