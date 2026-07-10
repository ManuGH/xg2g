import type { CapabilitySnapshot } from './playbackCapabilities';
import type { PlaybackClientContext } from './playbackRequestProfile';

const PROBE_BYTES = 512 * 1024;
const PROBE_TIMEOUT_MS = 3000;
const PROBE_HEADER = 'X-XG2G-Playback-Probe';

export type PlaybackNetworkProbe =
  | { kind: 'lan' }
  | { kind: 'measured'; downlinkMbps: number }
  | { kind: 'constrained' };

export async function measurePlaybackNetwork(apiBase: string): Promise<PlaybackNetworkProbe | undefined> {
	if (typeof window === 'undefined' || navigator.onLine === false) {
		return undefined;
	}

	const controller = new AbortController();
	const timeout = window.setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
	const startedAt = performance.now();
	try {
		const response = await fetch(`${apiBase}/system/healthz?playbackProbe=1`, {
			cache: 'no-store',
			credentials: 'same-origin',
			signal: controller.signal,
		});
		if (response.status === 204 && response.headers.get(PROBE_HEADER) === 'lan') {
			return { kind: 'lan' };
		}
		if (!response.ok || response.headers.get(PROBE_HEADER) !== 'measured') {
			return undefined;
		}

		const payload = await response.arrayBuffer();
		const elapsedMs = performance.now() - startedAt;
		if (payload.byteLength !== PROBE_BYTES || elapsedMs <= 0) {
			return undefined;
		}
		return { kind: 'measured', downlinkMbps: (payload.byteLength * 8) / elapsedMs / 1000 };
	} catch (error) {
		if (error instanceof DOMException && error.name === 'AbortError') {
			return { kind: 'constrained' };
		}
		return undefined;
	} finally {
		window.clearTimeout(timeout);
	}
}

export function applyPlaybackNetworkProbe(
	capabilities: CapabilitySnapshot,
	context: PlaybackClientContext,
	probe: PlaybackNetworkProbe | undefined,
): PlaybackClientContext {
	if (probe == null || probe.kind === 'lan') {
		return context;
	}

	const downlinkMbps = probe.kind === 'constrained' ? 1 : probe.downlinkMbps;
	capabilities.networkContext = {
		...capabilities.networkContext,
		kind: 'measured',
		downlinkKbps: Math.max(1, Math.round(downlinkMbps * 1000)),
	};
	return {
		...context,
		network: {
			...context.network,
			kind: 'measured',
			downlinkMbps,
		},
	};
}
