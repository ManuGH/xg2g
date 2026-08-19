import React, { useState, useEffect } from 'react';
import { debugError } from '../../utils/logging';
import styles from './NotificationCenter.module.css';

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
		debugError('[NotificationCenter] Push activation error:', e);
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
    <div className={styles.root}>
      {/* Bell Trigger Button */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        title="Benachrichtigungen"
        aria-label="Benachrichtigungen"
        aria-expanded={isOpen}
        className={`${styles.bell} ${isOpen ? styles.bellOpen : ''}`}
      >
        <span className={styles.bellIcon}>🔔</span>
        {unreadCount > 0 && <span className={styles.badge}>{unreadCount}</span>}
      </button>

      {/* Floating Popover Center */}
      {isOpen && (
        <div className={styles.popover}>
          {/* Header */}
          <div className={styles.header}>
            <div>
              <h3 className={styles.headerTitle}>Benachrichtigungen</h3>
              <p className={styles.headerSubtitle}>Facebook-style Notification Center</p>
            </div>
            {unreadCount > 0 && (
              <button onClick={handleMarkAllRead} className={styles.markAllButton}>
                Alle gelesen
              </button>
            )}
          </div>

          {/* WebPush Status Bar */}
          <div className={`${styles.pushBar} ${webPushEnabled ? styles.pushBarActive : styles.pushBarIdle}`}>
            <span className={webPushEnabled ? styles.pushLabelActive : styles.pushLabelIdle}>
              {webPushEnabled ? '✓ WebPush im Browser aktiv' : 'WebPush inaktiv'}
            </span>
            {!webPushEnabled && (
              <button onClick={handleToggleWebPush} className={styles.pushEnableButton}>
                Aktivieren
              </button>
            )}
          </div>

          {/* Filters */}
          <div className={styles.filters}>
            <button
              onClick={() => setFilter('unread')}
              className={`${styles.filterButton} ${filter === 'unread' ? styles.filterButtonActive : ''}`}
            >
              Ungelesen ({notifications.filter((n) => !n.readAt).length})
            </button>
            <button
              onClick={() => setFilter('all')}
              className={`${styles.filterButton} ${filter === 'all' ? styles.filterButtonActive : ''}`}
            >
              Alle ({notifications.length})
            </button>
          </div>

          {/* Action Result Toast */}
          {actionMessage && <div className={styles.toast}>{actionMessage}</div>}

          {/* Notification Items List */}
          <div className={styles.list}>
            {filteredNotifs.length === 0 ? (
              <div className={styles.emptyState}>
                <span className={styles.emptyIcon}>✨</span>
                <p className={styles.emptyText}>Keine Benachrichtigungen</p>
              </div>
            ) : (
              filteredNotifs.map((item) => (
                <div
                  key={item.id}
                  className={`${styles.item} ${item.readAt ? '' : styles.itemUnread}`}
                >
                  <div className={styles.itemHeader}>
                    <span className={styles.itemTitle}>{item.title}</span>
                    <span className={styles.itemTime}>{formatRelativeTime(item.createdAt)}</span>
                  </div>

                  <p className={styles.itemBody}>{item.body}</p>

                  {/* Inline Approval Actions */}
                  {item.type === 'approval_request' && item.resourceId && !item.readAt && (
                    <div className={styles.itemActions}>
                      <button
                        onClick={() => handleApproveContent(item, item.resourceId!)}
                        className={`${styles.actionButton} ${styles.approveButton}`}
                      >
                        Erlauben
                      </button>
                      <button
                        onClick={() => handleDenyContent(item, item.resourceId!)}
                        className={`${styles.actionButton} ${styles.denyButton}`}
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
