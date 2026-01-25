
// Basic Telemetry Service
// In real world, this would batch and send to /api/telemetry
// For now, it logs to console and exposes hooks for testing.

import { debugLog } from '../utils/logging';

export type TelemetryEventType =
  | 'ui.contract.consumed'
  | 'ui.contract.violation'
  | 'ui.error'
  | 'ui.failclosed';

export interface TelemetryEvent {
  type: TelemetryEventType;
  payload: any;
  meta?: any;
}

class TelemetryService {
  private queue: TelemetryEvent[] = [];
  private listeners: ((event: TelemetryEvent) => void)[] = [];

  public emit(type: TelemetryEventType, payload: any) {
    const event: TelemetryEvent = {
      type,
      payload,
      meta: {
        timestamp: Date.now(),
        requestId: 'unknown', // Should be enriched via context
      }
    };

    this.queue.push(event);
    this.listeners.forEach(l => l(event));

    // Console output for "Dashboard" visibility (dev mode)
    debugLog(`[TELEMETRY] ${type}`, payload);
  }

  // Test hook
  public subscribe(callback: (event: TelemetryEvent) => void) {
    this.listeners.push(callback);
    return () => {
      this.listeners = this.listeners.filter(l => l !== callback);
    };
  }

  public getEvents() {
    return [...this.queue];
  }

  public clear() {
    this.queue = [];
  }
}

export const telemetry = new TelemetryService();
