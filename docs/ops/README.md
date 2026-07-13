# Operations Index

Use this area for running, deploying, verifying, and recovering xg2g in real
environments. Architecture rationale belongs in `docs/arch/`.

## Core Runbooks

| Need | Document |
| :--- | :--- |
| Deploy a pinned ref | [Deployment Guide](DEPLOYMENT.md) |
| Manage systemd + Compose | [Systemd + Compose Runbook](RUNBOOK_SYSTEMD_COMPOSE.md) |
| Verify installation layout | [Installation Contract](INSTALLATION_CONTRACT.md) |
| Validate lifecycle gates | [Operational Lifecycle Contract](OPERATIONAL_LIFECYCLE_CONTRACT.md) |
| Check service health manually | [Service Smoke](SERVICE_SMOKE.md) |
| Run smoke tests | [Smoke Test](SMOKE_TEST.md) |

## Playback And Streaming

| Need | Document |
| :--- | :--- |
| Browser/client compatibility policy | [Client Profiles](CLIENT_PROFILES.md) |
| Enigma2 stream URL resolution | [Streaming Configuration](../STREAMING_CONFIGURATION.md) |
| HLS media protocol contract | [HLS Protocol Contract](HLS_PROTOCOL_CONTRACT.md) |
| Session lifecycle behavior | [Session Lifecycle](SESSION_LIFECYCLE.md) |
| Deployment runtime and FFmpeg invariants | [Deployment Runtime Contract](DEPLOYMENT_RUNTIME_CONTRACT.md) |
| Safari/iOS manual repro | [Safari Repro Run](SAFARI_REPRO_RUN.md) |
| Safari/iOS audit notes | [Safari Audit](SAFARI_AUDIT.md) |
| Live playback attestation | [Live Playback Attestation](live-playback-attestation.md) |

## Incidents And CI

| Need | Document |
| :--- | :--- |
| CI is red | [CI Failure Playbook](CI_FAILURE_PLAYBOOK.md) |
| Decision engine result is wrong | [Decision Incident Playbook](INCIDENT_PLAYBOOK_DECISION.md) |
| Contract drift suspicion | [Contract Invariants](CONTRACT_INVARIANTS.md) |
| Generated artifacts changed | [Generated Artifact Governance](GENERATED_ARTIFACT_GOVERNANCE.md) |
| Workspace contains local output | [Workspace Cleanup](WORKSPACE_CLEANUP.md) |

## Security And Public Exposure

| Need | Document |
| :--- | :--- |
| Runtime security operations | [Security Operations](SECURITY.md) |
| Public deployment contract | [Public Deployment Contract](PUBLIC_DEPLOYMENT_CONTRACT.md) |
| Public exposure security contract | [Public Exposure Security Contract](PUBLIC_EXPOSURE_SECURITY_CONTRACT.md) |
| Backup and restore | [Backup Restore](BACKUP_RESTORE.md) |

## Runtime Policy

| Need | Document |
| :--- | :--- |
| Runtime defaults | [Runtime Policy Defaults](RUNTIME_POLICY_DEFAULTS.md) |
| Observability | [Observability](OBSERVABILITY.md) |
| Observability decision fields | [Observability Decision Contract](OBSERVABILITY_DECISION_CONTRACT.md) |
| SLOs | [SLOs](SLOS.md) |
| Preflight behavior | [Preflight](PREFLIGHT.md) |

## Maintenance Rule

If a page describes an operator action, it belongs here. If it describes why the
system is shaped a certain way, link to `docs/arch/` instead of duplicating the
rationale.
