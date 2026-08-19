import React, { useState, useEffect } from 'react';
import styles from './FamilyManagementSection.module.css';
import { request } from '../../lib/api';

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
      const data = await request<FamilyMember[]>('/api/v3/household/members');
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
      const data = await request<{ inviteCode?: string; code?: string; inviteUrl?: string; url?: string }>('/api/v3/household/members/invite', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role: inviteRole, displayName: inviteName.trim() }),
      });

      const code = data.inviteCode || data.code || 'INV-' + Math.random().toString(36).substr(2, 8).toUpperCase();
      const url = data.inviteUrl || data.url || (window.location.origin + '/bootstrap?invite=' + code);
      setGeneratedInvite({ code, url });
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
      await request(`/api/v3/household/members/${encodeURIComponent(id)}`, { method: 'DELETE' });
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
    <div className={styles.section}>
      <div className={styles.header}>
        <div>
          <h3 className={styles.heading}>Familienmitglieder &amp; Einladungen</h3>
          <p className={styles.subheading}>
            Verwalten Sie Konten und erstellen Sie Einladungslinks für Familienmitglieder oder Gäste.
          </p>
        </div>
        <button
          onClick={() => {
            setGeneratedInvite(null);
            setInviteName('');
            setIsInviteOpen(true);
          }}
          className={`${styles.button} ${styles.inviteButton}`}
        >
          <span>✉️</span> Mitglied einladen
        </button>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}
      {success && <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>}

      {/* Members List */}
      {loading ? (
        <div className={styles.loading}>Mitglieder werden geladen...</div>
      ) : (
        <div className={styles.memberList}>
          {members.map((m) => (
            <div key={m.id} className={styles.memberRow}>
              <div className={styles.memberIdentity}>
                <div className={styles.memberAvatar}>
                  {m.role === 'admin' ? '👑' : m.role === 'member' ? '👤' : '🎟️'}
                </div>
                <div>
                  <div className={styles.memberName}>{m.displayName || m.username}</div>
                  <div className={styles.memberUsername}>Benutzername: {m.username}</div>
                </div>
              </div>

              <div className={styles.memberMeta}>
                <span
                  className={`${styles.roleBadge} ${
                    m.role === 'admin' ? styles.roleAdmin : m.role === 'member' ? styles.roleMember : styles.roleGuest
                  }`}
                >
                  {m.role === 'admin' ? 'Administrator' : m.role === 'member' ? 'Familienmitglied' : 'Gast'}
                </span>

                {m.role !== 'admin' && (
                  deletingId === m.id ? (
                    <div className={styles.confirmGroup}>
                      <button
                        onClick={() => setDeletingId(null)}
                        className={`${styles.button} ${styles.buttonNeutral} ${styles.buttonConfirmNo}`}
                      >
                        Nein
                      </button>
                      <button
                        onClick={() => handleRemoveMember(m.id)}
                        disabled={saving}
                        className={`${styles.button} ${styles.buttonDangerSolid}`}
                      >
                        Entfernen
                      </button>
                    </div>
                  ) : (
                    <button onClick={() => setDeletingId(m.id)} className={`${styles.button} ${styles.buttonDanger}`}>
                      Entfernen
                    </button>
                  )
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Invite Modal */}
      {isInviteOpen && (
        <div className={styles.scrim}>
          <div className={styles.modal}>
            <h3 className={styles.modalTitle}>Neues Mitglied einladen</h3>

            {!generatedInvite ? (
              <form onSubmit={handleCreateInvite} className={styles.form}>
                <div>
                  <label className={styles.label}>Anzeigename (optional)</label>
                  <input
                    type="text"
                    value={inviteName}
                    onChange={(e) => setInviteName(e.target.value)}
                    placeholder="z. B. Oma Maria, Cousine Lisa"
                    className={styles.input}
                  />
                </div>

                <div>
                  <label className={styles.label}>Zugriffsrolle</label>
                  <select
                    value={inviteRole}
                    onChange={(e) => setInviteRole(e.target.value as typeof inviteRole)}
                    className={styles.select}
                  >
                    <option value="member">Familienmitglied (Dauerhafter Zugriff)</option>
                    <option value="guest">Gast (Eingeschränkte Priorität &amp; Zeitfenster)</option>
                  </select>
                </div>

                <div className={styles.modalActions}>
                  <button
                    type="button"
                    onClick={() => setIsInviteOpen(false)}
                    className={`${styles.button} ${styles.buttonNeutral} ${styles.buttonCancel}`}
                  >
                    Abbrechen
                  </button>
                  <button type="submit" disabled={saving} className={`${styles.button} ${styles.buttonSubmit}`}>
                    {saving ? 'Generiere...' : 'Einladung erzeugen'}
                  </button>
                </div>
              </form>
            ) : (
              <div className={styles.inviteResult}>
                <div className={styles.inviteCodeBox}>
                  <div className={styles.inviteCodeLabel}>Einladungscode (1-malig gültig)</div>
                  <div className={styles.inviteCode}>{generatedInvite.code}</div>
                </div>

                <div>
                  <label className={styles.inviteLinkLabel}>Einladungs-Link</label>
                  <input type="text" readOnly value={generatedInvite.url} className={styles.inviteLinkInput} />
                </div>

                <div className={styles.inviteActions}>
                  <button
                    onClick={() => handleCopy(generatedInvite.url)}
                    className={`${styles.button} ${styles.copyButton} ${copied ? styles.copyButtonDone : ''}`}
                  >
                    {copied ? '✓ In Zwischenablage kopiert' : '📋 Link kopieren'}
                  </button>
                  <button onClick={() => setIsInviteOpen(false)} className={`${styles.button} ${styles.doneButton}`}>
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
