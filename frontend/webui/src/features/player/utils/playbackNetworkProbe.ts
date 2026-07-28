import type { CapabilitySnapshot } from './playbackCapabilities';
import type { PlaybackClientContext } from './playbackRequestProfile';

const PROBE_BYTES = 512 * 1024;
const PROBE_TIMEOUT_MS = 3000;
const PROBE_HEADER = 'X-XG2G-Playback-Probe';
const PROBE_CACHE_TTL_MS = 2 * 60 * 1000;
const CONSTRAINED_CACHE_TTL_MS = 15 * 1000;

type CachedPlaybackNetworkProbe = {
	expiresAt: number;
	value: PlaybackNetworkProbe;
};

type NetworkInformationLike = {
	type?: string;
	effectiveType?: string;
	downlink?: number;
	rtt?: number;
	saveData?: boolean;
	addEventListener?: (type: 'change', listener: () => void) => void;
	removeEventListener?: (type: 'change', listener: () => void) => void;
};

const probeCache = new Map<string, CachedPlaybackNetworkProbe>();
const probesInFlight = new Map<string, Promise<PlaybackNetworkProbe | undefined>>();
let networkGeneration = 0;
let windowInvalidationInstalled = false;
let observedConnection: NetworkInformationLike | null = null;


export type PlaybackNetworkProbe =
  | { kind: 'lan' }
  | { kind: 'measured'; downlinkMbps: number }
  | { kind: 'constrained' };

type PlaybackNetworkPolicyOptions = {
	forceConstrained?: boolean;
};

function mobileDataSavingsEnabled(): boolean {
	try {
		return typeof localStorage !== 'undefined'
			&& localStorage.getItem('xg2g.settings.saveMobileData') === 'true';
	} catch {
		return false;
	}
}

function requestedMaxBitrateKbps(
	context: PlaybackClientContext,
	options: PlaybackNetworkPolicyOptions,
): number | undefined {
	if (options.forceConstrained) {
		return 1000;
	}

	const network = context.network;
	const isSlowNetwork = network?.effectiveType === 'slow-2g'
		|| network?.effectiveType === '2g'
		|| network?.effectiveType === '3g'
		|| (typeof network?.downlinkMbps === 'number'
			&& network.downlinkMbps > 0
			&& network.downlinkMbps < 2.5);
	const isCellularOrMetered = network?.kind === 'cellular' || network?.metered === true;

	if (
		network?.saveData === true
		|| isSlowNetwork
		|| (isCellularOrMetered && mobileDataSavingsEnabled())
	) {
		return 2500;
	}
	return undefined;
}

function currentNetworkConnection(): NetworkInformationLike | null {
	if (typeof navigator === 'undefined') {
		return null;
	}
	return (navigator as Navigator & { connection?: NetworkInformationLike }).connection ?? null;
}

function currentNetworkFingerprint(): string | null {
	const connection = currentNetworkConnection();
	if (!connection) {
		return null;
	}
	const values = [
		connection.type,
		connection.effectiveType,
		connection.downlink,
		connection.rtt,
		connection.saveData,
	];
	if (values.every((value) => value === undefined)) {
		return null;
	}
	return values.map((value) => String(value ?? 'unknown')).join('|');
}

function invalidatePlaybackNetworkCache(): void {
	networkGeneration += 1;
	probeCache.clear();
	probesInFlight.clear();
}

function ensureNetworkInvalidationListeners(): void {
	if (typeof window === 'undefined') {
		return;
	}
	if (!windowInvalidationInstalled) {
		windowInvalidationInstalled = true;
		window.addEventListener('online', invalidatePlaybackNetworkCache);
		window.addEventListener('offline', invalidatePlaybackNetworkCache);
	}
	const connection = currentNetworkConnection();
	if (connection !== observedConnection) {
		observedConnection?.removeEventListener?.('change', invalidatePlaybackNetworkCache);
		observedConnection = connection;
		invalidatePlaybackNetworkCache();
		observedConnection?.addEventListener?.('change', invalidatePlaybackNetworkCache);
	}
}

async function runPlaybackNetworkProbe(apiBase: string): Promise<PlaybackNetworkProbe | undefined> {

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

export function measurePlaybackNetwork(apiBase: string): Promise<PlaybackNetworkProbe | undefined> {
	ensureNetworkInvalidationListeners();
	const now = Date.now();
	const networkFingerprint = currentNetworkFingerprint();
	const cacheKey = networkFingerprint ? `${apiBase}|${networkFingerprint}` : apiBase;
	const canReusePositiveEvidence = networkFingerprint !== null;
	const cached = probeCache.get(cacheKey);
	if (
		cached
		&& cached.expiresAt > now
		&& (cached.value.kind === 'constrained' || canReusePositiveEvidence)
	) {
		return Promise.resolve(cached.value);
	}
	if (cached) {
		probeCache.delete(cacheKey);
	}

	const inFlight = probesInFlight.get(cacheKey);
	if (inFlight) {
		return inFlight;
	}

	const generation = networkGeneration;
	const probe = runPlaybackNetworkProbe(apiBase).then((result) => {
		if (generation !== networkGeneration) {
			return undefined;
		}
		if (
			result
			&& (result.kind === 'constrained' || canReusePositiveEvidence)
		) {
			const ttl = result.kind === 'constrained' ? CONSTRAINED_CACHE_TTL_MS : PROBE_CACHE_TTL_MS;
			probeCache.set(cacheKey, { expiresAt: Date.now() + ttl, value: result });
		}
		return result;
	});
	probesInFlight.set(cacheKey, probe);
	const clearIfCurrent = () => {
		if (probesInFlight.get(cacheKey) === probe) {
			probesInFlight.delete(cacheKey);
		}
	};
	void probe.then(clearIfCurrent, clearIfCurrent);
	return probe;
}


export function applyPlaybackNetworkProbe(
	capabilities: CapabilitySnapshot,
	context: PlaybackClientContext,
	probe: PlaybackNetworkProbe | undefined,
	options: PlaybackNetworkPolicyOptions = {},
): PlaybackClientContext {
	let nextContext = context;
	if (probe != null && probe.kind !== 'lan') {
		const downlinkMbps = probe.kind === 'constrained' ? 1 : probe.downlinkMbps;
		capabilities.networkContext = {
			...capabilities.networkContext,
			kind: 'measured',
			downlinkKbps: Math.max(1, Math.round(downlinkMbps * 1000)),
		};
		nextContext = {
			...context,
			network: {
				...context.network,
				kind: 'measured',
				downlinkMbps,
			},
		};
	}

	const maxBitrateKbps = requestedMaxBitrateKbps(nextContext, options);
	if (maxBitrateKbps !== undefined) {
		capabilities.networkContext = {
			...capabilities.networkContext,
			maxBitrateKbps,
		};
	}
	return nextContext;
}
