// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useState, useEffect } from 'react';
import { Button } from '../ui';
import { createPasskeyCredential } from '../../lib/webauthn';
import {
  listPasskeys,
  deletePasskey,
  startPasskeyRegistration,
  finishPasskeyRegistration,
  revokeOtherSessions,
  type PasskeyItem,
} from '../../services/passkeyApi';

export default function SecuritySettingsSection() {
  const [passkeys, setPasskeys] = useState<PasskeyItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const fetchPasskeys = async () => {
    setLoading(true);
    try {
      const items = await listPasskeys();
      setPasskeys(items);
    } catch {
      // Quiet catch if passkeys not configured
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchPasskeys();
  }, []);

  const handleAddPasskey = async () => {
    setActionLoading(true);
    setMessage(null);
    try {
      const startRes = await startPasskeyRegistration('admin');
      const attestation = await createPasskeyCredential(startRes.options);
      const finishRes = await finishPasskeyRegistration(attestation);

      if (finishRes.success) {
        setMessage({ type: 'success', text: 'Passkey wurde erfolgreich hinzugefügt.' });
        void fetchPasskeys();
      } else {
        throw new Error('Passkey konnte nicht registriert werden.');
      }
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Passkey-Registrierung fehlgeschlagen.' });
    } finally {
      setActionLoading(false);
    }
  };

  const handleDeletePasskey = async (id: string) => {
    if (!confirm('Möchtest du diesen Passkey wirklich löschen?')) return;
    setActionLoading(true);
    setMessage(null);
    try {
      await deletePasskey(id);
      setMessage({ type: 'success', text: 'Passkey wurde gelöscht.' });
      void fetchPasskeys();
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Passkey konnte nicht gelöscht werden.' });
    } finally {
      setActionLoading(false);
    }
  };

  const handleRevokeOtherSessions = async () => {
    if (!confirm('Möchtest du alle anderen aktiven Sitzungen abmelden?')) return;
    setActionLoading(true);
    setMessage(null);
    try {
      await revokeOtherSessions();
      setMessage({ type: 'success', text: 'Alle anderen Sitzungen wurden erfolgreich abgemeldet.' });
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || 'Sitzungen konnten nicht abgemeldet werden.' });
    } finally {
      setActionLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', width: '100%' }}>
      <div>
        <h3 style={{ fontSize: '1.1rem', fontWeight: 600, marginBottom: '0.5rem' }}>Passkeys & Sicherheit</h3>
        <p style={{ fontSize: '0.875rem', color: '#9ca3af', marginBottom: '1rem' }}>
          Verwalte deine registrierten Passkeys für die anmeldungsfreie Bestätigung mit Face ID, Touch ID oder Sicherheitsschlüsseln.
        </p>

        {message ? (
          <div
            style={{
              padding: '0.75rem',
              borderRadius: '6px',
              fontSize: '0.875rem',
              marginBottom: '1rem',
              backgroundColor: message.type === 'success' ? 'rgba(34, 197, 94, 0.1)' : 'rgba(239, 68, 68, 0.1)',
              color: message.type === 'success' ? '#4ade80' : '#f87171',
              border: `1px solid ${message.type === 'success' ? 'rgba(34, 197, 94, 0.2)' : 'rgba(239, 68, 68, 0.2)'}`,
            }}
          >
            {message.text}
          </div>
        ) : null}

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          {loading ? (
            <div style={{ fontSize: '0.875rem', color: '#6b7280' }}>Lade Passkeys...</div>
          ) : passkeys.length === 0 ? (
            <div style={{ fontSize: '0.875rem', color: '#6b7280', fontStyle: 'italic' }}>
              Keine zusätzlichen Passkeys registriert.
            </div>
          ) : (
            passkeys.map((pk) => (
              <div
                key={pk.id}
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  padding: '0.75rem 1rem',
                  backgroundColor: 'rgba(255, 255, 255, 0.03)',
                  borderRadius: '8px',
                  border: '1px solid rgba(255, 255, 255, 0.06)',
                }}
              >
                <div>
                  <div style={{ fontWeight: 500, fontSize: '0.9rem' }}>{pk.name || 'Passkey'}</div>
                  <div style={{ fontSize: '0.75rem', color: '#6b7280' }}>
                    Erstellt am: {new Date(pk.createdAt).toLocaleDateString()}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => { void handleDeletePasskey(pk.id); }}
                  disabled={actionLoading}
                  style={{ color: '#ef4444' }}
                >
                  Löschen
                </Button>
              </div>
            ))
          )}

          <div style={{ marginTop: '0.5rem' }}>
            <Button
              onClick={() => { void handleAddPasskey(); }}
              disabled={actionLoading}
              style={{ fontSize: '0.9rem' }}
            >
              Passkey für dieses Gerät hinzufügen
            </Button>
          </div>
        </div>
      </div>

      <div style={{ borderTop: '1px solid rgba(255, 255, 255, 0.08)', paddingTop: '1.5rem' }}>
        <h3 style={{ fontSize: '1.1rem', fontWeight: 600, marginBottom: '0.5rem' }}>Aktive Sitzungen</h3>
        <p style={{ fontSize: '0.875rem', color: '#9ca3af', marginBottom: '1rem' }}>
          Melde alle anderen Browser und Geräte ab, die aktuell auf deinen xg2g-Server zugreifen.
        </p>
        <Button
          variant="secondary"
          onClick={() => { void handleRevokeOtherSessions(); }}
          disabled={actionLoading}
          style={{ fontSize: '0.9rem' }}
        >
          Andere Sitzungen abmelden
        </Button>
      </div>
    </div>
  );
}
