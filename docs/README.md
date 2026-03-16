# xg2g Documentation Portal

[![Coverage](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml)

xg2g documentation is split by audience so new users, operators, and
contributors can reach the right surface quickly.

## Start Here

- Product overview: [README.md](../README.md)
- API docs: [manugh.github.io/xg2g](https://manugh.github.io/xg2g/)
- Design source of truth: [decision/SPEC_INDEX.md](decision/SPEC_INDEX.md)

## Evaluate xg2g

- [Architecture Overview](arch/ARCHITECTURE.md)
- [Playback topology](arch/ENIGMA2_STREAMING_TOPOLOGY.md)
- [Product policy](PRODUCT_POLICY.md)
- [ADR index](ADR/README.md)

## Operate xg2g

- [Configuration Guide](guides/CONFIGURATION.md)
- [Deployment Guide](ops/DEPLOYMENT.md)
- [Observability](ops/OBSERVABILITY.md)
- [Security Policy](../SECURITY.md)
- [Systemd + Compose runbook](ops/RUNBOOK_SYSTEMD_COMPOSE.md)

## Build and Contribute

- [Development Guide](guides/DEVELOPMENT.md)
- [Developer Setup](dev/SETUP.md)
- [Contributing](../CONTRIBUTING.md)
- [Release template](release/RELEASE_TEMPLATE.md)
- [GitHub presence copy](release/GITHUB_PRESENCE_COPY.md)
- [CI Failure Playbook](ops/CI_FAILURE_PLAYBOOK.md)

## Structure

- [ADR/](ADR/) explains why major decisions were made.
- [ops/](ops/) contains runbooks, deployment contracts, and incident guidance.
- [arch/](arch/) covers system design and service boundaries.
- [dev/](dev/) documents local setup and contributor workflows.

## Governance

Changes to normative documentation must stay in sync with
[decision/SPEC_INDEX.md](decision/SPEC_INDEX.md) and the
[PRODUCT_POLICY.md](PRODUCT_POLICY.md). CI enforces required checks on pull
requests to reduce documentation drift.
