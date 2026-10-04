import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  debugError,
  debugLog,
  debugWarn,
  debugDiagnosticError,
  formatDiagnosticSummary,
  summarizeDiagnostic,
  isDebugLogsEnabled,
} from './logging';

describe('debug logging & allowlisted diagnostic error handling', () => {
  const originalDev = import.meta.env.DEV;
  const originalTestLogs = import.meta.env.VITE_XG2G_TEST_DEBUG_LOGS;

  afterEach(() => {
    (import.meta.env as any).DEV = originalDev;
    (import.meta.env as any).VITE_XG2G_TEST_DEBUG_LOGS = originalTestLogs;
    vi.restoreAllMocks();
  });

  it('stays quiet during tests unless explicitly enabled', () => {
    const log = vi.spyOn(console, 'log').mockImplementation(() => {});
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});

    debugLog('hidden log');
    debugWarn('hidden warn');
    debugError('hidden error');
    debugDiagnosticError('admin.test.op', new Error('hidden diagnostic error'));

    expect(log).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
    expect(error).not.toHaveBeenCalled();
  });

  it('keeps production debug logging disabled even if test debug logs flag is set', () => {
    (import.meta.env as any).DEV = false;
    (import.meta.env as any).VITE_XG2G_TEST_DEBUG_LOGS = '1';

    expect(isDebugLogsEnabled()).toBe(false);

    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    debugDiagnosticError('admin.profiles.load', new Error('some error'));
    expect(error).not.toHaveBeenCalled();
  });

  describe('allowlisted diagnostic summary', () => {
    it('summarizes validated status, error code, and correlation ID', () => {
      const err = {
        status: 403,
        code: 'FORBIDDEN',
        requestId: 'req-abc-999',
        message: 'Forbidden access to profile',
        responseBody: '{"secret":"do-not-leak"}',
      };

      const summary = summarizeDiagnostic('admin.profiles.load', err);
      expect(summary).toEqual({
        operation: 'admin.profiles.load',
        status: 403,
        code: 'FORBIDDEN',
        correlationId: 'req-abc-999',
      });

      const formatted = formatDiagnosticSummary('admin.profiles.load', err);
      expect(formatted).toBe('[admin.profiles.load] code=FORBIDDEN status=403 correlationId=req-abc-999');
      expect(formatted).not.toContain('Forbidden access to profile');
      expect(formatted).not.toContain('do-not-leak');
    });

    it('derives standard error code from status when code is not explicit', () => {
      const err = { status: 500, requestId: 'req-500' };
      const formatted = formatDiagnosticSummary('admin.devices.load', err);
      expect(formatted).toBe('[admin.devices.load] code=INTERNAL_ERROR status=500 correlationId=req-500');
    });

    it('classifies TypeError as NETWORK_ERROR without exposing URL or parameters', () => {
      const err = new TypeError('Failed to fetch https://user:super_secret_pw@internal.host/api/secret');
      const formatted = formatDiagnosticSummary('admin.family.load', err);
      expect(formatted).toBe('[admin.family.load] code=NETWORK_ERROR');
      expect(formatted).not.toContain('super_secret_pw');
      expect(formatted).not.toContain('internal.host');
    });
  });

  describe('injected secret redaction proof', () => {
    it('proves that injected secrets in error messages and response bodies are excluded from the logged output', () => {
      (import.meta.env as any).VITE_XG2G_TEST_DEBUG_LOGS = '1';
      expect(isDebugLogsEnabled()).toBe(true);

      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      const secretPayload = 'SUPER_SECRET_BEARER_TOKEN_987654321';
      const secretDbPassword = 'DB_PASSWORD_XYZ_12345';
      const secretResponseBody = '{"token": "JWT_SECRET_PAYLOAD_ABC"}';

      const maliciousErr = Object.assign(
        new Error(`Database failed with password=${secretDbPassword} and auth=${secretPayload}`),
        {
          status: 503,
          code: 'SERVICE_UNAVAILABLE',
          requestId: 'req-safe-trace-123',
          response: secretResponseBody,
          detail: 'Stack trace leaking credentials at /etc/passwd',
        }
      );

      debugDiagnosticError('admin.concurrency.save', maliciousErr);

      expect(consoleErrorSpy).toHaveBeenCalledTimes(1);
      const loggedOutput = String(consoleErrorSpy.mock.calls[0]?.[0] ?? '');

      // Assert allowlisted fields are present
      expect(loggedOutput).toContain('[admin.concurrency.save]');
      expect(loggedOutput).toContain('code=SERVICE_UNAVAILABLE');
      expect(loggedOutput).toContain('status=503');
      expect(loggedOutput).toContain('correlationId=req-safe-trace-123');

      // Assert that none of the injected secrets or arbitrary details are logged
      expect(loggedOutput).not.toContain(secretPayload);
      expect(loggedOutput).not.toContain(secretDbPassword);
      expect(loggedOutput).not.toContain(secretResponseBody);
      expect(loggedOutput).not.toContain('JWT_SECRET_PAYLOAD_ABC');
      expect(loggedOutput).not.toContain('Database failed');
      expect(loggedOutput).not.toContain('/etc/passwd');
      expect(loggedOutput).not.toContain('Stack trace');
    });
  });
});
