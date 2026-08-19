// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import styles from './ConcurrencySettingsSection.module.css';

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
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>Gleichzeitige Nutzung &amp; Tuner-Arbitrierung</h3>
        <p className={styles.subheading}>
          Konfigurieren Sie kapazitive Hardware-Grenzen für Tuner, Transcoder und deterministische Preemption-Prioritäten.
        </p>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}
      {success && <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>}

      {loading ? (
        <div className={styles.loading}>Ressourcen-Regeln werden geladen...</div>
      ) : (
        <form onSubmit={handleSave} className={styles.form}>
          {/* Capacity Limits Grid */}
          <div className={styles.capacityGrid}>
            {/* Live TV Services */}
            <div className={styles.capacityCard}>
              <div className={styles.capacityLabel}>📡 Max Live TV Sender</div>
              <div className={styles.capacityValue}>{policy.maxConcurrentLiveServices}</div>
              <input
                type="range"
                min="1"
                max="10"
                value={policy.maxConcurrentLiveServices}
                onChange={(e) => setPolicy({ ...policy, maxConcurrentLiveServices: Number(e.target.value) })}
                className={styles.capacitySlider}
              />
              <div className={styles.capacityHint}>Hardware-Tuner Limit</div>
            </div>

            {/* Household Viewers */}
            <div className={`${styles.capacityCard} ${styles.capacityViewers}`}>
              <div className={styles.capacityLabel}>👨‍👩‍👧‍👦 Max Zuschauer</div>
              <div className={styles.capacityValue}>{policy.maxConcurrentViewers}</div>
              <input
                type="range"
                min="1"
                max="20"
                value={policy.maxConcurrentViewers}
                onChange={(e) => setPolicy({ ...policy, maxConcurrentViewers: Number(e.target.value) })}
                className={styles.capacitySlider}
              />
              <div className={styles.capacityHint}>Aktive Client-Streams</div>
            </div>

            {/* DVR Recordings */}
            <div className={`${styles.capacityCard} ${styles.capacityRecordings}`}>
              <div className={styles.capacityLabel}>📼 Max Parallele Aufnahmen</div>
              <div className={styles.capacityValue}>{policy.maxParallelRecordings}</div>
              <input
                type="range"
                min="1"
                max="5"
                value={policy.maxParallelRecordings}
                onChange={(e) => setPolicy({ ...policy, maxParallelRecordings: Number(e.target.value) })}
                className={styles.capacitySlider}
              />
              <div className={styles.capacityHint}>Geschützte DVR Worker</div>
            </div>

            {/* Transcodes */}
            <div className={`${styles.capacityCard} ${styles.capacityTranscodes}`}>
              <div className={styles.capacityLabel}>⚙️ Max Transcodes</div>
              <div className={styles.capacityValue}>{policy.maxParallelTranscodes}</div>
              <input
                type="range"
                min="0"
                max="5"
                value={policy.maxParallelTranscodes}
                onChange={(e) => setPolicy({ ...policy, maxParallelTranscodes: Number(e.target.value) })}
                className={styles.capacitySlider}
              />
              <div className={styles.capacityHint}>FFmpeg Transcoder-Slots</div>
            </div>
          </div>

          {/* Preemption Arbitration Section */}
          <div className={styles.panel}>
            <div className={styles.panelHeader}>
              <div>
                <h4 className={styles.panelTitle}>Verdrängung &amp; Preemption (Deterministisch)</h4>
                <p className={styles.panelCopy}>
                  Bei Tuner-Engpässen werden Sessions nach festen Prioritätsstufen verdrängt. Identische Ränge verdrängen die jüngste Session (Tie-Breaker).
                </p>
              </div>
              <input
                type="checkbox"
                checked={policy.preemptionEnabled}
                onChange={(e) => setPolicy({ ...policy, preemptionEnabled: e.target.checked })}
                className={styles.checkbox}
              />
            </div>

            {policy.preemptionEnabled && (
              <div className={styles.rankList}>
                <div className={styles.rankListTitle}>Prioritätsreihenfolge (Höchste zuerst)</div>
                {(policy.preemptionPriorityRanks || ['admin_live', 'member_live', 'guest_live']).map((rank, idx) => (
                  <div key={rank} className={styles.rankRow}>
                    <div className={styles.rankIdentity}>
                      <span className={styles.rankNumber}>#{idx + 1}</span>
                      <span className={styles.rankName}>
                        {rank === 'admin_live' ? '👑 Admin Live-TV' : rank === 'member_live' ? '👤 Mitglied Live-TV' : '🎟️ Gast Live-TV'}
                      </span>
                    </div>

                    <div className={styles.rankActions}>
                      <button
                        type="button"
                        onClick={() => moveRank(idx, 'up')}
                        disabled={idx === 0}
                        aria-label="Priorität erhöhen"
                        className={styles.rankButton}
                      >
                        ▲
                      </button>
                      <button
                        type="button"
                        onClick={() => moveRank(idx, 'down')}
                        disabled={idx === (policy.preemptionPriorityRanks?.length || 3) - 1}
                        aria-label="Priorität senken"
                        className={styles.rankButton}
                      >
                        ▼
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className={styles.saveRow}>
            <button type="submit" disabled={saving} className={styles.saveButton}>
              {saving ? 'Speichern...' : '💾 Limits speichern'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
};

export default ConcurrencySettingsSection;
