# Design: Operational Degradation Contract v1

## 1. Objective

To define a deterministic and contractual behavior for the `xg2g` service when hardware resources (CPU/GPU/Tuner) are over-subscribed or failing.

## 2. Invariants (Cannot Degrade)

- **API Correctness**: All `api/v3` endpoints MUST return schema-valid responses, even under total resource exhaustion. Error responses (e.g., 503) MUST still be valid JSON.
- **State Integrity**: A failure in a transcoding session MUST NOT corrupt the global session store or recording schedule.
- **Auth Enforcement**: Performance pressure MUST NOT lead to "fail-open" authentication.
- **Health Reporting**: The service MUST truthfully report its internal congestion through the `/metrics` endpoint (Prometheus).

## 3. Elastic Boundaries (May Degrade)

The system is allowed to negotiate the following parameters downward during resource contention:

- **Resolution**: High-definition streams (1080p/4K) may be stepped down to 720p or 576p.
- **Framerate**: 50/60fps streams may be halved to 25/30fps to reduce GPU load.
- **Codec Complexity**: Profile/Level may be reduced (e.g., H.264 High to Main) to simplify encoding.
- **Time-to-Ready**: Session initiation latency is allowed to increase while waiting for a hardware lock.

## 4. Withdrawal Policy (Must Hard-Fail)

The system MUST reject new admission requests (HTTP 503) instead of "trying its best" under the following conditions. Admission rejection MUST occur before spawning a UTI process.

- **Tuner Exhaustion**: No physical tuners available and no "low-priority" stream can be preempted.
- **IO Saturation**: Recording disk write latency exceeds 1500ms (to prevent buffer overflows).
- **GPU Quota Met**: Maximum concurrent hardware sessions reached and software fallback is disabled or saturated.
- **CPU Starvation**: System load average (1m) exceeds `num_cores * 1.5` for more than 30 seconds.

## 5. Contractual Priority

1. **Active Recordings**: Highest priority. These will NEVER be degraded or preempted by live viewers.
2. **Live "Standard" Viewers**: Default priority. Subject to degradation before preemption.
3. **EPG/Metadata Sync**: Low priority. Background tasks will be auto-throttled to yield CPU to transcoding.
