---
name: "🚀 Focused Tech Review"
title: "Tech Review: [Component or Feature]"
labels: "tech-debt, security, enhancement"
assignees: ''
---

### 🎯 **Review Focus**

A clear, concise description of the component or feature under review. What is its purpose? What are the primary goals of this review (e.g., pre-production hardening, security audit, performance analysis)?

---

### ✅ **Review Checklist**

This checklist ensures all critical aspects of security, robustness, and CI/CD are covered.

#### 🛡️ **Security**

- [ ] **Security Headers**: Are appropriate security headers (`Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `HSTS`) applied globally and correctly?
- [ ] **Path Traversal**: Is the file server properly hardened against path traversal attacks (`/files/../../etc/passwd`)? Are symlinks handled safely?
- [ ] **Authentication/Authorization**: Are sensitive endpoints protected? Is the mechanism "fail-closed"? Are credentials compared in constant time?
- [ ] **Input Validation**: Is all user-provided input (headers, query parameters, body) properly validated and sanitized?
- [ ] **Error Handling**: Are internal error details prevented from leaking to the client?

#### 🌐 **Robustness & Resilience**

- [ ] **HTTP Transport Timeouts**: Does the outbound HTTP client (`openwebif`) have appropriate, non-default timeouts set (`DialContext`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`)?
- [ ] **Server Timeouts**: Does the inbound HTTP server have comprehensive timeouts set (`ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `ReadHeaderTimeout`)?
- [ ] **Graceful Shutdown**: Does the application handle `SIGINT`/`SIGTERM` signals to shut down gracefully, finishing in-flight requests?
- [ ] **Retries & Backoff**: Are transient network errors handled with a proper retry mechanism using exponential backoff?
- [ ] **Resource Management**: Are resources like file handles and network connections properly closed (e.g., `defer file.Close()`)?

#### CI/CD & Build Pipeline

- [ ] **Reproducible Builds**: Does the build process (`Makefile`, `Dockerfile`) produce bit-for-bit identical binaries from the same source code (`-trimpath`, `-buildid=`)?
- [ ] **Automated Scans**: Is the CI pipeline configured to run static analysis (`golangci-lint`), vulnerability scans (`govulncheck`), and container scans (`docker scout` or `grype`)?
- [ ] **Versioning**: Is the application version correctly injected into the binary at build time and exposed via an endpoint or flag?

#### 🐳 **Containerization (Docker)**

- [ ] **Health Checks**: Does the `Dockerfile` include a `HEALTHCHECK` instruction that accurately reflects the application's ready state (e.g., calling `/readyz`)?
- [ ] **Resource Limits**: Is the `docker-compose.yml` or Kubernetes deployment configured with sensible CPU and memory limits to prevent resource exhaustion?
- [ ] **Non-Root User**: Does the container run as a non-root user to minimize potential damage upon a compromise?
- [ ] **Multi-Stage Builds**: Is a multi-stage build used to create a minimal production image, excluding build tools and source code?

---

### 📝 **Findings & Recommendations**

A summary of any issues found during the review, with concrete, actionable recommendations for remediation.

**Example:**

- **Finding**: The OpenWebIF client uses `http.DefaultClient`, which has no timeouts.
- **Recommendation**: Create a dedicated `http.Client` with a custom transport, setting `DialContext`, `TLSHandshakeTimeout`, and `ResponseHeaderTimeout` to prevent indefinite hangs.
