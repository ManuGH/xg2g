const API_BASE = '/api';

export async function getHealth() {
  const res = await fetch(`${API_BASE}/health`);
  if (!res.ok) throw new Error('Failed to fetch health');
  return res.json();
}

export async function getConfig() {
  const res = await fetch(`${API_BASE}/config`);
  if (!res.ok) throw new Error('Failed to fetch config');
  return res.json();
}

export async function getBouquets() {
  const res = await fetch(`${API_BASE}/bouquets`);
  if (!res.ok) throw new Error('Failed to fetch bouquets');
  return res.json();
}

export async function getChannels(bouquet) {
  const params = new URLSearchParams();
  if (bouquet) params.append('bouquet', bouquet);
  const res = await fetch(`${API_BASE}/channels?${params.toString()}`);
  if (!res.ok) throw new Error('Failed to fetch channels');
  return res.json();
}

export async function getRecentLogs() {
  const res = await fetch(`${API_BASE}/logs/recent`);
  if (!res.ok) throw new Error('Failed to fetch logs');
  return res.json();
}
