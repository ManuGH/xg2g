# Reverse proxy & HTTPS — deployment scenarios

xg2g is shipped as a self-hosted product, so it is built to sit in front of, or
behind, whatever you already run. This page covers every common topology and the
**exact settings each one requires.**

xg2g is **secure by default**: it serves a strict set of security headers (HSTS,
CSP, COOP/CORP, X-Frame-Options, …), CSRF protection, rate-limiting and
HttpOnly+Secure+SameSite cookies natively — you do **not** need a reverse proxy
to add them.

> **Use HTTPS for any access beyond `localhost`.** Public deployment profiles
> (`reverse_proxy`, `tunnel`, `vps`) **require** TLS and will refuse to start
> otherwise (see the contract below), and authenticated media + the playback
> session cookie should never traverse cleartext. `http://localhost` is a
> browser *secure context* and is the supported plaintext path for local dev;
> plain HTTP to a LAN IP/hostname is not a supported way to run the web player.

---

## The setting that bites everyone: `XG2G_TRUSTED_PROXIES`

When xg2g runs behind a proxy, the proxy's IP is what xg2g sees as the client.
xg2g will **only** trust `X-Forwarded-For` / `X-Forwarded-Proto` /
`X-Forwarded-Host` (used for HTTPS detection, Secure cookies, the real client
IP, rate-limiting and the LAN guard) if the **direct** connection comes from an
IP listed in `XG2G_TRUSTED_PROXIES`.

> Behind a proxy without `XG2G_TRUSTED_PROXIES` set, those headers are **silently
> ignored**: HSTS missing over HTTPS, `Secure` cookies not set, every client
> logged as the proxy, rate-limit/LAN-guard keyed on the proxy. Nothing errors.

It is a comma-separated CIDR list, **empty by default** (safe: nothing trusted).
Set it to where the proxy connects *from*:

| Proxy location relative to xg2g | Value to use |
| --- | --- |
| Same host (proxy → 127.0.0.1) | `127.0.0.1/32,::1/128` |
| Same Docker network | that network's CIDR, e.g. `172.18.0.0/16` |
| Separate host on the LAN | the proxy host's IP, e.g. `10.10.55.2/32` |

---

## Deployment profile: `XG2G_CONNECTIVITY_PROFILE`

The connectivity contract validates your topology at startup and **fails fast**
on an insecure public setup. Pick the profile that matches reality:

| Profile (`XG2G_CONNECTIVITY_PROFILE`) | Public? | Hard requirement (fatal at startup if unmet) |
| --- | --- | --- |
| `lan` (default) | no | none |
| `reverse_proxy` | yes | `XG2G_TRUSTED_PROXIES` must be set |
| `tunnel` | yes | `XG2G_TRUSTED_PROXIES` must be set |
| `vps` | yes | `XG2G_TLS_ENABLED=true` (direct TLS) |

`XG2G_CONNECTIVITY_ALLOW_LOCAL_HTTP` (default `false`) is a deliberate,
**native-client-only** escape hatch for a supplemental plain-HTTP path; it does
**not** turn the browser web player into a plain-HTTP experience.

---

## Scenario matrix

| # | Topology | TLS by | `XG2G_CONNECTIVITY_PROFILE` | `XG2G_TRUSTED_PROXIES` | `XG2G_TLS_ENABLED` |
| --- | --- | --- | --- | --- | --- |
| 1 | **Local dev** (`http://localhost`) | none (secure context) | `lan` | empty | `false` |
| 2 | **Direct, public TLS** | xg2g itself | `vps` | empty | `true` (+ cert/key) |
| 3 | **Behind Caddy** | Caddy (auto) | `reverse_proxy` | proxy IP/CIDR | `false` |
| 4 | **Behind nginx** | nginx | `reverse_proxy` | proxy IP/CIDR | `false` |
| 5 | **Behind Traefik** | Traefik (ACME) | `reverse_proxy` | Docker net CIDR | `false` |
| 6 | **Cloudflare Tunnel** | Cloudflare edge | `tunnel` | `cloudflared` source IP | `false` |

For scenarios 2–6, also set `XG2G_ALLOWED_ORIGINS=https://your.domain` so CORS
and CSRF accept the public origin.

---

## Reference configs

Drop-in, commented starting points (replace `tv.example.com` and the upstream
host:port — `8088` prod / `8089` staging):

- **Caddy** — `reverse-proxy/caddy/Caddyfile` (automatic TLS, least effort)
- **nginx** — `reverse-proxy/nginx/xg2g.conf`
- **Traefik** — `reverse-proxy/traefik/docker-compose.traefik.yml`

Each file carries the required `XG2G_TRUSTED_PROXIES` and
`XG2G_CONNECTIVITY_PROFILE` values for that topology in a header comment.

---

## HTTP → HTTPS redirect: it belongs at the edge

xg2g listens on a **single scheme** — either TLS *or* plain HTTP, never both —
so it cannot redirect HTTP to HTTPS itself. The redirect is the proxy's job, and
all three reference configs do it (Caddy automatically, nginx via a `:80`
`return 308`, Traefik via `redirectscheme`).

`XG2G_FORCE_HTTPS` is an **HTTPS-posture flag** surfaced in the connectivity
contract; it does not perform an app-level redirect. For repeat visitors, the
`Strict-Transport-Security` (HSTS) header xg2g sends over HTTPS makes the browser
upgrade subsequent requests on its own.

---

## Streaming notes (HLS)

- xg2g **compresses its own responses** (text/JSON/JS/CSS + HLS playlists) and
  never compresses media segments. Do **not** enable gzip/`encode` at the proxy
  — it would double-compress. Proxies pass `Content-Encoding` through fine.
- Media segments are served as plain files with `Range` support; default proxy
  settings work. The nginx sample sets `proxy_buffering off` for lower latency.
- Match the proxy's max body size to xg2g's **4 MiB** request ceiling (the
  samples use `8m`/`8MB` headroom) so the app returns its clean RFC-7807 `413`.
- Media requests authenticate with the **session cookie** (not a bearer token),
  so the browser origin and the playback origin must match — another reason to
  serve the UI and the API from the same HTTPS origin.
- No WebSocket/SSE endpoints — no `Upgrade` handling needed.
