# Phase 5.3 Runbook: Soak & Load Validation (Nightly Gate)

## 0) Scope

**Validate under sustained load:**

- Admission correctness: 503 before spawn; deterministic decisions
- Preemption correctness: Recording > Live > Pulse
- Token correctness: No GPU token leaks; orphan recovery works
- Hermetic invariants: UTI/ffmpeg independent of host libs
- Observability truth: metrics/logs reflect reality

**Non-Goals:** Performance tuning, new config knobs, contract weakening.

---

## 1) Gate Strategy

### PR Gate (Fast, Every PR)

```bash
go test -race ./internal/admission/...
```

- Determinism property test (seeded) for `CanAdmit` + `SelectPreemptionTarget`
- No-spawn guarantee test must remain green
- **Hard fail**: Any flake is stop-the-line

### Nightly Soak Gate (8h+, Dedicated VAAPI Host)

- Full load matrix + chaos + hermetic probes
- Artifact capture (logs/metrics/traces)
- Automatic pass/fail scoring

---

## 2) Required Instrumentation

### Metrics (Prometheus)

| Metric | Labels |
|--------|--------|
| `xg2g_admission_admit_total` | priority |
| `xg2g_admission_reject_total` | reason, priority |
| `xg2g_preempt_total` | victim_priority, request_priority |
| `xg2g_active_sessions` | priority |
| `xg2g_gpu_tokens_in_use` | - |
| `xg2g_tuners_in_use` | - |

### Logs (Structured)

Every reject/preempt logs: `requestId`, `service_ref`, `priority`, `reason`,
snapshot: `loadavg1m`, `cores`, `gpu_tokens_in_use`, `tuners_in_use`

---

## 3) Soak Harness

**Responsibilities:**

- Generate traffic (Pulse/Live/Recording) with controlled ratios
- Maintain session lifecycles
- Inject chaos (SIGKILL UTI, container kill, tuner starvation)
- Query Prometheus, assert invariants
- Emit scoreboard + verdict

**Baseline Mix:** 60% Pulse, 35% Live, 5% Recording

---

## 4) Scenarios & Pass/Fail

### A) GPU Saturation

| Step | Action |
|------|--------|
| 1 | Drive Live until `gpu_tokens == limit` |
| 2 | Attempt +N Live for 15 min |

| Pass | Fail |
|------|------|
| All surplus → 503 | Any spawn on reject |
| Tokens never exceed limit | Token gauge > limit |
| Reject counter matches | Token never returns to baseline |

### B) Tuner Exhaustion + Preemption

| Pass | Fail |
|------|------|
| Recording admitted | Recording rejected while lower runs |
| Victims: Pulse first, then Live | Recording ever preempted |
| Victims get 410 Gone | Wrong status codes |

### C) CPU Pressure (30s Window)

| Pass | Fail |
|------|------|
| Rejects begin after ~30s | Early rejects |
| Rejects stop after recovery | Indefinite rejection |
| No flapping (≤1 transition/min) | Severe flapping |

### D) Orphan Recovery (SIGKILL)

| Pass | Fail |
|------|------|
| Tokens reclaimed in 5–30s | Token leak over time |
| Admission recovers | Tokens stuck after all stop |
| No 8h capacity loss | Deadlocks/stalls |

### E) Hermetic Probe

| Pass | Fail |
|------|------|
| UTI executes with masked /lib* | Dynamic resolution outside bundle |

---

## 5) Alert Thresholds

Fire fail-fast if:

- `gpu_tokens_in_use` doesn't return to baseline
- `admission_reject_total` spikes without load increase
- `preempt_total` increases without Recording requests
- Deadman: no admits for >N min while resources free

---

## 6) Output Artifacts

- Harness report (scenario pass/fail + counts)
- Prometheus snapshot
- Structured logs with request IDs
- Crash dumps / stack traces
- Random seeds for reproducibility

---

## 7) Closure Criteria

Phase 5.3 complete when:

- [x] 3 consecutive nightly runs pass
- [x] Zero token leaks
- [x] Zero "spawn on reject"
- [x] Priority invariants never violated
- [x] Hermetic probe passes
