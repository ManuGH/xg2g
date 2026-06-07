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

## New here? Quick start (a domain + Caddy)

The shortest path to a working, secure deployment. Caddy gives you automatic
HTTPS in a few lines and handles the redirect and forwarded headers for you.

1. Point a DNS name at the host (e.g. `tv.example.com` → your server). No public
   domain? See **"No public domain? (homelab)"** below.
2. Run xg2g normally — plain HTTP is fine, Caddy adds the TLS. (Don't set
   `XG2G_TLS_ENABLED`.)
3. Drop in the reference `reverse-proxy/caddy/Caddyfile`, replacing
   `tv.example.com` and the upstream `host:port`.
4. Set three env vars on xg2g so it trusts Caddy and accepts your URL:

   ```sh
   XG2G_CONNECTIVITY_PROFILE=reverse_proxy
   XG2G_TRUSTED_PROXIES=<ip-or-cidr-Caddy-connects-from>   # same host → 127.0.0.1/32
   XG2G_ALLOWED_ORIGINS=https://tv.example.com             # the exact URL you open
   ```

5. Start both and open `https://tv.example.com`. HTTPS, the HTTP→HTTPS redirect,
   Secure cookies and the web player all work.

A copy-paste Caddy + xg2g stack (with the Docker-network `TRUSTED_PROXIES`
already resolved) is at `reverse-proxy/caddy/docker-compose.caddy.yml`.

### Why the web player needs HTTPS

This is the single thing newcomers trip over — **the streams won't start over
plain HTTP on a LAN IP.** Three reasons, all by design:

- Browsers restrict parts of the web platform to a **secure context** (HTTPS, or
  `http://localhost` only) — a LAN IP over HTTP is *not* one.
- xg2g serves authenticated media and a playback **session cookie**; these should
  never cross the network in cleartext.
- Public deployment profiles **enforce** TLS at startup (see the profile table).

So pick an HTTPS path from the matrix below. `http://localhost` works for local
dev; everything else needs a proxy (or xg2g's own TLS).

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

## No public domain? (homelab / internal only)

You still get HTTPS without a public name — common for LAN + VPN (e.g.
WireGuard) setups:

- **Internal domain you control** (e.g. `tv.home.lan`): use a Caddy
  DNS-challenge plugin for your DNS provider to get a real, trusted cert, and
  resolve the name on your LAN via split-DNS (router / Pi-hole / OPNsense
  Unbound). Reach it the same way over the VPN.
- **No domain at all:** Caddy's internal CA (`tls internal`) issues a local cert
  — install Caddy's root on your devices once and they trust it. Or enable
  xg2g's own in-process TLS (`XG2G_TLS_ENABLED=true`), which self-signs for the
  host's IPs (expect a one-time browser warning).

Either way, set `XG2G_ALLOWED_ORIGINS` to the exact `https://…` URL you open,
and `XG2G_TRUSTED_PROXIES` to the proxy when one is in front.

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

---

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Web UI loads, but **streams won't start** / the play button does nothing / `403` on play | You opened xg2g over plain HTTP on a LAN IP, or the page's origin isn't allow-listed | Open it via your **HTTPS** URL, and set `XG2G_ALLOWED_ORIGINS` to that exact origin |
| **Container refuses to start** (`trustedProxies required` / `tls required`) | A public `XG2G_CONNECTIVITY_PROFILE` is set but its requirement is missing | Set `XG2G_TRUSTED_PROXIES` (reverse_proxy/tunnel) or `XG2G_TLS_ENABLED=true` (vps) — or use `lan` |
| HTTPS works, but **you're logged out repeatedly / no HSTS / cookies aren't `Secure`** | xg2g doesn't trust your proxy, so it ignores `X-Forwarded-Proto` | Set `XG2G_TRUSTED_PROXIES` to the proxy's source IP/CIDR |
| **Every visitor logs as the proxy's IP**; rate-limit throttles everyone together | Same — forwarded headers untrusted | Same — set `XG2G_TRUSTED_PROXIES` |
| Behind a proxy but **mixed-content / requests fail** | UI on HTTP while API on HTTPS (or vice-versa) | Serve UI and API from the **same** HTTPS origin |

Still stuck? Open the browser console (F12) on a failed play — a `403`, a CORS
message, or a `Mixed Content` warning each map to a row above.
