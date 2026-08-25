// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useState, useEffect } from 'react';
import { Button } from '../ui';
import {
  listPasskeys,
  deletePasskey,
  startPasskeyRegistration,
  finishPasskeyRegistration,
  revokeOtherSessions,
  type PasskeyCredentialSummary,
} from '../../services/passkeyApi';
import { createPasskeyCredential } from '../../lib/webauthn';
import { getStoredToken, setStoredToken, clearStoredToken } from '../../utils/tokenStorage';
import { useAppContext } from '../../context/AppContext';

export default function SecuritySettingsSection() {
  const { auth, setToken } = useAppContext();
  const [passkeys, setPasskeys] = useState<PasskeyCredentialSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);
  const [tokenInput, setTokenInput] = useState('');
  const [hasActiveToken, setHasActiveToken] = useState<boolean>(() => Boolean(auth.token || auth.isAuthenticated || getStoredToken()));

  // In-UI action confirmations (No native browser confirm popups!)
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [confirmRevokeOthers, setConfirmRevokeOthers] = useState(false);

  const fetchPasskeys = async () => {
    setLoading(true);
    setErrorMsg(null);
    try {
      const data = await listPasskeys();
      setPasskeys(data);
      setHasActiveToken(true);
    } catch (err: any) {
      if (err?.message?.includes('Authentication required') || err?.status === 401) {
        setErrorMsg('Admin-Authentifizierung erforderlich. Bitte melde dich unten mit deinem Admin-Token an.');
        setHasActiveToken(false);
      } else {
        setErrorMsg(err.message || 'Passkeys konnten nicht geladen werden.');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (auth.token || auth.isAuthenticated) {
      setHasActiveToken(true);
    }
    void fetchPasskeys();
  }, [auth.token, auth.isAuthenticated]);

  const handleSaveAdminToken = () => {
    const token = tokenInput.trim();
    if (!token) return;
    setStoredToken(token);
    setToken(token);
    setHasActiveToken(true);
    setTokenInput('');
    setSuccessMsg('✅ Admin-Token erfolgreich gespeichert!');
    void fetchPasskeys();
  };

  const handleLogoutToken = () => {
    clearStoredToken();
    setToken('');
    setHasActiveToken(false);
    setPasskeys([]);
    setSuccessMsg('Admin-Sitzung abgemeldet.');
  };

  const [passkeyNickname, setPasskeyNickname] = useState('');
  const [showNicknameModal, setShowNicknameModal] = useState(false);

  const handleAddPasskeyWithNickname = async (nicknameToUse: string) => {
    setActionLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);
    try {
      const startRes = await startPasskeyRegistration('admin', '');
      const attestation = await createPasskeyCredential(startRes.options);
      const nickname = nicknameToUse.trim() || 'Admin Passkey';
      const finishRes = await finishPasskeyRegistration(attestation, nickname);

      if (finishRes.status === 'registered' || finishRes.id || finishRes.credential) {
        setSuccessMsg(`Passkey "${nickname}" wurde erfolgreich hinzugefügt.`);
        setShowNicknameModal(false);
        setPasskeyNickname('');
        void fetchPasskeys();
      } else {
        throw new Error('Passkey konnte nicht gespeichert werden.');
      }
    } catch (err: any) {
      setErrorMsg(err.message || 'Passkey-Registrierung abgebrochen.');
    } finally {
      setActionLoading(false);
    }
  };

  const handleDeletePasskey = async (id: string) => {
    setActionLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);
    try {
      await deletePasskey(id);
      setSuccessMsg('Passkey wurde entfernt.');
      setDeletingId(null);
      void fetchPasskeys();
    } catch (err: any) {
      setErrorMsg(err.message || 'Passkey konnte nicht gelöscht werden.');
    } finally {
      setActionLoading(false);
    }
  };

  const handleRevokeOthers = async () => {
    setActionLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);
    try {
      await revokeOtherSessions();
      setSuccessMsg('Alle anderen aktiven Sitzungen wurden beendet.');
      setConfirmRevokeOthers(false);
    } catch (err: any) {
      setErrorMsg(err.message || 'Sitzungen konnten nicht beendet werden.');
    } finally {
      setActionLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', width: '100%', maxWidth: '720px' }}>
      <div>
        <h3 style={{ fontSize: '1.25rem', fontWeight: 600, margin: '0 0 0.25rem 0', color: 'var(--text-primary)' }}>
          Sicherheit, Admin-Zugang & Passkeys
        </h3>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-tertiary)', margin: 0 }}>
          Verwalte deine Admin-Authentifizierung, registrierte Passkeys und aktive Gerätesitzungen.
        </p>
      </div>

      {/* SECTION 0: ADMIN AUTHENTICATION CARD */}
      <div style={{ backgroundColor: hasActiveToken ? 'rgba(34, 197, 94, 0.05)' : 'rgba(56, 189, 248, 0.06)', border: `1px solid ${hasActiveToken ? 'rgba(34, 197, 94, 0.25)' : 'rgba(56, 189, 248, 0.25)'}`, borderRadius: '12px', padding: '1.25rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem', flexWrap: 'wrap', gap: '0.5rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontSize: '1.2rem' }}>{hasActiveToken ? '🟢' : '🔑'}</span>
            <strong style={{ color: 'var(--text-primary)', fontSize: '0.95rem' }}>
              {hasActiveToken ? 'Admin-Sitzung aktiv' : 'Admin-Authentifizierung'}
            </strong>
          </div>
          {hasActiveToken && (
            <Button size="sm" variant="ghost" onClick={handleLogoutToken}>
              Abmelden
            </Button>
          )}
        </div>

        {!hasActiveToken ? (
          <div>
            <p style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', margin: '0 0 0.75rem 0' }}>
              Gib dein Admin-Token ein (Standard: <code>test04</code>), um Passkeys und gekoppelte Geräte zu verwalten.
            </p>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
              <input
                type="password"
                placeholder="Admin-Token eingeben (z.B. test04)"
                value={tokenInput}
                onChange={(e) => setTokenInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleSaveAdminToken(); }}
                style={{ flex: 1, minWidth: '200px', padding: '0.5rem 0.75rem', borderRadius: '8px', border: '1px solid rgba(255,255,255,0.15)', backgroundColor: 'rgba(0,0,0,0.3)', color: 'var(--text-primary)', fontSize: '0.875rem' }}
              />
              <Button size="sm" onClick={handleSaveAdminToken} disabled={!tokenInput.trim()}>
                Anmelden
              </Button>
              <Button size="sm" variant="secondary" onClick={() => { setTokenInput('test04'); }}>
                test04 einsetzen
              </Button>
            </div>
          </div>
        ) : (
          <p style={{ fontSize: '0.85rem', color: 'var(--status-success)', margin: 0 }}>
            Du bist erfolgreich als Administrator authentifiziert. Alle Admin-Funktionen sind freigeschaltet.
          </p>
        )}
      </div>

      {errorMsg ? (
        <div style={{ padding: '0.75rem 1rem', borderRadius: '8px', backgroundColor: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', color: 'var(--status-error)', fontSize: '0.875rem' }}>
          {errorMsg}
        </div>
      ) : null}

      {successMsg ? (
        <div style={{ padding: '0.75rem 1rem', borderRadius: '8px', backgroundColor: 'rgba(34, 197, 94, 0.1)', border: '1px solid rgba(34, 197, 94, 0.3)', color: 'var(--status-success)', fontSize: '0.875rem' }}>
          {successMsg}
        </div>
      ) : null}

      {/* SECTION 1: PASSKEY MANAGEMENT */}
      <div style={{ backgroundColor: 'rgba(255, 255, 255, 0.03)', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '12px', padding: '1.25rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
          <div>
            <h4 style={{ fontSize: '1rem', fontWeight: 600, margin: 0, color: 'var(--text-primary)' }}>Registrierte Passkeys</h4>
            <span style={{ fontSize: '0.8rem', color: 'var(--text-tertiary)' }}>{passkeys.length} Passkey(s) verknüpft</span>
          </div>
          <Button
            size="sm"
            onClick={() => setShowNicknameModal(true)}
            disabled={actionLoading || !hasActiveToken}
            data-testid="add-passkey-button"
          >
            {actionLoading ? 'Registriere...' : 'Passkey hinzufügen'}
          </Button>
        </div>

        {/* Modal for Custom Passkey Nickname */}
        {showNicknameModal && (
          <div style={{ padding: '1rem', backgroundColor: 'rgba(56, 189, 248, 0.1)', borderRadius: '8px', marginBottom: '1rem', border: '1px solid rgba(56, 189, 248, 0.3)' }}>
            <h5 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', color: 'var(--accent-action)' }}>Passkey-Bezeichnung angeben</h5>
            <input
              type="text"
              value={passkeyNickname}
              onChange={(e) => setPasskeyNickname(e.target.value)}
              placeholder="z.B. Manuels MacBook Air (Touch ID)"
              style={{ width: '100%', padding: '0.5rem', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)', backgroundColor: 'rgba(0,0,0,0.3)', color: 'var(--text-primary)', fontSize: '0.875rem', marginBottom: '0.75rem' }}
            />
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <Button size="sm" onClick={() => { void handleAddPasskeyWithNickname(passkeyNickname); }} disabled={actionLoading}>
                Jetzt registrieren
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setShowNicknameModal(false)}>
                Abbrechen
              </Button>
            </div>
          </div>
        )}

        {loading ? (
          <div style={{ padding: '1rem', color: 'var(--text-tertiary)', fontSize: '0.875rem' }}>Lade Passkeys...</div>
        ) : passkeys.length === 0 ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--text-disabled)', fontSize: '0.875rem', backgroundColor: 'rgba(0, 0, 0, 0.2)', borderRadius: '8px' }}>
            Noch keine Passkeys registriert. Füge einen Passkey für schnellen Zugang via Touch ID / Face ID hinzu.
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            {passkeys.map((key) => (
              <div
                key={key.id}
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  padding: '0.75rem 1rem',
                  backgroundColor: 'rgba(0, 0, 0, 0.25)',
                  borderRadius: '8px',
                  border: '1px solid rgba(255, 255, 255, 0.05)',
                }}
              >
                <div>
                  <div style={{ fontWeight: 500, fontSize: '0.9rem', color: 'var(--text-primary)' }}>
                    {key.nickname || 'Passkey'}
                  </div>
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-disabled)', marginTop: '0.125rem' }}>
                    Erstellt am: {new Date(key.createdAt).toLocaleString()}
                  </div>
                </div>

                {deletingId === key.id ? (
                  <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                    <span style={{ fontSize: '0.8rem', color: 'var(--status-error)' }}>Löschen?</span>
                    <Button
                      size="sm"
                      variant="primary"
                      onClick={() => { void handleDeletePasskey(key.id); }}
                      disabled={actionLoading}
                      style={{ backgroundColor: 'var(--status-error)' }}
                    >
                      Ja
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setDeletingId(null)}>
                      Abbrechen
                    </Button>
                  </div>
                ) : (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setDeletingId(key.id)}
                    style={{ color: 'var(--status-error)' }}
                    data-testid={`delete-passkey-${key.id}`}
                  >
                    Entfernen
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* SECTION 2: SESSION MANAGEMENT */}
      <div style={{ backgroundColor: 'rgba(255, 255, 255, 0.03)', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '12px', padding: '1.25rem' }}>
        <h4 style={{ fontSize: '1rem', fontWeight: 600, margin: '0 0 0.5rem 0', color: 'var(--text-primary)' }}>Sitzungsverwaltung</h4>
        <p style={{ fontSize: '0.85rem', color: 'var(--text-tertiary)', margin: '0 0 1rem 0' }}>
          Beende alle anderen aktiven Web-Sitzungen auf anderen Geräten und Browsern.
        </p>

        {confirmRevokeOthers ? (
          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', backgroundColor: 'rgba(239, 68, 68, 0.08)', padding: '0.75rem 1rem', borderRadius: '8px' }}>
            <span style={{ fontSize: '0.85rem', color: 'var(--status-error)' }}>
              Wirklich alle anderen aktiven Sitzungen abmelden?
            </span>
            <Button
              size="sm"
              onClick={() => { void handleRevokeOthers(); }}
              disabled={actionLoading}
              style={{ backgroundColor: 'var(--status-error)', color: 'var(--text-primary)' }}
              data-testid="confirm-revoke-others-button"
            >
              Ja, alle abmelden
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setConfirmRevokeOthers(false)}>
              Abbrechen
            </Button>
          </div>
        ) : (
          <Button
            variant="secondary"
            onClick={() => setConfirmRevokeOthers(true)}
            disabled={actionLoading}
            data-testid="revoke-other-sessions-button"
          >
            Andere Sitzungen abmelden
          </Button>
        )}
      </div>
    </div>
  );
}
