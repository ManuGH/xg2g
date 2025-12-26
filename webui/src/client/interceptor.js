// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Global fetch interceptor for 401 handling
// This is a compatibility layer that maintains the original behavior
// while using the new TypeScript client underneath

const originalFetch = window.fetch;

window.fetch = async (...args) => {
  const response = await originalFetch(...args);

  if (response.status === 401) {
    // Only trigger if this is an API call (not for assets, etc.)
    const url = args[0];
    console.warn('[Security DEBUG] 401 Intercepted for URL:', url);
    if (typeof url === 'string' && url.includes('/api/')) {
      console.warn('[Security] 401 Unauthorized detected on:', url);
      window.dispatchEvent(new Event('auth-required'));
    }
  }

  return response;
};
