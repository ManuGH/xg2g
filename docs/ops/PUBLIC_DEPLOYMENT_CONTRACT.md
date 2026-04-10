# Public Deployment Contract

This document defines the fail-closed public deployment contract for `xg2g`.

## Goal

`xg2g` must not "mostly work" on the public internet. Public reachability is
valid only when the declared topology, proxy trust, and published endpoint
truth are internally consistent.

## Profiles

`connectivity.profile` defines policy only. It does not introduce hidden runtime
branches or client-side heuristics.

- `lan`
  - local-first deployment
  - public HTTPS origin is not required
- `reverse_proxy`
  - public HTTPS is terminated by an explicit reverse proxy
  - `trustedProxies` is required
- `tunnel`
  - public HTTPS is terminated by a tunnel ingress
  - `trustedProxies` is required
- `vps`
  - xg2g terminates public TLS directly
  - `tls.enabled` is required

## Failure Model

- `fatal`
  - startup is rejected
- `degraded`
  - startup may continue
  - readiness and the affected runtime flow are blocked
- `warn`
  - startup and runtime continue
  - diagnostics remain noisy by design

Examples:

- missing `trustedProxies` in `reverse_proxy` or `tunnel` is `fatal`
- missing public web endpoint is `degraded` for web bootstrap
- `allowLocalHTTP` in a public profile is `warn`

## Diagnostics

Use:

- `GET /api/v3/system/connectivity`
- `make verify-public-deployment XG2G_BASE_URL=https://tv.example.net XG2G_API_TOKEN=...`

The response is the operator truth for:

- effective profile and severity
- startup/readiness/pairing/web blocking state
- selected published endpoints for web, pairing, and native clients
- trusted proxy configuration
- current request evaluation:
  - remote address
  - loopback state
  - direct TLS vs trusted `X-Forwarded-Proto=https`
  - accepted proxy headers
  - request origin allow/deny decision

## Reference Topologies

### Caddy

```caddyfile
tv.example.net {
  encode zstd gzip
  reverse_proxy 127.0.0.1:8088 {
    header_up X-Forwarded-Proto https
    header_up X-Forwarded-Host {host}
    header_up X-Forwarded-For {remote_host}
  }
}
```

Recommended xg2g config:

```yaml
trustedProxies: "127.0.0.1/32"
connectivity:
  profile: reverse_proxy
  publishedEndpoints:
    - url: "https://tv.example.net"
      kind: public_https
      priority: 10
      allowPairing: true
      allowStreaming: true
      allowWeb: true
      allowNative: true
      advertiseReason: "public reverse proxy"
```

### Nginx

```nginx
server {
  listen 443 ssl http2;
  server_name tv.example.net;

  location / {
    proxy_pass http://127.0.0.1:8088;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_http_version 1.1;
  }
}
```

### Traefik

```yaml
http:
  routers:
    xg2g:
      rule: Host(`tv.example.net`)
      entryPoints: [websecure]
      service: xg2g
      tls: {}
  services:
    xg2g:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8088"
```

Use `connectivity.profile: reverse_proxy` and set `trustedProxies` to the
Traefik ingress source CIDR that reaches xg2g.

### Cloudflare Tunnel

```yaml
tunnel: <id>
credentials-file: /etc/cloudflared/<id>.json

ingress:
  - hostname: tv.example.net
    service: http://127.0.0.1:8088
  - service: http_status:404
```

Recommended xg2g config:

```yaml
trustedProxies: "127.0.0.1/32"
connectivity:
  profile: tunnel
  publishedEndpoints:
    - url: "https://tv.example.net"
      kind: public_https
      priority: 10
      allowPairing: true
      allowStreaming: true
      allowWeb: true
      allowNative: true
      advertiseReason: "cloudflare tunnel"
```

If you also publish a LAN-native fallback, keep it explicit and supplemental:

```yaml
connectivity:
  profile: tunnel
  allowLocalHTTP: true
  publishedEndpoints:
    - url: "https://tv.example.net"
      kind: public_https
      priority: 10
      allowPairing: true
      allowStreaming: true
      allowWeb: true
      allowNative: true
      advertiseReason: "cloudflare tunnel"
    - url: "http://192.168.1.20:8088"
      kind: local_http
      priority: 20
      allowPairing: true
      allowStreaming: true
      allowNative: true
      advertiseReason: "lan native fallback"
```

## Invalid States

These are contract violations:

- `reverse_proxy` or `tunnel` without `trustedProxies`
- `vps` without direct TLS
- public profile without a `public_https` endpoint
- `trustedProxies` set to `0.0.0.0/0` or `::/0`
- local endpoints marked as web-capable in a public profile
- wildcard `allowedOrigins` in a public profile
- public web endpoint not present in `allowedOrigins`

## Verification Path

After deployment:

1. Open the public URL in a browser.
2. Run `make verify-public-deployment XG2G_BASE_URL=https://tv.example.net XG2G_API_TOKEN=...`.
3. Confirm `/healthz` is healthy and `/readyz` is ready.
4. Confirm `GET /api/v3/system/connectivity` reports `status=ok` or expected `warn`.
5. Confirm `request.effectiveHttps=true`.
6. Confirm the selected public `webPublic`, `pairingPublic`, and `nativePublic` endpoints match the operator intent.
7. Confirm no blocking findings remain for `pairing` or `web`.
8. Confirm `allowedOrigins` includes the published public web origin exactly.
