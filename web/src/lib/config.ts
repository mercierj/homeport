// Environment configuration for the application
// Uses Vite's import.meta.env for environment variable access

export const config = {
  api: {
    // In dev mode, use empty string to leverage Vite's proxy (same-origin, no CORS issues)
    baseUrl: import.meta.env.VITE_API_URL || '',
    version: 'v1',
  },
  auth: {
    tokenExpiryMs: Number(import.meta.env.VITE_TOKEN_EXPIRY_MS) || 24 * 60 * 60 * 1000,
  },
} as const;

// Computed API base URL with version
export const API_BASE = `${config.api.baseUrl}/api/${config.api.version}`;
