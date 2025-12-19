# Security Invariants

**Version:** 1.0.0
**Status:** PROVISIONAL
**Last Updated:** 2025-12-19

## 1. Purpose and Scope

This document defines the **normative security guarantees** (invariants) of the `xg2g` application. It serves as a contract for operators, contributors, and dependent systems. All future changes to the codebase **MUST** verify that these invariants are preserved.

This is **NOT** a configuration guide or a runbook. For operational instructions, see `guides/CONFIGURATION.md`.

## 2. Threat Model (Brief)

### Trust Boundaries

* **Trusted:**
  * **Operator:** Controls the configuration file (`config.yaml`), environment variables at process start, and the filesystem within `DataDir`.
  * **Local Filesystem:** The process assumes it can safely read/write to its configured `DataDir`.
  * **Receiver (OpenWebIF):** Trusted to provide valid streams and EPG data, though the application must handle its potential unavailability or malformed responses gracefully.
* **Untrusted:**
  * **Clients:** Any HTTP request arriving at the API port (default 8000) or Proxy port (default 18000) is potentially malicious.
  * **Network:** The local network is generally assumed to be low-trust (e.g., IoT devices, guest wifi). `xg2g` binds to specific interfaces to limit exposure.

### Non-Goals

* This document does not guarantee network-layer security (e.g., DDOS protection, TLS termination).
* This document does not guarantee correctness of the upstream receiver (OpenWebIF).

### Assumptions

* The operator is responsible for securing the host environment (OS, firewall).
* `xg2g` is not a WAF; it provides application-level security controls but relies on the operator for network-level defense.

## 3. Invariants

**Index:**

* [3.1. Config Determinism](#31-config-determinism)
* [3.2. File Serving Security](#32-file-serving-security)
* [3.3. Proxy Security](#33-proxy-security)
* [3.4. Process Lifecycle](#34-process-lifecycle)

The following invariants **MUST** hold true for any release of `xg2g`.

### 3.1. Config Determinism

**Behavior:**

* The application configuration is immutable during the lifetime of a "configuration epoch".
* Environment variables are read **ONLY** during initial startup or explicit reload events.

**Rationale:**
Prevents "config drift" where the running state diverges from the known configuration source, and eliminates race conditions from hot-loop environment access.

**Enforcement:**

* **Code:** `internal/config` is the sole package authorized to read process environment variables (e.g., `os.LookupEnv` / `os.Getenv`).
* **Code:** Handlers access config via `config.Snapshot` or `config.AppConfig`, never potentially mutating globals or system env.
* **Tripwire:** CI Lint check (Planned) preventing `os.LookupEnv` outside `internal/config`.

### 3.2. File Serving Security

**Behavior:**

* **Allowlist Only:** The `/files/*` endpoint serves **ONLY** specific, harmless filenames (e.g., `playlist.m3u`, `xmltv.xml`). Arbitrary file access is denied.
* **Path Confinement:** Even for allowlisted filenames, resolved paths **MUST** remain within the configured `DataDir` after canonicalization (no symlink or traversal escape).
* **Denylist:** Known sensitive files (e.g., `config.yaml`, `.env`) are explicitly blocked as a defense-in-depth measure.

**Rationale:**
Prevents Directory Traversal and Information Disclosure vulnerabilities, ensuring the application cannot be used to exfiltrate system secrets.

**Enforcement:**

* **Code:** `internal/api/fileserver.go` implementation of `secureFileServer`.
* **Tests:** `TestSecureFileServer_AllowlistDenylist` and `TestSecureFileServer_PathTraversal` in `internal/api/fileserver_test.go`.

### 3.3. Proxy Security

**Behavior:**

* **Local Bind:** The stream proxy binds to `127.0.0.1` by default. Binding to `0.0.0.0` requires explicit operator configuration.
* **Explicit Auth:** Access to the proxy requires valid authentication (Token or Cookie), identical to the API, unless `AuthAnonymous` is explicitly enabled by the operator.
* **Upstream Validation:** The proxy forwards traffic **ONLY** to the configured `OpenWebIF` upstream.
  * Request URLs are rewritten to target the configured `OWIBase`.
* `file://` scheme in upstream targets is strictly forbidden.

**Rationale:**
Prevents Server-Side Request Forgery (SSRF) and Open Proxy abuse. Ensures that the proxy cannot be used to attack the local network or internal services.

**Enforcement:**

* **Code:** `internal/proxy` package.
* **Tests:** `TestProxy_SSRF_Protection` (or equivalent integration tests).

### 3.4. Process Lifecycle

**Behavior:**

* **No Zombies:** Child processes (e.g., FFmpeg) are **ALWAYS** waited for.
* **Cleanup:** Child processes are terminated (SIGKILL/SIGTERM) when the parent request context is cancelled or the stream ends.
* **Panic Safety:** The process reaper must defensively handle panics within the streaming lifecycle to ensure cleanup occurs during recoverable application faults.
* **Job Cancellation:** Background jobs (e.g., Refresh, Picon Pre-warm) respect context cancellation; terminating the parent job explicitly terminates all child goroutines.

**Rationale:**
Prevents resource exhaustion (process table leaks, open file descriptors) and ensures system stability over long runtimes.

**Enforcement:**

* **Code:** `internal/proxy/transcoder.go` (reaping logic).
* **Code:** `internal/jobs` (context propagation).
* **Tests:** `transcoder_test.go`, `TestContextCancellation`.

## 4. Breaking-Change Policy

Any change that modifies or relaxes an invariant defined in Section 3 is considered a **CRITICAL BREAKING CHANGE**.

**Requirements:**

1. **Changelog Entry:** Must be highlighted in `CHANGELOG.md` under a "Security" header.
2. **Documentation Update:** This document (`SECURITY_INVARIANTS.md`) must be updated to reflect the new state.
3. **Regression Tests:** New tests must be added to verify the new behavior and ensure the "relaxed" invariant is still bounded and safe.

## 5. Known Accepted Risks

* **Receiver-Side Auth:** `xg2g` can pass Basic Auth credentials to OpenWebIF, but it does not validate them itself. It relies on the receiver to reject invalid upstream credentials.
* **LAN Exposure:** If the operator binds the API or Proxy to `0.0.0.0`, the application is accessible to the local network. `xg2g` relies on proper Token authentication being enabled by the operator to secure this access.
* **Plaintext HTTP:** By default, `xg2g` speaks HTTP. HTTPS requires a reverse proxy (Nginx/Caddy) or trusted local network.

## 6. Planned Invariants (P2 Roadmap)

* **Metric Label Cardinality:** Rate limits must not explode metric label cardinality (e.g., tracking every unique malicious remote IP).
