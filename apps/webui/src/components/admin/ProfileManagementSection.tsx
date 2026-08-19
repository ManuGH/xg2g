import React, { useState, useEffect } from 'react';
import { request } from '../../lib/api';
import styles from './ProfileManagementSection.module.css';

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
      const data = await request<ProfileData[]>('/api/v3/household/profiles');
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
      maturityLevel: Number(formMaxRating),
      exitPin: formPin ? formPin.trim() : undefined,
    };

    try {
      const url = editingProfile
        ? `/api/v3/household/profiles/${encodeURIComponent(editingProfile.id)}`
        : '/api/v3/household/profiles';
      const method = editingProfile ? 'PUT' : 'POST';

      await request(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

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
      await request(`/api/v3/household/profiles/${encodeURIComponent(id)}`, { method: 'DELETE' });
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
    <div className={styles.section}>
      {/* Header & Create Action */}
      <div className={styles.header}>
        <div>
          <h3 className={styles.heading}>Verwaltete Sehprofile</h3>
          <p className={styles.subheading}>
            Erstellen Sie individuelle Bildschirmoberflächen mit Altersgrenzen und Jugendschutz-PINs.
          </p>
        </div>
        <button onClick={openCreateModal} className={`${styles.button} ${styles.createButton}`}>
          <span>✨</span> Neues Profil
        </button>
      </div>

      {/* Banners */}
      {error && (
        <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>
      )}
      {success && (
        <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>
      )}

      {/* Profiles Grid */}
      {loading ? (
        <div className={styles.loading}>Sehprofile werden geladen...</div>
      ) : profiles.length > 0 ? (
        <div className={styles.grid}>
          {profiles.map((p) => (
            <div key={p.id} className={styles.card}>
              <div>
                <div className={styles.cardTop}>
                  <div className={styles.avatar}>
                    {p.avatarUrl || (p.isChild ? '👦' : '👤')}
                  </div>
                  <span className={`${styles.badge} ${p.isChild ? styles.badgeChild : styles.badgeMain}`}>
                    {p.isChild ? 'Kinderprofil' : 'Hauptprofil'}
                  </span>
                </div>

                <h4 className={styles.name}>{p.name}</h4>
                <div className={styles.meta}>
                  <span>FSK max: <strong>{p.maxParentalRating ? `FSK ${p.maxParentalRating}` : 'Unbegrenzt'}</strong></span>
                  {p.pinCode && <span className={styles.metaPin}>🔒 PIN aktiv</span>}
                </div>

                <div className={styles.policy}>
                  Ohne EPG-Rating:{' '}
                  <span
                    className={`${styles.policyValue} ${
                      p.unknownRatingPolicy === 'block'
                        ? styles.policyBlock
                        : p.unknownRatingPolicy === 'request_approval'
                          ? styles.policyApproval
                          : styles.policyAllow
                    }`}
                  >
                    {p.unknownRatingPolicy === 'block' ? 'Sperren' : p.unknownRatingPolicy === 'request_approval' ? 'Freigabe erfordern' : 'Erlauben'}
                  </span>
                </div>
              </div>

              {/* Actions */}
              <div className={styles.actions}>
                {deletingId === p.id ? (
                  <>
                    <button onClick={() => setDeletingId(null)} className={`${styles.button} ${styles.buttonNeutral}`}>Abbrechen</button>
                    <button onClick={() => handleDelete(p.id)} disabled={saving} className={`${styles.button} ${styles.buttonDangerSolid}`}>Ja, löschen</button>
                  </>
                ) : (
                  <>
                    <button onClick={() => openEditModal(p)} className={`${styles.button} ${styles.buttonGhost}`}>Bearbeiten</button>
                    <button onClick={() => setDeletingId(p.id)} className={`${styles.button} ${styles.buttonDanger}`}>Löschen</button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className={styles.emptyState}>
          Keine zusätzlichen Sehprofile vorhanden. Erstellen Sie ein Profil für Kinder oder Haushaltsmitglieder.
        </div>
      )}

      {/* Modal / Bottom Sheet Form */}
      {isModalOpen && (
        <div className={styles.scrim}>
          <div className={styles.modal}>
            <h3 className={styles.modalTitle}>
              {editingProfile ? 'Sehprofil bearbeiten' : 'Neues Sehprofil anlegen'}
            </h3>

            <form onSubmit={handleSave} className={styles.form}>
              {/* Profile Name */}
              <div>
                <label className={styles.label}>Profilname</label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="z. B. Kinderzimmer, Wohnzimmer TV, Max"
                  className={styles.input}
                  required
                />
              </div>

              {/* Avatar Selector */}
              <div>
                <label className={styles.label}>Avatar Symbol</label>
                <div className={styles.avatarRow}>
                  {AVATAR_OPTIONS.map((emoji) => (
                    <button
                      key={emoji}
                      type="button"
                      onClick={() => setFormAvatar(emoji)}
                      className={`${styles.avatarOption} ${formAvatar === emoji ? styles.avatarOptionSelected : ''}`}
                    >
                      {emoji}
                    </button>
                  ))}
                </div>
              </div>

              {/* Child Profile Toggle */}
              <div className={styles.toggleRow}>
                <div>
                  <div className={styles.toggleTitle}>Kinderprofil (Kinder-UI)</div>
                  <div className={styles.toggleHint}>Aktiviert vereinfachte Navigation und strenge FSK-Filter</div>
                </div>
                <input
                  type="checkbox"
                  checked={formIsChild}
                  onChange={(e) => setFormIsChild(e.target.checked)}
                  className={styles.checkbox}
                />
              </div>

              {/* FSK Rating Limit */}
              <div>
                <div className={styles.ratingHeader}>
                  <label className={styles.ratingLabel}>Maximale FSK-Altersfreigabe</label>
                  <span className={styles.ratingValue}>FSK {formMaxRating}</span>
                </div>
                <input
                  type="range"
                  min="0"
                  max="18"
                  step="6"
                  value={formMaxRating}
                  onChange={(e) => setFormMaxRating(Number(e.target.value))}
                  className={styles.slider}
                />
                <div className={styles.ratingScale}>
                  <span>FSK 0</span>
                  <span>FSK 6</span>
                  <span>FSK 12</span>
                  <span>FSK 18</span>
                </div>
              </div>

              {/* Unknown Rating Strategy */}
              <div>
                <label className={styles.label}>Verhalten bei unbekannter EPG-Altersfreigabe (UNKNOWN = -1)</label>
                <select
                  value={formUnknownPolicy}
                  onChange={(e) => setFormUnknownPolicy(e.target.value as ProfileData['unknownRatingPolicy'])}
                  className={styles.select}
                >
                  <option value="request_approval">Freigabe-Anfrage an Admin senden (Empfohlen)</option>
                  <option value="block">Strikte Sperre (Fail-Closed)</option>
                  <option value="allow">Immer erlauben</option>
                </select>
              </div>

              {/* Jugendschutz PIN */}
              <div>
                <label className={styles.label}>Jugendschutz-PIN (optional)</label>
                <input
                  type="password"
                  maxLength={4}
                  value={formPin}
                  onChange={(e) => setFormPin(e.target.value.replace(/\D/g, ''))}
                  placeholder="4-stelliger PIN z. B. 1234"
                  className={`${styles.input} ${styles.pinInput}`}
                />
              </div>

              {/* Dialog Actions */}
              <div className={styles.modalActions}>
                <button
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  className={`${styles.button} ${styles.buttonNeutral} ${styles.buttonCancel}`}
                >
                  Abbrechen
                </button>
                <button type="submit" disabled={saving} className={`${styles.button} ${styles.buttonSubmit}`}>
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
