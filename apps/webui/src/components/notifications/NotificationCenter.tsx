import React, { useState, useEffect } from 'react';

export interface NotificationItem {
  id: string;
  householdId: string;
  userId: string;
  type: string;
  title: string;
  body: string;
  resourceId?: string;
  actionRequired?: string;
  createdAt: string;
  readAt?: string;
  expiresAt?: string;
}

export const NotificationCenter: React.FC = () => {
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [unreadCount, setUnreadCount] = useState<number>(0);
  const [isOpen, setIsOpen] = useState<boolean>(false);
  const [filter, setFilter] = useState<'all' | 'unread'>('unread');
  const [webPushEnabled, setWebPushEnabled] = useState<boolean>(false);
  const [actionMessage, setActionMessage] = useState<string | null>(null);

  const fetchNotifications = async () => {
    try {
      const res = await fetch('/api/v3/notifications');
      if (res.ok) {
        const data: NotificationItem[] = await res.json();
        setNotifications(data);
        const unread = data.filter((n) => !n.readAt).length;
        setUnreadCount(unread);
      }
    } catch {
      // Ignore network errors in demo mode
    }
  };

  useEffect(() => {
    fetchNotifications();

    // Check browser WebPush status
    if ('Notification' in window && Notification.permission === 'granted') {
      setWebPushEnabled(true);
    }

    // Connect to SSE stream if supported
    if (typeof window !== 'undefined' && 'EventSource' in window) {
      const eventSource = new EventSource('/api/v3/notifications/stream');

      eventSource.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data);
          if (typeof payload.unreadCount === 'number') {
            setUnreadCount(payload.unreadCount);
          }
          if (Array.isArray(payload.notifications)) {
            setNotifications(payload.notifications);
          } else {
            fetchNotifications();
          }
        } catch {
          fetchNotifications();
        }
      };

      eventSource.onerror = () => {
        eventSource.close();
      };

      return () => {
        eventSource.close();
      };
    }
  }, []);

  const handleMarkAllRead = async () => {
    try {
      await fetch('/api/v3/notifications/mark-all-read', { method: 'POST' });
      setNotifications((prev) => prev.map((n) => ({ ...n, readAt: new Date().toISOString() })));
      setUnreadCount(0);
    } catch {
      // Fallback update
      setNotifications((prev) => prev.map((n) => ({ ...n, readAt: new Date().toISOString() })));
      setUnreadCount(0);
    }
  };

  const handleMarkSingleRead = async (id: string) => {
    try {
      await fetch('/api/v3/notifications/mark-read', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      });
      setNotifications((prev) =>
        prev.map((n) => (n.id === id ? { ...n, readAt: new Date().toISOString() } : n))
      );
      setUnreadCount((prev) => Math.max(0, prev - 1));
    } catch {
      // Ignore
    }
  };

  const handleApproveContent = async (notif: NotificationItem, approvalId: string) => {
    setActionMessage(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${approvalId}/approve`, {
        method: 'POST',
      });
      if (res.status === 409) {
        setActionMessage('Anfrage wurde bereits von einem anderen Administrator entschieden.');
      } else if (res.ok) {
        setActionMessage('Freigabe erfolgreich erteilt.');
      }
      await handleMarkSingleRead(notif.id);
      fetchNotifications();
    } catch {
      setActionMessage('Netzwerkfehler beim Erteilen der Freigabe.');
    }
  };

  const handleDenyContent = async (notif: NotificationItem, approvalId: string) => {
    setActionMessage(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${approvalId}/deny`, {
        method: 'POST',
      });
      if (res.status === 409) {
        setActionMessage('Anfrage wurde bereits von einem anderen Administrator entschieden.');
      } else if (res.ok) {
        setActionMessage('Anfrage abgelehnt.');
      }
      await handleMarkSingleRead(notif.id);
      fetchNotifications();
    } catch {
      setActionMessage('Netzwerkfehler beim Ablehnen.');
    }
  };

  const handleToggleWebPush = async () => {
    if (!('Notification' in window) || !('serviceWorker' in navigator)) {
      alert('Ihr Browser unterstützt keine WebPush-Benachrichtigungen.');
      return;
    }

    try {
      const perm = await Notification.requestPermission();
      if (perm === 'granted') {
        setWebPushEnabled(true);
        const reg = await navigator.serviceWorker.register('/sw.js');
        const vapidRes = await fetch('/api/v3/notifications/vapid-key');
        if (vapidRes.ok) {
          const { publicKey } = await vapidRes.json();
          let sub = await reg.pushManager.getSubscription();
          if (!sub) {
            sub = await reg.pushManager.subscribe({
              userVisibleOnly: true,
              applicationServerKey: publicKey,
            });
          }
          const subJson = sub.toJSON();
          await fetch('/api/v3/notifications/push-subscriptions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              endpoint: sub.endpoint,
              keys: {
                p256dh: subJson.keys?.p256dh || '',
                auth: subJson.keys?.auth || '',
              },
            }),
          });
        }
      }
    } catch (e) {
      console.error('[NotificationCenter] Push activation error:', e);
    }
  };

  const filteredNotifs = filter === 'unread' ? notifications.filter((n) => !n.readAt) : notifications;

  const formatRelativeTime = (isoString: string): string => {
    const diffSec = Math.floor((Date.now() - new Date(isoString).getTime()) / 1000);
    if (diffSec < 60) return 'vor wenigen Sekunden';
    if (diffSec < 3600) return `vor ${Math.floor(diffSec / 60)} Min.`;
    if (diffSec < 86400) return `vor ${Math.floor(diffSec / 3600)} Std.`;
    return `vor ${Math.floor(diffSec / 86400)} Tagen`;
  };

  return (
    <div style={{ position: 'relative' }}>
      {/* Bell Trigger Button */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        title="Benachrichtigungen"
        style={{
          position: 'relative',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '42px',
          height: '42px',
          borderRadius: '12px',
          backgroundColor: isOpen ? 'rgba(56, 189, 248, 0.2)' : 'rgba(255, 255, 255, 0.05)',
          border: '1px solid rgba(255, 255, 255, 0.1)',
          color: '#f8fafc',
          cursor: 'pointer',
          transition: 'all 0.15s ease-in-out',
        }}
      >
        <span style={{ fontSize: '18px' }}>🔔</span>
        {unreadCount > 0 && (
          <span
            style={{
              position: 'absolute',
              top: '-4px',
              right: '-4px',
              backgroundColor: '#ef4444',
              color: '#ffffff',
              fontSize: '11px',
              fontWeight: 700,
              borderRadius: '10px',
              padding: '2px 6px',
              lineHeight: 1,
              boxShadow: '0 0 8px rgba(239, 68, 68, 0.6)',
            }}
          >
            {unreadCount}
          </span>
        )}
      </button>

      {/* Floating Popover Center */}
      {isOpen && (
        <div
          style={{
            position: 'absolute',
            top: '52px',
            right: 0,
            width: '380px',
            backgroundColor: '#1e293b',
            borderRadius: '16px',
            border: '1px solid rgba(255, 255, 255, 0.12)',
            boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.5), 0 8px 10px -6px rgba(0, 0, 0, 0.3)',
            zIndex: 1000,
            overflow: 'hidden',
          }}
        >
          {/* Header */}
          <div
            style={{
              padding: '16px 20px',
              backgroundColor: '#0f172a',
              borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <div>
              <h3 style={{ margin: 0, fontSize: '16px', fontWeight: 600, color: '#f8fafc' }}>
                Benachrichtigungen
              </h3>
              <p style={{ margin: '2px 0 0 0', fontSize: '12px', color: '#94a3b8' }}>
                Facebook-style Notification Center
              </p>
            </div>
            {unreadCount > 0 && (
              <button
                onClick={handleMarkAllRead}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#38bdf8',
                  fontSize: '12px',
                  cursor: 'pointer',
                  fontWeight: 500,
                }}
              >
                Alle gelesen
              </button>
            )}
          </div>

          {/* WebPush Status Bar */}
          <div
            style={{
              padding: '8px 20px',
              backgroundColor: webPushEnabled ? 'rgba(34,197,94,0.1)' : 'rgba(234,179,8,0.1)',
              borderBottom: '1px solid rgba(255, 255, 255, 0.05)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              fontSize: '12px',
            }}
          >
            <span style={{ color: webPushEnabled ? '#4ade80' : '#fde047' }}>
              {webPushEnabled ? '✓ WebPush im Browser aktiv' : 'WebPush inaktiv'}
            </span>
            {!webPushEnabled && (
              <button
                onClick={handleToggleWebPush}
                style={{
                  padding: '2px 8px',
                  borderRadius: '6px',
                  border: 'none',
                  backgroundColor: '#eab308',
                  color: '#0f172a',
                  fontWeight: 600,
                  fontSize: '11px',
                  cursor: 'pointer',
                }}
              >
                Aktivieren
              </button>
            )}
          </div>

          {/* Filters */}
          <div
            style={{
              display: 'flex',
              padding: '8px 16px',
              gap: '8px',
              borderBottom: '1px solid rgba(255, 255, 255, 0.05)',
            }}
          >
            <button
              onClick={() => setFilter('unread')}
              style={{
                padding: '4px 12px',
                borderRadius: '8px',
                border: 'none',
                backgroundColor: filter === 'unread' ? '#38bdf8' : 'transparent',
                color: filter === 'unread' ? '#0f172a' : '#94a3b8',
                fontSize: '12px',
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Ungelesen ({notifications.filter((n) => !n.readAt).length})
            </button>
            <button
              onClick={() => setFilter('all')}
              style={{
                padding: '4px 12px',
                borderRadius: '8px',
                border: 'none',
                backgroundColor: filter === 'all' ? '#38bdf8' : 'transparent',
                color: filter === 'all' ? '#0f172a' : '#94a3b8',
                fontSize: '12px',
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Alle ({notifications.length})
            </button>
          </div>

          {/* Action Result Toast */}
          {actionMessage && (
            <div
              style={{
                padding: '8px 16px',
                backgroundColor: 'rgba(56, 189, 248, 0.15)',
                color: '#38bdf8',
                fontSize: '12px',
                borderBottom: '1px solid rgba(56, 189, 248, 0.3)',
              }}
            >
              {actionMessage}
            </div>
          )}

          {/* Notification Items List */}
          <div style={{ maxHeight: '340px', overflowY: 'auto' }}>
            {filteredNotifs.length === 0 ? (
              <div style={{ padding: '32px', textAlign: 'center', color: '#64748b' }}>
                <span style={{ fontSize: '24px' }}>✨</span>
                <p style={{ margin: '8px 0 0 0', fontSize: '13px' }}>Keine Benachrichtigungen</p>
              </div>
            ) : (
              filteredNotifs.map((item) => (
                <div
                  key={item.id}
                  style={{
                    padding: '14px 16px',
                    borderBottom: '1px solid rgba(255, 255, 255, 0.05)',
                    backgroundColor: item.readAt ? 'transparent' : 'rgba(56, 189, 248, 0.04)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '6px',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <span style={{ fontSize: '13px', fontWeight: 600, color: '#f8fafc' }}>
                      {item.title}
                    </span>
                    <span style={{ fontSize: '11px', color: '#64748b' }}>
                      {formatRelativeTime(item.createdAt)}
                    </span>
                  </div>

                  <p style={{ margin: 0, fontSize: '13px', color: '#cbd5e1', lineHeight: '1.4' }}>
                    {item.body}
                  </p>

                  {/* Inline Approval Actions */}
                  {item.type === 'approval_request' && item.resourceId && !item.readAt && (
                    <div style={{ display: 'flex', gap: '8px', marginTop: '6px' }}>
                      <button
                        onClick={() => handleApproveContent(item, item.resourceId!)}
                        style={{
                          padding: '6px 12px',
                          borderRadius: '8px',
                          border: 'none',
                          backgroundColor: '#22c55e',
                          color: '#ffffff',
                          fontSize: '12px',
                          fontWeight: 600,
                          cursor: 'pointer',
                        }}
                      >
                        Erlauben
                      </button>
                      <button
                        onClick={() => handleDenyContent(item, item.resourceId!)}
                        style={{
                          padding: '6px 12px',
                          borderRadius: '8px',
                          border: 'none',
                          backgroundColor: 'rgba(239, 68, 68, 0.2)',
                          color: '#f87171',
                          fontSize: '12px',
                          fontWeight: 600,
                          cursor: 'pointer',
                        }}
                      >
                        Ablehnen
                      </button>
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
};
