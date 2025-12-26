// Legacy compatibility: OpenAPI configuration object
// Re-exports the modern client configuration

import { client } from '../../client-ts/client.gen';

// Internal token storage
let _token: string | null = null;

export const OpenAPI = {
  BASE: '/api/v3',

  get TOKEN() {
    return _token;
  },

  set TOKEN(value: string | null) {
    _token = value;
    // Configure client with new token
    if (value) {
      client.interceptors.request.use((request) => {
        request.headers.set('Authorization', `Bearer ${value}`);
        return request;
      });
    }
  },

  setConfig(config: { BASE?: string; TOKEN?: string }) {
    if (config.BASE !== undefined) {
      this.BASE = config.BASE;
      client.setConfig({ baseUrl: config.BASE });
    }
    if (config.TOKEN !== undefined) {
      this.TOKEN = config.TOKEN;
    }
  }
};
