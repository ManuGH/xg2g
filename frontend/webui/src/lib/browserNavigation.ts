export function reloadWindowLocation(token?: string): void {
  if (typeof window !== 'undefined') {
    const normalizedToken = String(token || '').trim();
    if (normalizedToken) {
      const url = new URL(window.location.href);
      url.hash = new URLSearchParams({ xg2g_boot_token: normalizedToken }).toString();
      window.location.replace(url.toString());
      return;
    }
    window.location.reload();
  }
}
