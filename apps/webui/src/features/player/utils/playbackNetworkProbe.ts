import type { CapabilitySnapshot } from './playbackCapabilities';
import type { PlaybackClientContext } from './playbackRequestProfile';

const PROBE_BYTES = 512 * 1024;
const PROBE_TIMEOUT_MS = 3000;
const PROBE_HEADER = 'X-XG2G-Playback-Probe';
const PROBE_CACHE_TTL_MS = 2 * 60 * 1000;
const CONSTRAINED_CACHE_TTL_MS = 15 * 1000;
// TTL used when the platform gives us no way to notice a network change.
// Safari (iOS and macOS) ships no Network Information API, so there is no
// connection fingerprint and no 'change' event — without this the cache was
// unreachable on every Apple client and each playback start re-measured. A
// shorter window bounds how stale a reused verdict can be; the online/offline
// listeners still invalidate on the transitions the platform does report.
const UNKEYED_CACHE_TTL_MS = 45 * 1000;
// Below this the response body was already buffered when the headers resolved,
// so the transfer window is not a measurement of anything.
const MIN_TRANSFER_WINDOW_MS = 5;
// Number of recent samples the reported downlink is a median over.
const SAMPLE_WINDOW = 3;

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
const sampleHistory = new Map<string, number[]>();
let networkGeneration = 0;
let windowInvalidationInstalled = false;
let observedConnection: NetworkInformationLike | null = null;


export type PlaybackNetworkProbe =
  | { kind: 'lan' }
  | { kind: 'measured'; downlinkMbps: number }
  | { kind: 'constrained' };

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
	sampleHistory.clear();
}

/**
 * Drop every cached verdict and sample. The cache is module state that now
 * survives without a connection fingerprint, so tests must be able to start
 * from a clean one instead of inheriting the previous test's measurement.
 */
export function resetPlaybackNetworkProbeCache(): void {
	invalidatePlaybackNetworkCache();
}

// A single 512 KB burst on a mobile link is a noisy estimator: TCP is still
// ramping, and one bad sample is enough to move the profile decision a whole
// rung — which, with a single-rendition output, means a new encoder session.
// Reporting the median of the last few samples rejects the outlier without
// lagging a genuine change the way an average would.
function smoothedDownlinkMbps(cacheKey: string, sample: number): number {
	const history = sampleHistory.get(cacheKey) ?? [];
	history.push(sample);
	while (history.length > SAMPLE_WINDOW) {
		history.shift();
	}
	sampleHistory.set(cacheKey, history);
	const sorted = [...history].sort((a, b) => a - b);
	return sorted[Math.floor(sorted.length / 2)] ?? sample;
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

		// fetch() resolves once the response HEADERS are in, so timing the body
		// read alone keeps connection setup and time-to-first-byte out of the
		// throughput figure. That mattered: on a high-RTT mobile link the old
		// wall-clock measurement divided 512 KB by (RTT + transfer), which
		// systematically under-reported the link by a factor that grew with
		// latency — exactly the conditions where the reading is used to pick a
		// profile. If the body was already buffered by then the window is too
		// small to mean anything, so fall back to the wall clock.
		const transferStartedAt = performance.now();
		const payload = await response.arrayBuffer();
		const now = performance.now();
		const transferElapsedMs = now - transferStartedAt;
		const elapsedMs = transferElapsedMs >= MIN_TRANSFER_WINDOW_MS
			? transferElapsedMs
			: now - startedAt;
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
	// A fingerprint lets us notice a network change and drop the entry, so a
	// verdict can be trusted for longer. Without one (Safari) the entry is still
	// worth reusing — re-measuring per start was the bigger error — but only for
	// a short window, since nothing but online/offline will invalidate it.
	const positiveTtlMs = networkFingerprint ? PROBE_CACHE_TTL_MS : UNKEYED_CACHE_TTL_MS;
	const cached = probeCache.get(cacheKey);
	if (cached && cached.expiresAt > now) {
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
		if (!result) {
			return result;
		}
		const value: PlaybackNetworkProbe = result.kind === 'measured'
			? { kind: 'measured', downlinkMbps: smoothedDownlinkMbps(cacheKey, result.downlinkMbps) }
			: result;
		const ttl = value.kind === 'constrained' ? CONSTRAINED_CACHE_TTL_MS : positiveTtlMs;
		probeCache.set(cacheKey, { expiresAt: Date.now() + ttl, value });
		return value;
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
): PlaybackClientContext {
	if (probe == null) {
		return context;
	}

	// A LAN verdict is the server confirming the client is a private peer on the
	// media path — the strongest positive evidence we can get, and the reason it
	// ships no payload to measure. Keeping the browser's own resource-timing
	// guess here was the bug: that heuristic divides bytes by wall time incl.
	// latency, so a fast LAN routinely estimated 15-35 Mbps, which is too slow
	// for the quality rung and too fast for the bandwidth rung — the profile
	// silently stayed unresolved. Record the verdict and drop the guess.
	if (probe.kind === 'lan') {
		capabilities.networkContext = {
			...capabilities.networkContext,
			kind: 'lan',
			downlinkKbps: undefined,
		};
		return {
			...context,
			network: {
				...context.network,
				kind: 'lan',
				downlinkMbps: undefined,
			},
		};
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
