# xg2g Documentation

This is the documentation entrypoint. Use it as the router, not as a dumping
ground for implementation detail.

## Read First

| Need | Start Here |
| :--- | :--- |
| Run xg2g for the first time | [Product README](../README.md) |
| Understand the repository | [Repository Map](dev/REPO_MAP.md) |
| Set up a dev machine | [Developer Setup](dev/SETUP.md) |
| Deploy or operate the service | [Operations Index](ops/README.md) |
| Understand architecture and ownership | [Architecture Index](arch/README.md) |
| Work on the WebUI | [WebUI Index](webui/README.md) |
| Prepare a release | [Release Index](release/README.md) |

## Documentation Areas

| Area | Purpose |
| :--- | :--- |
| [guides/](guides/README.md) | User-facing setup, configuration, and reference material. |
| [ops/](ops/README.md) | Deployment, runtime contracts, runbooks, incident response, and security operations. |
| [arch/](arch/README.md) | System design, package ownership, playback decision rules, codec/container behavior. |
| [dev/](dev/README.md) | Contributor setup, repo map, local workflow, workspace hygiene. |
| [webui/](webui/README.md) | UI contracts, telemetry, error mapping, frontend/backend consumption rules. |
| [ADR/](ADR/README.md) | Accepted architecture decisions and historical context. |
| [decision/](decision/SPEC_INDEX.md) | Normative decision-engine specs. |
| [release/](release/README.md) | Release templates, release notes, and GitHub copy. |

## Operational Shortcuts

| Task | Document |
| :--- | :--- |
| Configure xg2g | [Configuration Guide](guides/CONFIGURATION.md) |
| Deploy with systemd/Compose | [Deployment Guide](ops/DEPLOYMENT.md) |
| Debug failed CI | [CI Failure Playbook](ops/CI_FAILURE_PLAYBOOK.md) |
| Debug playback decision behavior | [Decision Incident Playbook](ops/INCIDENT_PLAYBOOK_DECISION.md) |
| Check browser/client playback policy | [Client Profiles](ops/CLIENT_PROFILES.md) |
| Check codec/container rules | [Codec Matrix](arch/CODEC_MATRIX.md) |
| Operate Safari/iOS repro checks | [Safari Repro Run](ops/SAFARI_REPRO_RUN.md) |
| Review OpenAPI/API docs | [Published API Docs](https://manugh.github.io/xg2g/) |

## Maintenance Rules

- Keep category README files as navigation only. Put detailed behavior in the
  linked contract, runbook, or architecture document.
- Update [docs/ops/CLIENT_PROFILES.md](ops/CLIENT_PROFILES.md) when browser
  family or runtime-probe behavior changes.
- Update [docs/arch/CODEC_MATRIX.md](arch/CODEC_MATRIX.md) when codec,
  container, or transcode-target behavior changes.
- Update [docs/dev/REPO_MAP.md](dev/REPO_MAP.md) when source ownership,
  generated artifacts, or required gates change.
- The root [README.md](../README.md) is generated from
  [backend/templates/README.md.tmpl](../backend/templates/README.md.tmpl); edit
  the template as the source of truth.

## Drift Checks

Before opening a PR that changes normative docs, run the smallest relevant
verification gate plus:

```bash
git diff --check
```

If generated artifacts changed, run the matching generator/verification target
before committing.
