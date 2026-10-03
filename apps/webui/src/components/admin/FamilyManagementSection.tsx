import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { request } from '../../lib/api';

export interface FamilyMember {
  id: string;
  username: string;
  role: 'admin' | 'member' | 'guest';
  displayName?: string;
  createdAt?: string;
}

export const FamilyManagementSection: React.FC = () => {
  const { t } = useTranslation();
  const [members, setMembers] = useState<FamilyMember[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [errorDetail, setErrorDetail] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Invite Modal
  const [isInviteOpen, setIsInviteOpen] = useState<boolean>(false);
  const [inviteRole, setInviteRole] = useState<'member' | 'guest'>('member');
  const [inviteName, setInviteName] = useState<string>('');
  const [generatedInvite, setGeneratedInvite] = useState<{ code: string; url: string } | null>(null);
  const [copied, setCopied] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const fetchMembers = async () => {
    setLoading(true);
    setError(null);
    setErrorDetail(null);
    try {
      const data = await request<FamilyMember[]>('/api/v3/household/members');
      setMembers(Array.isArray(data) ? data : []);
    } catch (e: any) {
      setError(t('admin.family.loadError'));
      setErrorDetail(e?.message && e.message !== t('admin.family.loadError') ? e.message : null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchMembers();
  }, []);

  const handleCreateInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setErrorDetail(null);
    setGeneratedInvite(null);

    try {
      const data = await request<{ inviteCode?: string; code?: string; inviteUrl?: string; url?: string }>('/api/v3/household/members/invite', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role: inviteRole, displayName: inviteName.trim() }),
      });

      const code = data.inviteCode || data.code || 'INV-' + Math.random().toString(36).substr(2, 8).toUpperCase();
      const url = data.inviteUrl || data.url || (window.location.origin + '/bootstrap?invite=' + code);
      setGeneratedInvite({ code, url });
      setSuccess(t('admin.family.inviteSuccess'));
    } catch (e: any) {
      setError(t('admin.family.inviteError'));
      setErrorDetail(e?.message && e.message !== t('admin.family.inviteError') ? e.message : null);
    } finally {
      setSaving(false);
    }
  };

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleRemoveMember = async (id: string) => {
    setSaving(true);
    setError(null);
    setErrorDetail(null);
    setSuccess(null);
    try {
      await request(`/api/v3/household/members/${encodeURIComponent(id)}`, { method: 'DELETE' });
      setSuccess(t('admin.family.removeSuccess'));
      setDeletingId(null);
      void fetchMembers();
    } catch (e: any) {
      setError(t('admin.family.removeError'));
      setErrorDetail(e?.message && e.message !== t('admin.family.removeError') ? e.message : null);
    } finally {
      setSaving(false);
    }
  };

  const roleLabels: Record<string, string> = {
    admin: t('admin.family.roleAdmin'),
    member: t('admin.family.roleMember'),
    guest: t('admin.family.roleGuest'),
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.family.title')}</h3>
          <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
            {t('admin.family.subtitle')}
          </p>
        </div>
        <button
          onClick={() => {
            setGeneratedInvite(null);
            setInviteName('');
            setIsInviteOpen(true);
          }}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '10px 16px',
            borderRadius: '12px',
            backgroundColor: 'var(--accent-action)',
            color: 'var(--bg-base)',
            border: 'none',
            fontWeight: 600,
            fontSize: '14px',
            cursor: 'pointer',
          }}
        >
          <span>✉️</span> {t('admin.family.inviteMember')}
        </button>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          <div>⚠️ {error}</div>
          {errorDetail && (
            <div data-testid="error-detail" style={{ marginTop: '4px', fontSize: '11px', opacity: 0.85, fontFamily: 'monospace' }}>
              {errorDetail}
            </div>
          )}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: 'var(--status-success)', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {/* Members List */}
      {loading ? (
        <div style={{ color: 'var(--text-tertiary)', fontSize: '14px', padding: '24px', textAlign: 'center' }}>{t('admin.family.loading')}</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {members.map((m) => (
            <div
              key={m.id}
              style={{
                backgroundColor: 'var(--surface-panel-strong)',
                padding: '16px 20px',
                borderRadius: '14px',
                border: '1px solid rgba(255,255,255,0.08)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', backgroundColor: 'var(--bg-base)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '20px' }}>
                  {m.role === 'admin' ? '👑' : m.role === 'member' ? '👤' : '🎟️'}
                </div>
                <div>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: '15px' }}>
                    {m.displayName || m.username}
                  </div>
                  <div style={{ fontSize: '12px', color: 'var(--text-tertiary)', marginTop: '2px' }}>
                    {t('admin.family.usernameLabel', { username: m.username })}
                  </div>
                </div>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                <span
                  style={{
                    padding: '4px 12px',
                    borderRadius: '20px',
                    fontSize: '12px',
                    fontWeight: 600,
                    backgroundColor: m.role === 'admin' ? 'rgba(56,189,248,0.2)' : m.role === 'member' ? 'rgba(34,197,94,0.2)' : 'rgba(234,179,8,0.2)',
                    color: m.role === 'admin' ? 'var(--accent-action)' : m.role === 'member' ? 'var(--status-success)' : 'var(--status-warning)',
                  }}
                >
                  {roleLabels[m.role] || m.role}
                </span>

                {m.role !== 'admin' && (
                  deletingId === m.id ? (
                    <div style={{ display: 'flex', gap: '6px' }}>
                      <button onClick={() => setDeletingId(null)} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '12px' }}>{t('admin.family.no')}</button>
                      <button onClick={() => handleRemoveMember(m.id)} disabled={saving} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--status-error)', color: 'var(--text-primary)', fontSize: '12px', fontWeight: 600 }}>{t('admin.family.remove')}</button>
                    </div>
                  ) : (
                    <button onClick={() => setDeletingId(m.id)} style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid rgba(239,68,68,0.3)', backgroundColor: 'rgba(239,68,68,0.1)', color: 'var(--status-error)', fontSize: '12px', cursor: 'pointer' }}>{t('admin.family.remove')}</button>
                  )
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Invite Modal */}
      {isInviteOpen && (
        <div style={{ position: 'fixed', inset: 0, backgroundColor: 'rgba(15,23,42,0.8)', backdropFilter: 'blur(8px)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: '20px' }}>
          <div style={{ backgroundColor: 'var(--surface-panel-strong)', borderRadius: '24px', width: '100%', maxWidth: '480px', border: '1px solid rgba(255,255,255,0.12)', padding: '28px' }}>
            <h3 style={{ margin: '0 0 16px 0', fontSize: '20px', fontWeight: 700, color: 'var(--text-primary)' }}>
              {t('admin.family.inviteModalTitle')}
            </h3>

            {!generatedInvite ? (
              <form onSubmit={handleCreateInvite} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>{t('admin.family.displayNameLabel')}</label>
                  <input
                    type="text"
                    value={inviteName}
                    onChange={(e) => setInviteName(e.target.value)}
                    placeholder={t('admin.family.displayNamePlaceholder')}
                    style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '6px' }}>{t('admin.family.roleLabel')}</label>
                  <select
                    value={inviteRole}
                    onChange={(e) => setInviteRole(e.target.value as any)}
                    style={{ width: '100%', padding: '10px 14px', borderRadius: '10px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '14px', outline: 'none' }}
                  >
                    <option value="member">{t('admin.family.roleOptionMember')}</option>
                    <option value="guest">{t('admin.family.roleOptionGuest')}</option>
                  </select>
                </div>

                <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end', marginTop: '12px' }}>
                  <button type="button" onClick={() => setIsInviteOpen(false)} style={{ padding: '10px 18px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '14px', cursor: 'pointer' }}>{t('admin.family.cancel')}</button>
                  <button type="submit" disabled={saving} style={{ padding: '10px 20px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--accent-action)', color: 'var(--bg-base)', fontSize: '14px', fontWeight: 700, cursor: 'pointer' }}>{saving ? t('admin.family.generating') : t('admin.family.generateInvite')}</button>
                </div>
              </form>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div style={{ padding: '16px', borderRadius: '12px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(56,189,248,0.3)', textAlign: 'center' }}>
                  <div style={{ fontSize: '12px', color: 'var(--text-tertiary)', marginBottom: '4px' }}>{t('admin.family.inviteCodeLabel')}</div>
                  <div style={{ fontSize: '24px', fontWeight: 800, color: 'var(--accent-action)', letterSpacing: '3px' }}>{generatedInvite.code}</div>
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-tertiary)', marginBottom: '4px' }}>{t('admin.family.inviteUrlLabel')}</label>
                  <input type="text" readOnly value={generatedInvite.url} style={{ width: '100%', padding: '10px', borderRadius: '8px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.1)', color: 'var(--text-secondary)', fontSize: '12px' }} />
                </div>

                <div style={{ display: 'flex', gap: '12px', justifyContent: 'space-between', marginTop: '8px' }}>
                  <button onClick={() => handleCopy(generatedInvite.url)} style={{ flex: 1, padding: '10px', borderRadius: '10px', border: 'none', backgroundColor: copied ? 'var(--status-success)' : 'var(--surface-highlight)', color: 'var(--text-primary)', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}>
                    {copied ? t('admin.family.copied') : t('admin.family.copyLink')}
                  </button>
                  <button onClick={() => setIsInviteOpen(false)} style={{ padding: '10px 18px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--accent-action)', color: 'var(--bg-base)', fontSize: '13px', fontWeight: 700, cursor: 'pointer' }}>
                    {t('admin.family.done')}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default FamilyManagementSection;
