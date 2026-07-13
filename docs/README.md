# xg2g Documentation

Start with the public guides and operator runbooks. The remaining subtrees are
technical reference material for maintainers and contributors.

## Run xg2g — User

You want to stream your Enigma2 receiver in a browser.

| Step | Document |
| :--- | :--- |
| Get streaming, step by step | [Getting Started](guides/GETTING_STARTED.md) |
| Install in one command | [Quickstart](../README.md#quickstart) |
| Set the essential options | [Configuration → Essential](guides/CONFIGURATION.md#essential-start-here) |
| Full configuration reference | [Configuration Guide](guides/CONFIGURATION.md) |
| Understand playback & codecs | [Codec Matrix](arch/CODEC_MATRIX.md) |
| Something not working? | [Troubleshooting](guides/TROUBLESHOOTING.md) |
| Published API reference | [API Docs](https://manugh.github.io/xg2g/) |

## Operate xg2g — Operator

You run xg2g as a service for others.

| Task | Document |
| :--- | :--- |
| Deploy (systemd / Compose) | [Deployment Guide](ops/DEPLOYMENT.md) |
| Security operations | [Security](ops/SECURITY.md) |
| Browser / client playback policy | [Client Profiles](ops/CLIENT_PROFILES.md) |
| Runbooks & incident response | [Operations Index](ops/README.md) |
| Decision-engine incidents | [Decision Incident Playbook](ops/INCIDENT_PLAYBOOK_DECISION.md) |

## All Documentation Areas

| Area | Audience | Purpose |
| :--- | :--- | :--- |
| [guides/](guides/README.md) | User | Setup, configuration, and reference material. |
| [ops/](ops/README.md) | Operator | Deployment, runtime contracts, runbooks, incident response, security ops. |
| [arch/](arch/README.md) | Reference | System design, package ownership, playback decision rules, codec behavior. |

## Maintenance Rules

- Keep category README files as navigation only. Put detailed behavior in the
  linked contract, runbook, or architecture document.
- Update [docs/ops/CLIENT_PROFILES.md](ops/CLIENT_PROFILES.md) when browser
  family or runtime-probe behavior changes.
- Update [docs/arch/CODEC_MATRIX.md](arch/CODEC_MATRIX.md) when codec,
  container, or transcode-target behavior changes.
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
