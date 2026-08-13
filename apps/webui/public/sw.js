// xg2g WebPush Service Worker (sw.js)

self.addEventListener('push', (event) => {
  if (!event.data) return;

  try {
    const data = event.data.json();
    const title = data.title || 'xg2g - Freigabe erforderlich';
    const body = data.body || 'Eine neue Freigabeanfrage wartet auf Ihre Entscheidung.';
    const resourceId = data.resourceId || '';

    const options = {
      body: body,
      icon: '/pwa-192x192.png',
      badge: '/badge.png',
      tag: resourceId ? `approval-${resourceId}` : 'xg2g-notification',
      data: {
        url: resourceId ? `/settings?section=parental&approvalId=${resourceId}` : '/settings?section=parental',
      },
      actions: [
        { action: 'open', title: 'Öffnen' },
        { action: 'dismiss', title: 'Schließen' },
      ],
    };

    event.waitUntil(self.registration.showNotification(title, options));
  } catch (err) {
    console.error('[WebPush SW] Error displaying push notification:', err);
  }
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  if (event.action === 'dismiss') return;

  const targetUrl = new URL(
    event.notification.data?.url || '/settings?section=parental',
    self.location.origin
  ).href;

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windowClients) => {
      for (const client of windowClients) {
        if (client.url === targetUrl && 'focus' in client) {
          return client.focus();
        }
      }
      if (clients.openWindow) {
        return clients.openWindow(targetUrl);
      }
    })
  );
});
