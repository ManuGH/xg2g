// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

export interface ResourcePolicyData {
  householdId: string;
  maxConcurrentLiveServices: number;
  maxConcurrentViewers: number;
  maxParallelRecordings: number;
  maxParallelTranscodes: number;
  preemptionEnabled: boolean;
  preemptionPriorityRanks?: string[];
}

export const ConcurrencySettingsSection: React.FC = () => {
  const [policy, setPolicy] = useState<ResourcePolicyData>({
    householdId: 'default_household',
    maxConcurrentLiveServices: 3,
    maxConcurrentViewers: 5,
    maxParallelRecordings: 2,
    maxParallelTranscodes: 2,
    preemptionEnabled: true,
    preemptionPriorityRanks: ['admin_live', 'member_live', 'guest_live'],
  });

  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchPolicy = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/resource-policy');
      if (res.ok) {
        const data = await res.json();
        if (data) setPolicy((prev) => ({ ...prev, ...data }));
      }
    } catch {
      setError('Ressourcen-Limits konnten nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchPolicy();
  }, []);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSuccess(null);

    try {
      const res = await fetch('/api/v3/household/resource-policy', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(policy),
      });

      if (!res.ok) throw new Error('Speichern der Ressourcen-Limits fehlgeschlagen.');
      setSuccess('Ressourcen-Limits und Preemption-Regeln wurden erfolgreich gespeichert.');
    } catch (e: any) {
      setError(e.message || 'Fehler beim Speichern.');
    } finally {
      setSaving(false);
    }
  };

  const moveRank = (index: number, direction: 'up' | 'down') => {
    const ranks = [...(policy.preemptionPriorityRanks || ['admin_live', 'member_live', 'guest_live'])];
    const targetIndex = direction === 'up' ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= ranks.length) return;

    const elemAtIndex = ranks[index];
    const elemAtTarget = ranks[targetIndex];
    if (elemAtIndex !== undefined && elemAtTarget !== undefined) {
      ranks[index] = elemAtTarget;
      ranks[targetIndex] = elemAtIndex;
      setPolicy({ ...policy, preemptionPriorityRanks: ranks });
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>Gleichzeitige Nutzung & Tuner-Arbitrierung</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
          Konfigurieren Sie kapazitive Hardware-Grenzen für Tuner, Transcoder und deterministische Preemption-Prioritäten.
        </p>
      </div>

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

      {loading ? (
        <div style={{ color: 'var(--text-tertiary)', fontSize: '14px', padding: '24px', textAlign: 'center' }}>Ressourcen-Regeln werden geladen...</div>
      ) : (
        <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {/* Capacity Limits Grid */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px' }}>
            {/* Live TV Services */}
            <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '20px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
              <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--accent-action)' }}>📡 Max Live TV Sender</div>
              <div style={{ fontSize: '32px', fontWeight: 800, color: 'var(--text-primary)', margin: '8px 0' }}>{policy.maxConcurrentLiveServices}</div>
              <input
                type="range"
                min="1"
                max="10"
                value={policy.maxConcurrentLiveServices}
                onChange={(e) => setPolicy({ ...policy, maxConcurrentLiveServices: Number(e.target.value) })}
                style={{ width: '100%', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
              />
              <div style={{ fontSize: '11px', color: 'var(--text-tertiary)', marginTop: '6px' }}>Hardware-Tuner Limit</div>
            </div>

            {/* Household Viewers */}
            <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '20px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
              <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--status-success)' }}>👨‍👩‍👧‍👦 Max Zuschauer</div>
              <div style={{ fontSize: '32px', fontWeight: 800, color: 'var(--text-primary)', margin: '8px 0' }}>{policy.maxConcurrentViewers}</div>
              <input
                type="range"
                min="1"
                max="20"
                value={policy.maxConcurrentViewers}
                onChange={(e) => setPolicy({ ...policy, maxConcurrentViewers: Number(e.target.value) })}
                style={{ width: '100%', accentColor: 'var(--status-success)', cursor: 'pointer' }}
              />
              <div style={{ fontSize: '11px', color: 'var(--text-tertiary)', marginTop: '6px' }}>Aktive Client-Streams</div>
            </div>

            {/* DVR Recordings */}
            <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '20px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
              <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--status-warning)' }}>📼 Max Parallele Aufnahmen</div>
              <div style={{ fontSize: '32px', fontWeight: 800, color: 'var(--text-primary)', margin: '8px 0' }}>{policy.maxParallelRecordings}</div>
              <input
                type="range"
                min="1"
                max="5"
                value={policy.maxParallelRecordings}
                onChange={(e) => setPolicy({ ...policy, maxParallelRecordings: Number(e.target.value) })}
                style={{ width: '100%', accentColor: 'var(--status-warning)', cursor: 'pointer' }}
              />
              <div style={{ fontSize: '11px', color: 'var(--text-tertiary)', marginTop: '6px' }}>Geschützte DVR Worker</div>
            </div>

            {/* Transcodes */}
            <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '20px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
              <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--status-info)' }}>⚙️ Max Transcodes</div>
              <div style={{ fontSize: '32px', fontWeight: 800, color: 'var(--text-primary)', margin: '8px 0' }}>{policy.maxParallelTranscodes}</div>
              <input
                type="range"
                min="0"
                max="5"
                value={policy.maxParallelTranscodes}
                onChange={(e) => setPolicy({ ...policy, maxParallelTranscodes: Number(e.target.value) })}
                style={{ width: '100%', accentColor: 'var(--status-info)', cursor: 'pointer' }}
              />
              <div style={{ fontSize: '11px', color: 'var(--text-tertiary)', marginTop: '6px' }}>FFmpeg Transcoder-Slots</div>
            </div>
          </div>

          {/* Preemption Arbitration Section */}
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div>
                <h4 style={{ margin: 0, fontSize: '16px', color: 'var(--text-primary)' }}>Verdrängung & Preemption (Deterministisch)</h4>
                <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
                  Bei Tuner-Engpässen werden Sessions nach festen Prioritätsstufen verdrängt. Identische Ränge verdrängen die jüngste Session (Tie-Breaker).
                </p>
              </div>
              <input
                type="checkbox"
                checked={policy.preemptionEnabled}
                onChange={(e) => setPolicy({ ...policy, preemptionEnabled: e.target.checked })}
                style={{ width: '20px', height: '20px', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
              />
            </div>

            {policy.preemptionEnabled && (
              <div style={{ marginTop: '16px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
                <div style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-disabled)', textTransform: 'uppercase' }}>Prioritätsreihenfolge (Höchste zuerst)</div>
                {(policy.preemptionPriorityRanks || ['admin_live', 'member_live', 'guest_live']).map((rank, idx) => (
                  <div
                    key={rank}
                    style={{
                      padding: '12px 16px',
                      backgroundColor: 'var(--bg-base)',
                      borderRadius: '10px',
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      border: '1px solid rgba(255,255,255,0.06)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                      <span style={{ fontSize: '12px', fontWeight: 800, color: 'var(--accent-action)', width: '20px' }}>#{idx + 1}</span>
                      <span style={{ fontSize: '14px', color: 'var(--text-primary)', fontWeight: 600 }}>
                        {rank === 'admin_live' ? '👑 Admin Live-TV' : rank === 'member_live' ? '👤 Mitglied Live-TV' : '🎟️ Gast Live-TV'}
                      </span>
                    </div>

                    <div style={{ display: 'flex', gap: '4px' }}>
                      <button
                        type="button"
                        onClick={() => moveRank(idx, 'up')}
                        disabled={idx === 0}
                        style={{ padding: '4px 10px', borderRadius: '6px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', cursor: idx === 0 ? 'not-allowed' : 'pointer' }}
                      >
                        ▲
                      </button>
                      <button
                        type="button"
                        onClick={() => moveRank(idx, 'down')}
                        disabled={idx === (policy.preemptionPriorityRanks?.length || 3) - 1}
                        style={{ padding: '4px 10px', borderRadius: '6px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', cursor: idx === (policy.preemptionPriorityRanks?.length || 3) - 1 ? 'not-allowed' : 'pointer' }}
                      >
                        ▼
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="submit"
              disabled={saving}
              style={{
                padding: '12px 24px',
                borderRadius: '12px',
                border: 'none',
                backgroundColor: 'var(--accent-action)',
                color: 'var(--bg-base)',
                fontSize: '14px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              {saving ? 'Speichern...' : '💾 Limits speichern'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
};

export default ConcurrencySettingsSection;
