const API_BASE = '/api';

// Helper to handle Auth and JSON responses
async function fetchClient(endpoint, options = {}) {
  const token = localStorage.getItem('XG2G_API_TOKEN');
  const headers = {
    'Content-Type': 'application/json',
    ...options.headers,
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers,
  });

  if (response.status === 401) {
    // Dispatch event so App.jsx can show auth prompt
    window.dispatchEvent(new CustomEvent('auth-required'));
    throw new Error('Unauthorized');
  }

  if (!response.ok) {
    throw new Error(`API Error: ${response.statusText}`);
  }

  // specific check for 204 No Content
  if (response.status === 204) return null;

  return response.json();
}

export async function getHealth() {
  return fetchClient('/system/health');
}

export async function getConfig() {
  return fetchClient('/system/config');
}

export async function getBouquets() {
  return fetchClient('/services/bouquets');
}

export async function getChannels(bouquet) {
  const query = bouquet ? `?bouquet=${encodeURIComponent(bouquet)}` : '';
  return fetchClient(`/services${query}`);
}

export async function toggleService(id, enabled) {
  return fetchClient(`/services/${encodeURIComponent(id)}/toggle`, {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  });
}

export async function getRecentLogs() {
  // api defines it as array of LogEntry
  return fetchClient('/logs');
}

export async function regeneratePlaylist() {
  // Calls POST /system/refresh
  return fetchClient('/system/refresh', {
    method: 'POST',
  });
}

