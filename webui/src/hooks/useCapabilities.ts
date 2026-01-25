
import { useState, useEffect } from 'react';
import { Capabilities } from '../contracts/PolicyEngine';

// Default to V3 Legacy (No strict contracts)
const DEFAULT_CAPABILITIES: Capabilities = {
  'contracts.playbackInfoDecision': 'absent'
};

// In real app: Fetch from /api/v3/system/capabilities
// Here: Mock implementation or attempt fetch
export function useCapabilities() {
  const [capabilities] = useState<Capabilities>(DEFAULT_CAPABILITIES);
  const [loading] = useState(false); // Assume loaded for simplified synchronous testing loops, or use async

  useEffect(() => {
    // Attempt fetch?
    // For this environment, we assume specific capabilities are injected via Mock/Test
    // or default to legacy.

    // Let's implement a fake "window.flags" or similar for testing?
    // Or just hardcode to "Mock V4" if we assume we are building V4?

    // Strict Engineering: We default to LEGACY effectively (safe fallback).
    // But if we want to enable V4 features, we must detect them.

    // For the purpose of "Version Matrix", we want to expose a way to set this.
    // We'll stick to DEFAULT unless mocked.
  }, []);

  return {
    capabilities,
    loading
  };
}
