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
import styles from './SecuritySettingsSection.module.css';

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
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>
          Sicherheit, Admin-Zugang &amp; Passkeys
        </h3>
        <p className={styles.subheading}>
          Verwalte deine Admin-Authentifizierung, registrierte Passkeys und aktive Gerätesitzungen.
        </p>
      </div>

      {/* SECTION 0: ADMIN AUTHENTICATION CARD */}
      <div className={`${styles.authCard} ${hasActiveToken ? styles.authCardActive : styles.authCardIdle}`}>
        <div className={styles.authHeader}>
          <div className={styles.authIdentity}>
            <span className={styles.authIcon}>{hasActiveToken ? '🟢' : '🔑'}</span>
            <strong className={styles.authTitle}>
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
            <p className={styles.authCopy}>
              Gib dein Admin-Token ein (Standard: <code>test04</code>), um Passkeys und gekoppelte Geräte zu verwalten.
            </p>
            <div className={styles.tokenRow}>
              <input
                type="password"
                placeholder="Admin-Token eingeben (z.B. test04)"
                value={tokenInput}
                onChange={(e) => setTokenInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleSaveAdminToken(); }}
                className={`${styles.input} ${styles.tokenInput}`}
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
          <p className={styles.authSuccess}>
            Du bist erfolgreich als Administrator authentifiziert. Alle Admin-Funktionen sind freigeschaltet.
          </p>
        )}
      </div>

      {errorMsg ? (
        <div className={`${styles.banner} ${styles.bannerError}`}>{errorMsg}</div>
      ) : null}

      {successMsg ? (
        <div className={`${styles.banner} ${styles.bannerSuccess}`}>{successMsg}</div>
      ) : null}

      {/* SECTION 1: PASSKEY MANAGEMENT */}
      <div className={styles.panel}>
        <div className={styles.panelHeader}>
          <div>
            <h4 className={styles.panelTitle}>Registrierte Passkeys</h4>
            <span className={styles.panelMeta}>{passkeys.length} Passkey(s) verknüpft</span>
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
          <div className={styles.nicknamePrompt}>
            <h5 className={styles.nicknameTitle}>Passkey-Bezeichnung angeben</h5>
            <input
              type="text"
              value={passkeyNickname}
              onChange={(e) => setPasskeyNickname(e.target.value)}
              placeholder="z.B. Manuels MacBook Air (Touch ID)"
              className={`${styles.input} ${styles.nicknameInput}`}
            />
            <div className={styles.nicknameActions}>
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
          <div className={styles.loading}>Lade Passkeys...</div>
        ) : passkeys.length === 0 ? (
          <div className={styles.emptyState}>
            Noch keine Passkeys registriert. Füge einen Passkey für schnellen Zugang via Touch ID / Face ID hinzu.
          </div>
        ) : (
          <div className={styles.passkeyList}>
            {passkeys.map((key) => (
              <div key={key.id} className={styles.passkeyRow}>
                <div>
                  <div className={styles.passkeyName}>{key.nickname || 'Passkey'}</div>
                  <div className={styles.passkeyDate}>
                    Erstellt am: {new Date(key.createdAt).toLocaleString()}
                  </div>
                </div>

                {deletingId === key.id ? (
                  <div className={styles.confirmGroup}>
                    <span className={styles.confirmLabel}>Löschen?</span>
                    <Button
                      size="sm"
                      variant="danger"
                      onClick={() => { void handleDeletePasskey(key.id); }}
                      disabled={actionLoading}
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
                    variant="danger-ghost"
                    onClick={() => setDeletingId(key.id)}
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
      <div className={styles.panel}>
        <h4 className={styles.sessionTitle}>Sitzungsverwaltung</h4>
        <p className={styles.sessionCopy}>
          Beende alle anderen aktiven Web-Sitzungen auf anderen Geräten und Browsern.
        </p>

        {confirmRevokeOthers ? (
          <div className={styles.revokeConfirm}>
            <span className={styles.revokeLabel}>
              Wirklich alle anderen aktiven Sitzungen abmelden?
            </span>
            <Button
              size="sm"
              variant="danger"
              onClick={() => { void handleRevokeOthers(); }}
              disabled={actionLoading}
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
