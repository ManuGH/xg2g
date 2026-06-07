# Reverse proxy & HTTPS — deployment scenarios

xg2g is shipped as a self-hosted product, so it is built to sit in front of, or
behind, whatever you already run. This page covers every common topology and the
**exact settings each one requires.**

xg2g is **secure by default**: it serves a strict set of security headers (HSTS,
CSP, COOP/CORP, X-Frame-Options, …), CSRF protection, rate-limiting and
HttpOnly+Secure+SameSite cookies natively — you do **not** need a reverse proxy
to add them. A proxy is only needed for public TLS / a domain name.

---

## The one rule that bites everyone: `XG2G_TRUSTED_PROXIES`

When xg2g runs behind a proxy, the proxy's IP is what xg2g sees as the client.
xg2g will **only** trust the `X-Forwarded-For` / `X-Forwarded-Proto` /
`X-Forwarded-Host` headers (used for HTTPS detection, Secure cookies, the real
client IP, rate-limiting and the LAN guard) if the **direct** connection comes
from an IP listed in `XG2G_TRUSTED_PROXIES`.

> If you put a proxy in front and **don't** set `XG2G_TRUSTED_PROXIES`, those
> forwarded headers are **silently ignored**. Symptoms: HSTS missing over HTTPS,
> `Secure` cookies not set, every client logged as the proxy IP, rate-limit and
> LAN guard keyed on the proxy. Nothing errors — it just quietly misbehaves.

`XG2G_TRUSTED_PROXIES` is a comma-separated CIDR list and is **empty by default**
(safe: no forwarded headers trusted). Set it to where the proxy connects *from*:

| Proxy location relative to xg2g | Value to use |
| --- | --- |
| Same host (proxy → 127.0.0.1) | `127.0.0.1/32,::1/128` |
| Same Docker network | that network's CIDR, e.g. `172.18.0.0/16` |
| Separate host on the LAN | the proxy host's IP, e.g. `10.10.55.2/32` |

---

## Scenario matrix

| # | Topology | TLS by | `XG2G_TRUSTED_PROXIES` | `XG2G_TLS_ENABLED` | Notes |
| --- | --- | --- | --- | --- | --- |
| 1 | **Direct, LAN-only (default)** | none (HTTP) | empty | `false` | `http://host:8088`. Fine on a trusted LAN. |
| 2 | **Direct, in-process TLS** | xg2g itself | empty | `true` (+ cert/key) | Hardened TLS 1.2+ AEAD handshake out of the box (#548). No proxy. |
| 3 | **Behind Caddy** | Caddy (auto) | proxy IP/CIDR | `false` | Easiest public HTTPS. See `reverse-proxy/caddy/`. |
| 4 | **Behind nginx** | nginx | proxy IP/CIDR | `false` | See `reverse-proxy/nginx/`. |
| 5 | **Behind Traefik** | Traefik (ACME) | Docker net CIDR | `false` | See `reverse-proxy/traefik/`. |
| 6 | **Cloudflare Tunnel** | Cloudflare edge | `cloudflared` source IP | `false` | `cloudflared` sets `X-Forwarded-Proto: https`; trust its source. |

For scenarios 3–6, also set `XG2G_ALLOWED_ORIGINS=https://your.domain` so CORS
and CSRF accept the public origin.

---

## Reference configs

Drop-in, commented starting points (replace `tv.example.com` and the upstream
host:port — `8088` prod / `8089` staging):

- **Caddy** — `reverse-proxy/caddy/Caddyfile` (automatic TLS, least effort)
- **nginx** — `reverse-proxy/nginx/xg2g.conf`
- **Traefik** — `reverse-proxy/traefik/docker-compose.traefik.yml`

Each file carries the required `XG2G_TRUSTED_PROXIES` value for that topology in
a header comment.

---

## HTTP → HTTPS redirect: it belongs at the edge

xg2g listens on a **single scheme** — either TLS *or* plain HTTP, never both —
so it cannot redirect HTTP to HTTPS itself. The redirect is the proxy's job, and
all three reference configs do it (Caddy automatically, nginx via a `:80`
`return 308`, Traefik via `redirectscheme`).

`XG2G_FORCE_HTTPS` is an **HTTPS-posture flag** surfaced in the connectivity
contract (it advertises "this deployment expects HTTPS"); it does not perform an
app-level redirect. For repeat visitors, the `Strict-Transport-Security` (HSTS)
header xg2g already sends over HTTPS makes the browser upgrade subsequent
requests on its own.

---

## Streaming notes (HLS)

- xg2g **compresses its own responses** (text/JSON/JS/CSS + HLS playlists) and
  never compresses media segments. Do **not** enable gzip/`encode` at the proxy
  — it would double-compress. Proxies pass `Content-Encoding` through fine.
- Media segments are served as plain files with `Range` support; the default
  proxy settings work. The nginx sample sets `proxy_buffering off` to stream
  bytes through with lower latency.
- Match the proxy's max body size to xg2g's **4 MiB** request ceiling (the
  samples use `8m`/`8MB` headroom) so the app returns its clean RFC-7807 `413`.
- No WebSocket/SSE endpoints — no `Upgrade` handling needed.
