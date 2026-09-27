# How-To Guides (Diátaxis: Problem-Oriented)

How-to guides provide direct, actionable recipes to solve specific real-world operational problems with **xg2g**.

---

## What is a How-To Guide?

According to the [Diátaxis framework](https://diataxis.fr/how-to-guides/), a how-to guide is:
- **Problem-oriented**: Directly answers the question "How do I do X?".
- **Goal-focused**: Assumes basic competence; provides an efficient recipe without tutorial hand-holding.
- **Practical & outcome-driven**: Focuses on operational reliability, maintenance, and diagnostics.

---

## Available How-To Guides

### Installation & Deployment

| Guide | Description |
| :--- | :--- |
| [**Linux Host Installation**](../guides/INSTALLATION.md) | Run `setup-linux.sh`, configure systemd units, set up user groups and permissions. |
| [**Production Deployment**](../ops/DEPLOYMENT.md) | Docker Compose orchestration, container limits, storage mounts, and auto-restart policies. |
| [**Security Hardening**](../ops/SECURITY.md) | Reverse proxy configuration (Caddy/Nginx), TLS certificates, token authentication, and firewall rules. |

### Operations, Maintenance & Triage

| Guide | Description |
| :--- | :--- |
| [**Maintainer Incident Triage & Operations Runbook**](../ops/RUNBOOK_SYSTEMD_COMPOSE.md) | Standard triage sequence for receiver lockups (`dvb.cpp ASSERTION cnt == 1`), tuner starvation, and streaming stalls. |
| [**Troubleshooting & Diagnostics**](../guides/TROUBLESHOOTING.md) | Running `xg2g-admin doctor`, inspecting logs, validating tuner availability, and analyzing error states. |

---

## Where to Go Next

- New to xg2g? Start with a [**Tutorial**](../tutorials/README.md).
- Looking up specific config keys or reason codes? Consult [**Reference**](../reference/README.md).
- Seeking deep architectural rationale or concurrency models? Explore [**Explanation**](../explanation/README.md).
