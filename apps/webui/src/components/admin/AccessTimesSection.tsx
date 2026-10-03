// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { debugDiagnosticError } from '../../utils/logging';

export interface AccessPolicyData {
  accountId: string;
  allowedDaysMask: number;
  dailyStart: string;
  dailyEnd: string;
  liveTvAllowed: boolean;
  recordingsAllowed: boolean;
}

const DAYS_OF_WEEK = [
  { bit: 1, key: 'admin.accessTimes.days.1' as const },
  { bit: 2, key: 'admin.accessTimes.days.2' as const },
  { bit: 4, key: 'admin.accessTimes.days.4' as const },
  { bit: 8, key: 'admin.accessTimes.days.8' as const },
  { bit: 16, key: 'admin.accessTimes.days.16' as const },
  { bit: 32, key: 'admin.accessTimes.days.32' as const },
  { bit: 64, key: 'admin.accessTimes.days.64' as const },
];

export const AccessTimesSection: React.FC = () => {
  const { t } = useTranslation();
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
  const [correlationId, setCorrelationId] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchAccessPolicy = async () => {
    setLoading(true);
    setError(null);
    setCorrelationId(null);
    try {
      const res = await fetch('/api/v3/household/policies/access');
      if (res.ok) {
        const data = await res.json();
        if (data) setPolicy((prev) => ({ ...prev, ...data }));
      }
    } catch (e: any) {
      debugDiagnosticError('admin.accessTimes.load', e);
      setError(t('admin.accessTimes.loadError'));
      setCorrelationId(e?.requestId || null);
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
    setCorrelationId(null);
    setSuccess(null);

    try {
      const res = await fetch('/api/v3/household/policies/access', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(policy),
      });

      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setSuccess(t('admin.accessTimes.saveSuccess'));
    } catch (e: any) {
      debugDiagnosticError('admin.accessTimes.save', e);
      setError(t('admin.accessTimes.saveError'));
      setCorrelationId(e?.requestId || null);
    } finally {
      setSaving(false);
    }
  };

  // Timeline bar calculations (0..24h)
  const parseHourFraction = (tStr: string) => {
    const [h, m] = tStr.split(':').map(Number);
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
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.accessTimes.title')}</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
          {t('admin.accessTimes.subtitle')}
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          <span>⚠️ {error}</span>
          {correlationId && (
            <span data-testid="error-reference" style={{ marginLeft: '8px', opacity: 0.75, fontSize: '11px', fontFamily: 'monospace' }}>
              ({correlationId})
            </span>
          )}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: 'var(--status-success)', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--text-tertiary)', fontSize: '14px', padding: '24px', textAlign: 'center' }}>{t('admin.accessTimes.loading')}</div>
      ) : (
        <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {/* Days of Week Selector */}
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h4 style={{ margin: '0 0 12px 0', fontSize: '15px', color: 'var(--text-primary)' }}>{t('admin.accessTimes.allowedDays')}</h4>
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
                      border: isActive ? '2px solid var(--accent-action)' : '1px solid rgba(255,255,255,0.1)',
                      backgroundColor: isActive ? 'rgba(56,189,248,0.2)' : 'var(--bg-base)',
                      color: isActive ? 'var(--accent-action)' : 'var(--text-disabled)',
                      fontSize: '14px',
                      fontWeight: 700,
                      cursor: 'pointer',
                      transition: 'all 0.15s ease',
                    }}
                  >
                    {t(d.key)}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Time Window & Timeline Preview */}
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h4 style={{ margin: '0 0 16px 0', fontSize: '15px', color: 'var(--text-primary)' }}>{t('admin.accessTimes.dailyWindow')}</h4>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '20px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>{t('admin.accessTimes.startTime')}</label>
                <input
                  type="time"
                  value={policy.dailyStart}
                  onChange={(e) => setPolicy({ ...policy, dailyStart: e.target.value })}
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>{t('admin.accessTimes.endTime')}</label>
                <input
                  type="time"
                  value={policy.dailyEnd}
                  onChange={(e) => setPolicy({ ...policy, dailyEnd: e.target.value })}
                  style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                />
              </div>
            </div>

            {/* 24h Timeline Visualizer */}
            <div style={{ marginTop: '16px' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '12px', color: 'var(--text-tertiary)', marginBottom: '6px' }}>
                <span>{t('admin.accessTimes.timelineStart')}</span>
                <span style={{ color: 'var(--accent-action)', fontWeight: 600 }}>{t('admin.accessTimes.allowedRange', { start: policy.dailyStart, end: policy.dailyEnd })}</span>
                <span>{t('admin.accessTimes.timelineEnd')}</span>
              </div>

              <div style={{ height: '16px', width: '100%', backgroundColor: 'var(--bg-base)', borderRadius: '8px', overflow: 'hidden', position: 'relative', border: '1px solid rgba(255,255,255,0.1)' }}>
                {!isOvernight ? (
                  <div
                    style={{
                      position: 'absolute',
                      left: `${startPct}%`,
                      width: `${Math.max(0, endPct - startPct)}%`,
                      height: '100%',
                      backgroundColor: 'var(--accent-action)',
                      borderRadius: '4px',
                    }}
                  />
                ) : (
                  <>
                    <div style={{ position: 'absolute', left: 0, width: `${endPct}%`, height: '100%', backgroundColor: 'var(--accent-action)' }} />
                    <div style={{ position: 'absolute', left: `${startPct}%`, right: 0, height: '100%', backgroundColor: 'var(--accent-action)' }} />
                  </>
                )}
              </div>
            </div>
          </div>

          {/* Product Permissions Toggles */}
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)', display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <h4 style={{ margin: 0, fontSize: '15px', color: 'var(--text-primary)' }}>{t('admin.accessTimes.productPermissions')}</h4>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderRadius: '12px', backgroundColor: 'var(--bg-base)' }}>
              <div>
                <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.accessTimes.liveTvAccess')}</div>
                <div style={{ fontSize: '12px', color: 'var(--text-tertiary)' }}>{t('admin.accessTimes.liveTvAccessDesc')}</div>
              </div>
              <input
                type="checkbox"
                checked={policy.liveTvAllowed}
                onChange={(e) => setPolicy({ ...policy, liveTvAllowed: e.target.checked })}
                style={{ width: '20px', height: '20px', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
              />
            </div>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderRadius: '12px', backgroundColor: 'var(--bg-base)' }}>
              <div>
                <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.accessTimes.recordingsAccess')}</div>
                <div style={{ fontSize: '12px', color: 'var(--text-tertiary)' }}>{t('admin.accessTimes.recordingsAccessDesc')}</div>
              </div>
              <input
                type="checkbox"
                checked={policy.recordingsAllowed}
                onChange={(e) => setPolicy({ ...policy, recordingsAllowed: e.target.checked })}
                style={{ width: '20px', height: '20px', accentColor: 'var(--accent-action)', cursor: 'pointer' }}
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
                backgroundColor: 'var(--accent-action)',
                color: 'var(--bg-base)',
                fontSize: '14px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              {saving ? t('admin.accessTimes.saving') : t('admin.accessTimes.save')}
            </button>
          </div>
        </form>
      )}
    </div>
  );
};

export default AccessTimesSection;
