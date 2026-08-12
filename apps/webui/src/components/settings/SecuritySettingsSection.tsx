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

export default function SecuritySettingsSection() {
  const [passkeys, setPasskeys] = useState<PasskeyCredentialSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  // In-UI action confirmations (No native browser confirm popups!)
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [confirmRevokeOthers, setConfirmRevokeOthers] = useState(false);

  const fetchPasskeys = async () => {
    setLoading(true);
    setErrorMsg(null);
    try {
      const data = await listPasskeys();
      setPasskeys(data);
    } catch (err: any) {
      setErrorMsg(err.message || 'Passkeys konnten nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchPasskeys();
  }, []);

  const handleAddPasskey = async () => {
    setActionLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);
    try {
      const startRes = await startPasskeyRegistration();
      const attestation = await createPasskeyCredential(startRes.options);
      const finishRes = await finishPasskeyRegistration(attestation, 'Neuer Passkey');

      if (finishRes.status === 'registered' || finishRes.id || finishRes.credential) {
        setSuccessMsg('Passkey wurde erfolgreich hinzugefügt.');
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
        <h3 style={{ fontSize: '1.25rem', fontWeight: 600, margin: '0 0 0.25rem 0', color: '#f3f4f6' }}>
          Sicherheit & Passkeys
        </h3>
        <p style={{ fontSize: '0.875rem', color: '#9ca3af', margin: 0 }}>
          Verwalte deine registrierten Passkeys und aktiven Gerätesitzungen.
        </p>
      </div>

      {errorMsg ? (
        <div style={{ padding: '0.75rem 1rem', borderRadius: '8px', backgroundColor: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', color: '#fca5a5', fontSize: '0.875rem' }}>
          {errorMsg}
        </div>
      ) : null}

      {successMsg ? (
        <div style={{ padding: '0.75rem 1rem', borderRadius: '8px', backgroundColor: 'rgba(34, 197, 94, 0.1)', border: '1px solid rgba(34, 197, 94, 0.3)', color: '#86efac', fontSize: '0.875rem' }}>
          {successMsg}
        </div>
      ) : null}

      {/* SECTION 1: PASSKEY MANAGEMENT */}
      <div style={{ backgroundColor: 'rgba(255, 255, 255, 0.03)', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '12px', padding: '1.25rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
          <div>
            <h4 style={{ fontSize: '1rem', fontWeight: 600, margin: 0, color: '#f9fafb' }}>Registrierte Passkeys</h4>
            <span style={{ fontSize: '0.8rem', color: '#9ca3af' }}>{passkeys.length} Passkey(s) verknüpft</span>
          </div>
          <Button
            size="sm"
            onClick={() => { void handleAddPasskey(); }}
            disabled={actionLoading}
            data-testid="add-passkey-button"
          >
            {actionLoading ? 'Registriere...' : 'Passkey hinzufügen'}
          </Button>
        </div>

        {loading ? (
          <div style={{ padding: '1rem', color: '#9ca3af', fontSize: '0.875rem' }}>Lade Passkeys...</div>
        ) : passkeys.length === 0 ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: '#6b7280', fontSize: '0.875rem', backgroundColor: 'rgba(0, 0, 0, 0.2)', borderRadius: '8px' }}>
            Noch keine Passkeys registriert. Füge einen Passkey für schnellen Zugang via Touch ID / Face ID hinzu.
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            {passkeys.map((key) => (
              <div
                key={key.id}
                style={{
                  display: 'flex',
                  justify: 'space-between',
                  alignItems: 'center',
                  padding: '0.75rem 1rem',
                  backgroundColor: 'rgba(0, 0, 0, 0.25)',
                  borderRadius: '8px',
                  border: '1px solid rgba(255, 255, 255, 0.05)',
                }}
              >
                <div>
                  <div style={{ fontWeight: 500, fontSize: '0.9rem', color: '#f3f4f6' }}>
                    {key.nickname || 'Passkey'}
                  </div>
                  <div style={{ fontSize: '0.75rem', color: '#6b7280', marginTop: '0.125rem' }}>
                    Erstellt am: {new Date(key.createdAt).toLocaleString()}
                  </div>
                </div>

                {deletingId === key.id ? (
                  <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                    <span style={{ fontSize: '0.8rem', color: '#ef4444' }}>Löschen?</span>
                    <Button
                      size="sm"
                      variant="primary"
                      onClick={() => { void handleDeletePasskey(key.id); }}
                      disabled={actionLoading}
                      style={{ backgroundColor: '#dc2626' }}
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
                    style={{ color: '#ef4444' }}
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
        <h4 style={{ fontSize: '1rem', fontWeight: 600, margin: '0 0 0.5rem 0', color: '#f9fafb' }}>Sitzungsverwaltung</h4>
        <p style={{ fontSize: '0.85rem', color: '#9ca3af', margin: '0 0 1rem 0' }}>
          Beende alle anderen aktiven Web-Sitzungen auf anderen Geräten und Browsern.
        </p>

        {confirmRevokeOthers ? (
          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', backgroundColor: 'rgba(239, 68, 68, 0.08)', padding: '0.75rem 1rem', borderRadius: '8px' }}>
            <span style={{ fontSize: '0.85rem', color: '#fca5a5' }}>
              Wirklich alle anderen aktiven Sitzungen abmelden?
            </span>
            <Button
              size="sm"
              onClick={() => { void handleRevokeOthers(); }}
              disabled={actionLoading}
              style={{ backgroundColor: '#dc2626', color: '#fff' }}
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
