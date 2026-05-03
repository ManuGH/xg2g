# Development Index

Use this area for contributor orientation and local development workflow.

## Start Here

| Need | Document |
| :--- | :--- |
| Fast repo orientation | [Repository Map](REPO_MAP.md) |
| Local setup and pinned tools | [Developer Setup](SETUP.md) |
| End-to-end workflow | [Development Guide](../guides/DEVELOPMENT.md) |
| CI failure workflow | [CI Failure Playbook](../ops/CI_FAILURE_PLAYBOOK.md) |

## Common Paths

| Task | Document |
| :--- | :--- |
| Backend package ownership | [Package Layout](../arch/PACKAGE_LAYOUT.md) |
| Config surface changes | [Config Surfaces](../guides/CONFIG_SURFACES.md) |
| Generated artifacts | [Generated Artifact Governance](../ops/GENERATED_ARTIFACT_GOVERNANCE.md) |
| Workspace cleanup | [Workspace Cleanup](../ops/WORKSPACE_CLEANUP.md) |
| Log hygiene | [Log Hygiene Ticket](LOG_HYGIENE_TICKET.md) |

## Minimum Local Proof

Pick the narrowest proof that covers your change, then run the broader gate
before PR:

```bash
make doctor
make ci-pr
git diff --check
```

For frontend-only work, use `npm --prefix frontend/webui test` and
`npm --prefix frontend/webui run lint` before the broader gate.
