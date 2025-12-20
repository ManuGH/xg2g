// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

/**
 * Global API Error Interceptor
 *
 * Automatically handles authentication errors (401) across all API calls.
 * This prevents having to manually check for 401 in every component.
 */

import { OpenAPI } from './core/OpenAPI';

/**
 * Wraps the fetch function to intercept responses and handle 401 errors globally.
 */
const originalFetch = window.fetch;

window.fetch = async (...args) => {
  const response = await originalFetch(...args);

  // If we get a 401 Unauthorized, trigger the auth modal
  if (response.status === 401) {
    // Only trigger if this is an API call (not for assets, etc.)
    const url = args[0];
    if (typeof url === 'string' && url.includes('/api/')) {
      console.warn('[Security] 401 Unauthorized detected on:', url);
      window.dispatchEvent(new Event('auth-required'));
    }
  }

  return response;
};

/**
 * Initialize the interceptor when the token changes.
 * Updates the OpenAPI client configuration.
 */
export function initializeAuth() {
  const storedToken = localStorage.getItem('XG2G_API_TOKEN');
  if (storedToken) {
    OpenAPI.TOKEN = storedToken;
    console.log('[Security] API token loaded from localStorage');
  } else {
    console.warn('[Security] No API token found - authentication required');
  }
}

/**
 * Security monitor that logs authentication state changes.
 */
export function enableSecurityMonitoring() {
  // Log when token is added/removed from localStorage
  const originalSetItem = localStorage.setItem;
  localStorage.setItem = function (key) {
    if (key === 'XG2G_API_TOKEN') {
      console.log('[Security] API token updated');
    }
    originalSetItem.apply(this, arguments);
  };

  const originalRemoveItem = localStorage.removeItem;
  localStorage.removeItem = function (key) {
    if (key === 'XG2G_API_TOKEN') {
      console.warn('[Security] API token removed');
    }
    originalRemoveItem.apply(this, arguments);
  };
}

// Auto-initialize on import
initializeAuth();
enableSecurityMonitoring();
