# RBAC Scopes (v3 API)

## Overview

v3 endpoints require scopes in addition to a valid API token. Each route declares required scopes at registration time. Tokens without explicit scopes default to **read-only** access for v3.

## Scopes

| Scope | Meaning |
| :--- | :--- |
| `v3:read` | Read-only access (GET endpoints). |
| `v3:write` | Write/control access (POST intents). Implies `v3:read`. |
| `v3:admin` | Reserved for future admin operations. Implies `v3:write` + `v3:read`. |

## Configuration

### Environment Variables

- `XG2G_API_TOKEN_SCOPES`: Comma-separated scopes for `XG2G_API_TOKEN`.
  - Example: `XG2G_API_TOKEN_SCOPES=v3:read,v3:write`
- `XG2G_API_TOKENS`: Additional tokens with scopes.
  - Format: `token=scopes;token2=scopes`
  - Example: `XG2G_API_TOKENS=tokenA=v3:read;tokenB=v3:read,v3:write`

If `XG2G_AUTH_ANONYMOUS=true` and **no tokens** are configured, v3 access is read-only.

### config.yaml

```yaml
api:
  token: "primary-token"
  tokenScopes:
    - v3:read
    - v3:write
  tokens:
    - token: "read-only-token"
      scopes: ["v3:read"]
```

## Endpoint → Scope Mapping

| Endpoint | Method | Required Scope |
| :--- | :--- | :--- |
| `/api/v3/intents` | `POST` | `v3:write` |
| `/api/v3/sessions` | `GET` | `v3:read` |
| `/api/v3/sessions/{sessionID}` | `GET` | `v3:read` |
| `/api/v3/sessions/{sessionID}/hls/{filename}` | `GET` | `v3:read` |
