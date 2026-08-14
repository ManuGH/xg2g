// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

export interface ProfileData {
  id: string;
  name: string;
  avatarUrl?: string;
  isChild: boolean;
  maxParentalRating: number;
  unknownRatingPolicy: 'block' | 'request_approval' | 'allow';
  pinCode?: string;
}

const AVATAR_OPTIONS = ['👤', '👦', '👧', '👨', '👩', '🧑', '🎭', '🎬', '🍿'];

export const ProfileManagementSection: React.FC = () => {
  const [profiles, setProfiles] = useState<ProfileData[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState<boolean>(false);
  const [editingProfile, setEditingProfile] = useState<ProfileData | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  // Form State
  const [formName, setFormName] = useState<string>('');
  const [formAvatar, setFormAvatar] = useState<string>('👤');
  const [formIsChild, setFormIsChild] = useState<boolean>(false);
  const [formMaxRating, setFormMaxRating] = useState<number>(12);
  const [formUnknownPolicy, setFormUnknownPolicy] = useState<'block' | 'request_approval' | 'allow'>('request_approval');
  const [formPin, setFormPin] = useState<string>('');
  const [saving, setSaving] = useState<boolean>(false);

  const fetchProfiles = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/profiles');
      if (!res.ok) throw new Error('Sehprofile konnten nicht geladen werden.');
      const data = await res.json();
      setProfiles(Array.isArray(data) ? data : []);
    } catch (e: any) {
      setError(e.message || 'Fehler beim Laden der Sehprofile.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchProfiles();
  }, []);

  const openCreateModal = () => {
    setEditingProfile(null);
    setFormName('');
    setFormAvatar('👤');
    setFormIsChild(false);
    setFormMaxRating(12);
    setFormUnknownPolicy('request_approval');
    setFormPin('');
    setIsModalOpen(true);
  };

  const openEditModal = (p: ProfileData) => {
    setEditingProfile(p);
    setFormName(p.name);
    setFormAvatar(p.avatarUrl || (p.isChild ? '👦' : '👤'));
    setFormIsChild(p.isChild);
    setFormMaxRating(p.maxParentalRating || 12);
    setFormUnknownPolicy(p.unknownRatingPolicy || 'request_approval');
    setFormPin(p.pinCode || '');
    setIsModalOpen(true);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formName.trim()) {
      setError('Bitte einen Namen für das Profil eingeben.');
      return;
    }

    setSaving(true);
    setError(null);
    setSuccess(null);

    const payload = {
      name: formName.trim(),
      avatarUrl: formAvatar,
      isChild: formIsChild,
      maxParentalRating: Number(formMaxRating),
      unknownRatingPolicy: formUnknownPolicy,
      pinCode: formPin ? formPin.trim() : undefined,
    };

    try {
      const url = editingProfile
        ? `/api/v3/household/profiles/${editingProfile.id}`
        : '/api/v3/household/profiles';
      const method = editingProfile ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!res.ok) {
        throw new Error('Speichern des Profils fehlgeschlagen.');
      }

      setSuccess(editingProfile ? 'Profil wurde aktualisiert.' : 'Neues Sehprofil wurde angelegt.');
      setIsModalOpen(false);
      void fetchProfiles();
    } catch (e: any) {
      setError(e.message || 'Fehler beim Speichern.');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/profiles/${id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Löschen des Profils fehlgeschlagen.');
      setSuccess('Profil wurde gelöscht.');
      setDeletingId(null);
      void fetchProfiles();
    } catch (e: any) {
      setError(e.message || 'Fehler beim Löschen.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      {/* Header & Create Action */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>Verwaltete Sehprofile</h3>
          <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
            Erstellen Sie individuelle Bildschirmoberflächen mit Altersgrenzen und Jugendschutz-PINs.
          </p>
        </div>
        <button
          onClick={openCreateModal}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '10px 16px',
            borderRadius: '12px',
            backgroundColor: 'var(--accent-action)',
            color: 'var(--bg-base)',
            border: 'none',
            fontWeight: 600,
            fontSize: '14px',
            cursor: 'pointer',
            transition: 'transform 0.1s ease',
          }}
        >
          <span>✨</span> Neues Profil
        </button>
      </div>

      {/* Banners */}
      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: 'var(--status-success)', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {/* Profiles Grid */}
      {loading ? (
        <div style={{ color: 'var(--text-tertiary)', fontSize: '14px', padding: '24px', textAlign: 'center' }}>Sehprofile werden geladen...</div>
      ) : profiles.length > 0 ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: '16px' }}>
          {profiles.map((p) => (
            <div
              key={p.id}
              style={{
                backgroundColor: 'var(--surface-panel-strong)',
                borderRadius: '16px',
                padding: '20px',
                border: '1px solid rgba(255,255,255,0.08)',
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'space-between',
                transition: 'border-color 0.2s ease',
              }}
            >
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <div style={{ fontSize: '40px', backgroundColor: 'var(--bg-base)', width: '56px', height: '56px', borderRadius: '16px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                    {p.avatarUrl || (p.isChild ? '👦' : '👤')}
                  </div>
                  <span
                    style={{
                      padding: '4px 10px',
                      borderRadius: '12px',
                      fontSize: '11px',
                      fontWeight: 600,
                      backgroundColor: p.isChild ? 'rgba(234,179,8,0.15)' : 'rgba(56,189,248,0.15)',
                      color: p.isChild ? 'var(--status-warning)' : 'var(--accent-action)',
                    }}
                  >
                    {p.isChild ? 'Kinderprofil' : 'Hauptprofil'}
                  </span>
                </div>

                <h4 style={{ margin: '14px 0 4px 0', fontSize: '16px', color: 'var(--text-primary)' }}>{p.name}</h4>
                <div style={{ fontSize: '12px', color: 'var(--text-tertiary)', display: 'flex', gap: '8px', alignItems: 'center', marginTop: '6px' }}>
                  <span>FSK max: <strong>{p.maxParentalRating ? `FSK ${p.maxParentalRating}` : 'Unbegrenzt'}</strong></span>
                  {p.pinCode && <span style={{ color: 'var(--status-success)' }}>🔒 PIN aktiv</span>}
                </div>

                <div style={{ marginTop: '12px', fontSize: '11px', color: 'var(--text-disabled)' }}>
                  Ohne EPG-Rating:{' '}
                  <span style={{ color: p.unknownRatingPolicy === 'block' ? 'var(--status-error)' : p.unknownRatingPolicy === 'request_approval' ? 'var(--status-warning)' : 'var(--status-success)', fontWeight: 600 }}>
                    {p.unknownRatingPolicy === 'block' ? 'Sperren' : p.unknownRatingPolicy === 'request_approval' ? 'Freigabe erfordern' : 'Erlauben'}
                  </span>
                </div>
              </div>

              {/* Actions */}
              <div style={{ marginTop: '20px', paddingTop: '12px', borderTop: '1px solid rgba(255,255,255,0.06)', display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
                {deletingId === p.id ? (
                  <>
                    <button onClick={() => setDeletingId(null)} style={{ padding: '6px 12px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '12px', cursor: 'pointer' }}>Abbrechen</button>
                    <button onClick={() => handleDelete(p.id)} disabled={saving} style={{ padding: '6px 12px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--status-error)', color: 'var(--text-primary)', fontSize: '12px', fontWeight: 600, cursor: 'pointer' }}>Ja, löschen</button>
                  </>
                ) : (
                  <>
                    <button onClick={() => openEditModal(p)} style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid rgba(255,255,255,0.1)', backgroundColor: 'transparent', color: 'var(--accent-action)', fontSize: '12px', fontWeight: 500, cursor: 'pointer' }}>Bearbeiten</button>
                    <button onClick={() => setDeletingId(p.id)} style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid rgba(239,68,68,0.3)', backgroundColor: 'rgba(239,68,68,0.1)', color: 'var(--status-error)', fontSize: '12px', cursor: 'pointer' }}>Löschen</button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '32px', borderRadius: '16px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: '14px', border: '1px dashed rgba(255,255,255,0.1)' }}>
          Keine zusätzlichen Sehprofile vorhanden. Erstellen Sie ein Profil für Kinder oder Haushaltsmitglieder.
        </div>
      )}

      {/* Modal / Bottom Sheet Form */}
      {isModalOpen && (
        <div style={{ position: 'fixed', inset: 0, backgroundColor: 'rgba(15,23,42,0.8)', backdropFilter: 'blur(8px)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: '20px' }}>
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', borderRadius: '24px', width: '100%', maxWidth: '520px', border: '1px solid rgba(255,255,255,0.12)', padding: '28px', boxShadow: '0 25px 50px -12px rgba(0,0,0,0.5)' }}>
            <h3 style={{ margin: '0 0 20px 0', fontSize: '20px', fontWeight: 700, color: 'var(--text-primary)' }}>
              {editingProfile ? 'Sehprofil bearbeiten' : 'Neues Sehprofil anlegen'}
            </h3>

            <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              {/* Profile Name */}
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>Profilname</label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="z. B. Kinderzimmer, Wohnzimmer TV, Max"
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                  required
                />
              </div>

              {/* Avatar Selector */}
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>Avatar Symbol</label>
                <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                  {AVATAR_OPTIONS.map((emoji) => (
                    <button
                      key={emoji}
                      type="button"
                      onClick={() => setFormAvatar(emoji)}
                      style={{
                        fontSize: '24px',
                        width: '44px',
                        height: '44px',
                        borderRadius: '10px',
                        border: formAvatar === emoji ? '2px solid var(--accent-action)' : '1px solid rgba(255,255,255,0.1)',
                        backgroundColor: formAvatar === emoji ? 'rgba(56,189,248,0.2)' : 'var(--bg-base)',
                        cursor: 'pointer',
                        transition: 'all 0.15s ease',
                      }}
                    >
                      {emoji}
                    </button>
                  ))}
                </div>
              </div>

              {/* Child Profile Toggle */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', borderRadius: '12px', backgroundColor: 'var(--bg-base)' }}>
                <div>
                  <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>Kinderprofil (Kinder-UI)</div>
                  <div style={{ fontSize: '12px', color: 'var(--text-tertiary)' }}>Aktiviert vereinfachte Navigation und strenge FSK-Filter</div>
                </div>
                <input
                  type="checkbox"
                  checked={formIsChild}
                  onChange={(e) => setFormIsChild(e.target.checked)}
                  style={{ width: '20px', height: '20px', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
                />
              </div>

              {/* FSK Rating Limit */}
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '6px' }}>
                  <label style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)' }}>Maximale FSK-Altersfreigabe</label>
                  <span style={{ fontSize: '13px', fontWeight: 700, color: 'var(--accent-action)' }}>FSK {formMaxRating}</span>
                </div>
                <input
                  type="range"
                  min="0"
                  max="18"
                  step="6"
                  value={formMaxRating}
                  onChange={(e) => setFormMaxRating(Number(e.target.value))}
                  style={{ width: '100%', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
                />
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-disabled)', marginTop: '4px' }}>
                  <span>FSK 0</span>
                  <span>FSK 6</span>
                  <span>FSK 12</span>
                  <span>FSK 18</span>
                </div>
              </div>

              {/* Unknown Rating Strategy */}
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>Verhalten bei unbekannter EPG-Altersfreigabe (UNKNOWN = -1)</label>
                <select
                  value={formUnknownPolicy}
                  onChange={(e) => setFormUnknownPolicy(e.target.value as any)}
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                >
                  <option value="request_approval">Freigabe-Anfrage an Admin senden (Empfohlen)</option>
                  <option value="block">Strikte Sperre (Fail-Closed)</option>
                  <option value="allow">Immer erlauben</option>
                </select>
              </div>

              {/* Jugendschutz PIN */}
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>Jugendschutz-PIN (optional)</label>
                <input
                  type="password"
                  maxLength={4}
                  value={formPin}
                  onChange={(e) => setFormPin(e.target.value.replace(/\D/g, ''))}
                  placeholder="4-stelliger PIN z. B. 1234"
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none', letterSpacing: '4px' }}
                />
              </div>

              {/* Dialog Actions */}
              <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end', marginTop: '16px' }}>
                <button
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  style={{ padding: '10px 18px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '14px', fontWeight: 500, cursor: 'pointer' }}
                >
                  Abbrechen
                </button>
                <button
                  type="submit"
                  disabled={saving}
                  style={{ padding: '10px 20px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--accent-action)', color: 'var(--bg-base)', fontSize: '14px', fontWeight: 700, cursor: 'pointer' }}
                >
                  {saving ? 'Speichern...' : 'Profil speichern'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};

export default ProfileManagementSection;
