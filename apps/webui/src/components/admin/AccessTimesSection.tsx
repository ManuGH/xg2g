// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

export interface AccessPolicyData {
  accountId: string;
  allowedDaysMask: number;
  dailyStart: string;
  dailyEnd: string;
  liveTvAllowed: boolean;
  recordingsAllowed: boolean;
}

const DAYS_OF_WEEK = [
  { bit: 1, label: 'Mo' },
  { bit: 2, label: 'Di' },
  { bit: 4, label: 'Mi' },
  { bit: 8, label: 'Do' },
  { bit: 16, label: 'Fr' },
  { bit: 32, label: 'Sa' },
  { bit: 64, label: 'So' },
];

export const AccessTimesSection: React.FC = () => {
  const [policy, setPolicy] = useState<AccessPolicyData>({
    accountId: 'default_member',
    allowedDaysMask: 127, // All 7 days
    dailyStart: '07:00',
    dailyEnd: '22:00',
    liveTvAllowed: true,
    recordingsAllowed: true,
  });

  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchAccessPolicy = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/policies/access');
      if (res.ok) {
        const data = await res.json();
        if (data) setPolicy((prev) => ({ ...prev, ...data }));
      }
    } catch (e: any) {
      setError('Zugriffszeiten konnten nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchAccessPolicy();
  }, []);

  const toggleDayBit = (bit: number) => {
    const newMask = (policy.allowedDaysMask & bit) ? (policy.allowedDaysMask & ~bit) : (policy.allowedDaysMask | bit);
    setPolicy({ ...policy, allowedDaysMask: newMask });
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSuccess(null);

    try {
      const res = await fetch('/api/v3/household/policies/access', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(policy),
      });

      if (!res.ok) throw new Error('Speichern der Zugriffsregeln fehlgeschlagen.');
      setSuccess('Zugriffszeiten und Tagesfenster wurden erfolgreich aktualisiert.');
    } catch (e: any) {
      setError(e.message || 'Fehler beim Speichern.');
    } finally {
      setSaving(false);
    }
  };

  // Timeline bar calculations (0..24h)
  const parseHourFraction = (t: string) => {
    const [h, m] = t.split(':').map(Number);
    return (h || 0) + (m || 0) / 60;
  };

  const startHour = parseHourFraction(policy.dailyStart);
  const endHour = parseHourFraction(policy.dailyEnd);
  const startPct = (startHour / 24) * 100;
  const endPct = (endHour / 24) * 100;
  const isOvernight = endHour < startHour;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: '#f8fafc' }}>Tägliche Zugriffszeiten & Sperrstunden</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: '#94a3b8' }}>
          Legen Sie tägliche Sehfenster und erlaubte Wochentage fest. Außerhalb dieser Zeiten schlägt die Arbitrierung fail-closed fehl.
        </p>
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

      {loading ? (
        <div style={{ color: '#94a3b8', fontSize: '14px', padding: '24px', textAlign: 'center' }}>Zugriffszeiten werden geladen...</div>
      ) : (
        <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {/* Days of Week Selector */}
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h4 style={{ margin: '0 0 12px 0', fontSize: '15px', color: '#f8fafc' }}>Erlaubte Wochentage</h4>
            <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
              {DAYS_OF_WEEK.map((d) => {
                const isActive = (policy.allowedDaysMask & d.bit) !== 0;
                return (
                  <button
                    key={d.bit}
                    type="button"
                    onClick={() => toggleDayBit(d.bit)}
                    style={{
                      width: '46px',
                      height: '46px',
                      borderRadius: '12px',
                      border: isActive ? '2px solid #38bdf8' : '1px solid rgba(255,255,255,0.1)',
                      backgroundColor: isActive ? 'rgba(56,189,248,0.2)' : '#0f172a',
                      color: isActive ? '#38bdf8' : '#64748b',
                      fontSize: '14px',
                      fontWeight: 700,
                      cursor: 'pointer',
                      transition: 'all 0.15s ease',
                    }}
                  >
                    {d.label}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Time Window & Timeline Preview */}
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h4 style={{ margin: '0 0 16px 0', fontSize: '15px', color: '#f8fafc' }}>Tägliches Sehzeitfenster</h4>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '20px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: '#cbd5e1', marginBottom: '6px' }}>Startzeit (ab)</label>
                <input
                  type="time"
                  value={policy.dailyStart}
                  onChange={(e) => setPolicy({ ...policy, dailyStart: e.target.value })}
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.15)', color: '#f8fafc', fontSize: '14px', outline: 'none' }}
                />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: '#cbd5e1', marginBottom: '6px' }}>Endzeit (bis)</label>
                <input
                  type="time"
                  value={policy.dailyEnd}
                  onChange={(e) => setPolicy({ ...policy, dailyEnd: e.target.value })}
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.15)', color: '#f8fafc', fontSize: '14px', outline: 'none' }}
                />
              </div>
            </div>

            {/* 24h Timeline Visualizer */}
            <div style={{ marginTop: '16px' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '12px', color: '#94a3b8', marginBottom: '6px' }}>
                <span>00:00 Uhr</span>
                <span style={{ color: '#38bdf8', fontWeight: 600 }}>Erlaubt: {policy.dailyStart} – {policy.dailyEnd} Uhr</span>
                <span>24:00 Uhr</span>
              </div>

              <div style={{ height: '16px', width: '100%', backgroundColor: '#0f172a', borderRadius: '8px', overflow: 'hidden', position: 'relative', border: '1px solid rgba(255,255,255,0.1)' }}>
                {!isOvernight ? (
                  <div
                    style={{
                      position: 'absolute',
                      left: `${startPct}%`,
                      width: `${Math.max(0, endPct - startPct)}%`,
                      height: '100%',
                      backgroundColor: '#38bdf8',
                      borderRadius: '4px',
                    }}
                  />
                ) : (
                  <>
                    <div style={{ position: 'absolute', left: 0, width: `${endPct}%`, height: '100%', backgroundColor: '#38bdf8' }} />
                    <div style={{ position: 'absolute', left: `${startPct}%`, right: 0, height: '100%', backgroundColor: '#38bdf8' }} />
                  </>
                )}
              </div>
            </div>
          </div>

          {/* Product Permissions Toggles */}
          <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)', display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <h4 style={{ margin: 0, fontSize: '15px', color: '#f8fafc' }}>Produkt-Berechtigungen</h4>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderRadius: '12px', backgroundColor: '#0f172a' }}>
              <div>
                <div style={{ fontSize: '14px', fontWeight: 600, color: '#f8fafc' }}>Live-TV Zugriff</div>
                <div style={{ fontSize: '12px', color: '#94a3b8' }}>Erlaubt das Ansehen von Live-Sendern im Wochentagsfenster</div>
              </div>
              <input
                type="checkbox"
                checked={policy.liveTvAllowed}
                onChange={(e) => setPolicy({ ...policy, liveTvAllowed: e.target.checked })}
                style={{ width: '20px', height: '20px', accentColor: '#38bdf8', cursor: 'pointer' }}
              />
            </div>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderRadius: '12px', backgroundColor: '#0f172a' }}>
              <div>
                <div style={{ fontSize: '14px', fontWeight: 600, color: '#f8fafc' }}>Aufnahmen & Bibliothek</div>
                <div style={{ fontSize: '12px', color: '#94a3b8' }}>Erlaubt das Ansehen und Programmieren von DVR-Aufnahmen</div>
              </div>
              <input
                type="checkbox"
                checked={policy.recordingsAllowed}
                onChange={(e) => setPolicy({ ...policy, recordingsAllowed: e.target.checked })}
                style={{ width: '20px', height: '20px', accentColor: '#38bdf8', cursor: 'pointer' }}
              />
            </div>
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="submit"
              disabled={saving}
              style={{
                padding: '12px 24px',
                borderRadius: '12px',
                border: 'none',
                backgroundColor: '#38bdf8',
                color: '#0f172a',
                fontSize: '14px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              {saving ? 'Speichern...' : '💾 Zugriffszeiten speichern'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
};

export default AccessTimesSection;
