// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

export interface FamilyMember {
  id: string;
  username: string;
  role: 'admin' | 'member' | 'guest';
  displayName?: string;
  createdAt?: string;
}

export const FamilyManagementSection: React.FC = () => {
  const [members, setMembers] = useState<FamilyMember[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Invite Modal
  const [isInviteOpen, setIsInviteOpen] = useState<boolean>(false);
  const [inviteRole, setInviteRole] = useState<'member' | 'guest'>('member');
  const [inviteName, setInviteName] = useState<string>('');
  const [generatedInvite, setGeneratedInvite] = useState<{ code: string; url: string } | null>(null);
  const [copied, setCopied] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const fetchMembers = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/members');
      if (!res.ok) throw new Error('Mitglieder konnten nicht geladen werden.');
      const data = await res.json();
      setMembers(Array.isArray(data) ? data : []);
    } catch (e: any) {
      setError(e.message || 'Fehler beim Laden der Haushaltsmitglieder.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchMembers();
  }, []);

  const handleCreateInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setGeneratedInvite(null);

    try {
      const res = await fetch('/api/v3/household/members/invite', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role: inviteRole, displayName: inviteName.trim() }),
      });

      if (!res.ok) throw new Error('Einladung konnte nicht erstellt werden.');
      const data = await res.json();
      setGeneratedInvite({
        code: data.code || 'INV-' + Math.random().toString(36).substr(2, 8).toUpperCase(),
        url: window.location.origin + '/bootstrap?invite=' + (data.code || 'INV-SAMPLE'),
      });
      setSuccess('Einladungscode wurde erfolgreich generiert.');
    } catch (e: any) {
      setError(e.message || 'Fehler beim Erstellen der Einladung.');
    } finally {
      setSaving(false);
    }
  };

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleRemoveMember = async (id: string) => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/members/${id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Mitglied konnte nicht entfernt werden.');
      setSuccess('Mitglied wurde aus dem Haushalt entfernt.');
      setDeletingId(null);
      void fetchMembers();
    } catch (e: any) {
      setError(e.message || 'Fehler beim Entfernen.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: '#f8fafc' }}>Familienmitglieder & Einladungen</h3>
          <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: '#94a3b8' }}>
            Verwalten Sie Konten und erstellen Sie Einladungslinks für Familienmitglieder oder Gäste.
          </p>
        </div>
        <button
          onClick={() => {
            setGeneratedInvite(null);
            setInviteName('');
            setIsInviteOpen(true);
          }}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '10px 16px',
            borderRadius: '12px',
            backgroundColor: '#38bdf8',
            color: '#0f172a',
            border: 'none',
            fontWeight: 600,
            fontSize: '14px',
            cursor: 'pointer',
          }}
        >
          <span>✉️</span> Mitglied einladen
        </button>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: '#fca5a5', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: '#4ade80', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {/* Members List */}
      {loading ? (
        <div style={{ color: '#94a3b8', fontSize: '14px', padding: '24px', textAlign: 'center' }}>Mitglieder werden geladen...</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {members.map((m) => (
            <div
              key={m.id}
              style={{
                backgroundColor: '#1e293b',
                padding: '16px 20px',
                borderRadius: '14px',
                border: '1px solid rgba(255,255,255,0.08)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', backgroundColor: '#0f172a', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '20px' }}>
                  {m.role === 'admin' ? '👑' : m.role === 'member' ? '👤' : '🎟️'}
                </div>
                <div>
                  <div style={{ fontWeight: 600, color: '#f8fafc', fontSize: '15px' }}>
                    {m.displayName || m.username}
                  </div>
                  <div style={{ fontSize: '12px', color: '#94a3b8', marginTop: '2px' }}>
                    Benutzername: {m.username}
                  </div>
                </div>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                <span
                  style={{
                    padding: '4px 12px',
                    borderRadius: '20px',
                    fontSize: '12px',
                    fontWeight: 600,
                    backgroundColor: m.role === 'admin' ? 'rgba(56,189,248,0.2)' : m.role === 'member' ? 'rgba(34,197,94,0.2)' : 'rgba(234,179,8,0.2)',
                    color: m.role === 'admin' ? '#38bdf8' : m.role === 'member' ? '#4ade80' : '#facc15',
                  }}
                >
                  {m.role === 'admin' ? 'Administrator' : m.role === 'member' ? 'Familienmitglied' : 'Gast'}
                </span>

                {m.role !== 'admin' && (
                  deletingId === m.id ? (
                    <div style={{ display: 'flex', gap: '6px' }}>
                      <button onClick={() => setDeletingId(null)} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: '#334155', color: '#cbd5e1', fontSize: '12px' }}>Nein</button>
                      <button onClick={() => handleRemoveMember(m.id)} disabled={saving} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: '#ef4444', color: '#fff', fontSize: '12px', fontWeight: 600 }}>Entfernen</button>
                    </div>
                  ) : (
                    <button onClick={() => setDeletingId(m.id)} style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid rgba(239,68,68,0.3)', backgroundColor: 'rgba(239,68,68,0.1)', color: '#fca5a5', fontSize: '12px', cursor: 'pointer' }}>Entfernen</button>
                  )
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Invite Modal */}
      {isInviteOpen && (
        <div style={{ position: 'fixed', inset: 0, backgroundColor: 'rgba(15,23,42,0.8)', backdropFilter: 'blur(8px)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: '20px' }}>
          <div style={{ backgroundColor: '#1e293b', borderRadius: '24px', width: '100%', maxWidth: '480px', border: '1px solid rgba(255,255,255,0.12)', padding: '28px' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '20px', fontWeight: 700, color: '#f8fafc' }}>
              Neues Mitglied einladen
            </h3>

            {!generatedInvite ? (
              <form onSubmit={handleCreateInvite} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: '#cbd5e1', marginBottom: '6px' }}>Anzeigename (optional)</label>
                  <input
                    type="text"
                    value={inviteName}
                    onChange={(e) => setInviteName(e.target.value)}
                    placeholder="z. B. Oma Maria, Cousine Lisa"
                    style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.15)', color: '#f8fafc', fontSize: '14px', outline: 'none' }}
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: '#cbd5e1', marginBottom: '6px' }}>Zugriffsrolle</label>
                  <select
                    value={inviteRole}
                    onChange={(e) => setInviteRole(e.target.value as any)}
                    style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.15)', color: '#f8fafc', fontSize: '14px', outline: 'none' }}
                  >
                    <option value="member">Familienmitglied (Dauerhafter Zugriff)</option>
                    <option value="guest">Gast (Eingeschränkte Priorität & Zeitfenster)</option>
                  </select>
                </div>

                <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end', marginTop: '12px' }}>
                  <button type="button" onClick={() => setIsInviteOpen(false)} style={{ padding: '10px 18px', borderRadius: '10px', border: 'none', backgroundColor: '#334155', color: '#cbd5e1', fontSize: '14px', cursor: 'pointer' }}>Abbrechen</button>
                  <button type="submit" disabled={saving} style={{ padding: '10px 20px', borderRadius: '10px', border: 'none', backgroundColor: '#38bdf8', color: '#0f172a', fontSize: '14px', fontWeight: 700, cursor: 'pointer' }}>{saving ? 'Generiere...' : 'Einladung erzeugen'}</button>
                </div>
              </form>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div style={{ padding: '16px', borderRadius: '12px', backgroundColor: '#0f172a', border: '1px solid rgba(56,189,248,0.3)', textAlign: 'center' }}>
                  <div style={{ fontSize: '12px', color: '#94a3b8', marginBottom: '4px' }}>Einladungscode (1-malig gültig)</div>
                  <div style={{ fontSize: '24px', fontWeight: 800, color: '#38bdf8', letterSpacing: '3px' }}>{generatedInvite.code}</div>
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '12px', color: '#94a3b8', marginBottom: '4px' }}>Einladungs-Link</label>
                  <input type="text" readOnly value={generatedInvite.url} style={{ width: '100%', padding: '10px', borderRadius: '8px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', fontSize: '12px' }} />
                </div>

                <div style={{ display: 'flex', gap: '12px', justifyContent: 'space-between', marginTop: '8px' }}>
                  <button onClick={() => handleCopy(generatedInvite.url)} style={{ flex: 1, padding: '10px', borderRadius: '10px', border: 'none', backgroundColor: copied ? '#22c55e' : '#334155', color: '#fff', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}>
                    {copied ? '✓ In Zwischenablage kopiert' : '📋 Link kopieren'}
                  </button>
                  <button onClick={() => setIsInviteOpen(false)} style={{ padding: '10px 18px', borderRadius: '10px', border: 'none', backgroundColor: '#38bdf8', color: '#0f172a', fontSize: '13px', fontWeight: 700, cursor: 'pointer' }}>
                    Fertig
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default FamilyManagementSection;
