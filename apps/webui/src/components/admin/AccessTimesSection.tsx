// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import styles from './AccessTimesSection.module.css';

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
    } catch {
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
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>Tägliche Zugriffszeiten &amp; Sperrstunden</h3>
        <p className={styles.subheading}>
          Legen Sie tägliche Sehfenster und erlaubte Wochentage fest. Außerhalb dieser Zeiten schlägt die Arbitrierung fail-closed fehl.
        </p>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}
      {success && <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>}

      {loading ? (
        <div className={styles.loading}>Zugriffszeiten werden geladen...</div>
      ) : (
        <form onSubmit={handleSave} className={styles.form}>
          {/* Days of Week Selector */}
          <div className={styles.panel}>
            <h4 className={styles.panelTitle}>Erlaubte Wochentage</h4>
            <div className={styles.dayRow}>
              {DAYS_OF_WEEK.map((d) => {
                const isActive = (policy.allowedDaysMask & d.bit) !== 0;
                return (
                  <button
                    key={d.bit}
                    type="button"
                    onClick={() => toggleDayBit(d.bit)}
                    className={`${styles.dayButton} ${isActive ? styles.dayButtonActive : ''}`}
                    aria-pressed={isActive}
                  >
                    {d.label}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Time Window & Timeline Preview */}
          <div className={styles.panel}>
            <h4 className={`${styles.panelTitle} ${styles.panelTitleSpaced}`}>Tägliches Sehzeitfenster</h4>

            <div className={styles.windowGrid}>
              <div>
                <label className={styles.label}>Startzeit (ab)</label>
                <input
                  type="time"
                  value={policy.dailyStart}
                  onChange={(e) => setPolicy({ ...policy, dailyStart: e.target.value })}
                  className={styles.input}
                />
              </div>
              <div>
                <label className={styles.label}>Endzeit (bis)</label>
                <input
                  type="time"
                  value={policy.dailyEnd}
                  onChange={(e) => setPolicy({ ...policy, dailyEnd: e.target.value })}
                  className={styles.input}
                />
              </div>
            </div>

            {/* 24h Timeline Visualizer */}
            <div className={styles.timeline}>
              <div className={styles.timelineLegend}>
                <span>00:00 Uhr</span>
                <span className={styles.timelineWindow}>Erlaubt: {policy.dailyStart} – {policy.dailyEnd} Uhr</span>
                <span>24:00 Uhr</span>
              </div>

              <div className={styles.timelineTrack}>
                {!isOvernight ? (
                  <div
                    className={`${styles.timelineFill} ${styles.timelineFillRounded}`}
                    style={{ left: `${startPct}%`, width: `${Math.max(0, endPct - startPct)}%` }}
                  />
                ) : (
                  <>
                    <div className={styles.timelineFill} style={{ left: 0, width: `${endPct}%` }} />
                    <div className={styles.timelineFill} style={{ left: `${startPct}%`, right: 0 }} />
                  </>
                )}
              </div>
            </div>
          </div>

          {/* Product Permissions Toggles */}
          <div className={`${styles.panel} ${styles.permissionPanel}`}>
            <h4 className={styles.permissionTitle}>Produkt-Berechtigungen</h4>

            <div className={styles.toggleRow}>
              <div>
                <div className={styles.toggleTitle}>Live-TV Zugriff</div>
                <div className={styles.toggleHint}>Erlaubt das Ansehen von Live-Sendern im Wochentagsfenster</div>
              </div>
              <input
                type="checkbox"
                checked={policy.liveTvAllowed}
                onChange={(e) => setPolicy({ ...policy, liveTvAllowed: e.target.checked })}
                className={styles.checkbox}
              />
            </div>

            <div className={styles.toggleRow}>
              <div>
                <div className={styles.toggleTitle}>Aufnahmen &amp; Bibliothek</div>
                <div className={styles.toggleHint}>Erlaubt das Ansehen und Programmieren von DVR-Aufnahmen</div>
              </div>
              <input
                type="checkbox"
                checked={policy.recordingsAllowed}
                onChange={(e) => setPolicy({ ...policy, recordingsAllowed: e.target.checked })}
                className={styles.checkbox}
              />
            </div>
          </div>

          <div className={styles.saveRow}>
            <button type="submit" disabled={saving} className={styles.saveButton}>
              {saving ? 'Speichern...' : '💾 Zugriffszeiten speichern'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
};

export default AccessTimesSection;
