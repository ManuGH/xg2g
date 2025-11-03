# xg2g Konfiguration

> Quelle: `docs/config.schema.json` (Draft 2020-12)

## Übersicht

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `api` | `object` |  | HTTP API server configuration for runtime control |
| `bouquets` | `array<string>` | ✓ | List of Enigma2 bouquet references (filenames or service references) |
| `dataDir` | `string` | ✓ | Base directory for generated XMLTV files and cache. Environment variables like ${HOME} are expanded. |
| `epg` | `object` | ✓ | Electronic Program Guide (EPG) fetching and generation configuration |
| `logLevel` | `string` |  | Logging verbosity level |
| `metrics` | `object` |  | Prometheus metrics exporter configuration |
| `openWebIF` | `object` | ✓ | OpenWebIF API configuration for Enigma2 receiver |
| `picons` | `object` |  | Picon/channel logo configuration |
| `version` | `string` | ✓ | Configuration version identifier |

## `api`

**Typ:** `object`  
**Pflicht:**   

HTTP API server configuration for runtime control

**Felder:**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `listenAddr` | `string` |  | API server listen address (host:port) |
| `token` | `string` |  | Bearer token for API authentication. Can also be provided via environment variable XG2G_API_TOKEN. Never logged in plaintext. Environment variables are expanded. |

### `api.listenAddr`

**Typ:** `string`  
**Pflicht:**   
**Default:** `:8080`  

API server listen address (host:port)

**Beispiel:**

```yaml
:8080
```

### `api.token`

**Typ:** `string`  
**Pflicht:**   

Bearer token for API authentication. Can also be provided via environment variable XG2G_API_TOKEN. Never logged in plaintext. Environment variables are expanded.

**Beispiel:**

```yaml
my-secret-token
```

## `bouquets`

**Typ:** `array<string>`  
**Pflicht:** ✓  

List of Enigma2 bouquet references (filenames or service references)

**Beispiel:**

```yaml
[userbouquet.favourites.tv]
```

**Elementtyp:**
**Typ:** `string`  
**Pflicht:**   

**Beispiel:**

```yaml
userbouquet.favourites.tv
```

## `dataDir`

**Typ:** `string`  
**Pflicht:** ✓  

Base directory for generated XMLTV files and cache. Environment variables like ${HOME} are expanded.

**Beispiel:**

```yaml
/var/lib/xg2g
```

## `epg`

**Typ:** `object`  
**Pflicht:** ✓  

Electronic Program Guide (EPG) fetching and generation configuration

**Felder:**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `days` | `integer` |  | Number of days of EPG data to fetch (1-14) |
| `enabled` | `boolean` |  | Enable or disable EPG fetching and XMLTV generation |
| `fuzzyMax` | `integer` |  | Maximum Levenshtein distance for fuzzy matching service names (0-10) |
| `maxConcurrency` | `integer` |  | Maximum concurrent EPG requests to the receiver (1-10) |
| `retries` | `integer` |  | Number of retry attempts for failed EPG requests (0-5) |
| `source` | `string` |  | EPG data source: 'bouquet' (fetch full bouquet EPG) or 'per-service' (fetch per service) |
| `timeoutMs` | `integer` |  | Timeout in milliseconds for individual EPG requests (100-60000ms) |
| `xmltvPath` | `string` |  | Output path for generated XMLTV file (relative to dataDir or absolute) |

### `epg.days`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `7`  

Number of days of EPG data to fetch (1-14)

**Beispiel:**

```yaml
7
```

### `epg.enabled`

**Typ:** `boolean`  
**Pflicht:**   
**Default:** `true`  

Enable or disable EPG fetching and XMLTV generation

### `epg.fuzzyMax`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `2`  

Maximum Levenshtein distance for fuzzy matching service names (0-10)

**Beispiel:**

```yaml
2
```

### `epg.maxConcurrency`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `5`  

Maximum concurrent EPG requests to the receiver (1-10)

**Beispiel:**

```yaml
5
```

### `epg.retries`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `2`  

Number of retry attempts for failed EPG requests (0-5)

**Beispiel:**

```yaml
2
```

### `epg.source`

**Typ:** `string`  
**Pflicht:**   
**Default:** `per-service`  
**Erlaubte Werte:** `bouquet`, `per-service`  

EPG data source: 'bouquet' (fetch full bouquet EPG) or 'per-service' (fetch per service)

**Beispiel:**

```yaml
per-service
```

### `epg.timeoutMs`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `15000`  

Timeout in milliseconds for individual EPG requests (100-60000ms)

**Beispiel:**

```yaml
15000
```

### `epg.xmltvPath`

**Typ:** `string`  
**Pflicht:**   

Output path for generated XMLTV file (relative to dataDir or absolute)

**Beispiel:**

```yaml
guide.xml
```

## `logLevel`

**Typ:** `string`  
**Pflicht:**   
**Default:** `info`  
**Erlaubte Werte:** `debug`, `info`, `warn`, `error`  

Logging verbosity level

## `metrics`

**Typ:** `object`  
**Pflicht:**   

Prometheus metrics exporter configuration

**Felder:**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `enabled` | `boolean` |  | Enable or disable Prometheus metrics endpoint |
| `listenAddr` | `string` |  | Metrics server listen address (host:port) |

### `metrics.enabled`

**Typ:** `boolean`  
**Pflicht:**   
**Default:** `false`  

Enable or disable Prometheus metrics endpoint

### `metrics.listenAddr`

**Typ:** `string`  
**Pflicht:**   
**Default:** `:9090`  

Metrics server listen address (host:port)

**Beispiel:**

```yaml
:9090
```

## `openWebIF`

**Typ:** `object`  
**Pflicht:** ✓  

OpenWebIF API configuration for Enigma2 receiver

**Felder:**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `backoff` | `string` |  | Initial backoff duration between retries (Go duration format) |
| `baseUrl` | `string` | ✓ | Base URL of the Enigma2 receiver's OpenWebIF interface |
| `maxBackoff` | `string` |  | Maximum backoff duration for exponential backoff (Go duration format) |
| `password` | `string` |  | OpenWebIF authentication password (if required). Can also be provided via environment variable XG2G_OWI_PASS. Never logged in plaintext. Environment variables like ${PASS} are expanded. |
| `retries` | `integer` |  | Number of retry attempts for failed HTTP requests |
| `streamPort` | `integer` |  | Port number for IPTV streaming URLs |
| `timeout` | `string` |  | HTTP request timeout duration (Go duration format: 10s, 500ms, 1m) |
| `username` | `string` |  | OpenWebIF authentication username (if required). Environment variables like ${USER} are expanded. |

### `openWebIF.backoff`

**Typ:** `string`  
**Pflicht:**   
**Default:** `500ms`  

Initial backoff duration between retries (Go duration format)

**Beispiel:**

```yaml
500ms
```

### `openWebIF.baseUrl`

**Typ:** `string`  
**Pflicht:** ✓  
**Format:** `uri`  

Base URL of the Enigma2 receiver's OpenWebIF interface

**Beispiel:**

```yaml
http://receiver.local
```

### `openWebIF.maxBackoff`

**Typ:** `string`  
**Pflicht:**   
**Default:** `30s`  

Maximum backoff duration for exponential backoff (Go duration format)

**Beispiel:**

```yaml
30s
```

### `openWebIF.password`

**Typ:** `string`  
**Pflicht:**   

OpenWebIF authentication password (if required). Can also be provided via environment variable XG2G_OWI_PASS. Never logged in plaintext. Environment variables like ${PASS} are expanded.

**Beispiel:**

```yaml
dreambox
```

### `openWebIF.retries`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `3`  

Number of retry attempts for failed HTTP requests

**Beispiel:**

```yaml
3
```

### `openWebIF.streamPort`

**Typ:** `integer`  
**Pflicht:**   
**Default:** `8001`  

Port number for IPTV streaming URLs

**Beispiel:**

```yaml
8001
```

### `openWebIF.timeout`

**Typ:** `string`  
**Pflicht:**   
**Default:** `10s`  

HTTP request timeout duration (Go duration format: 10s, 500ms, 1m)

**Beispiel:**

```yaml
10s
```

### `openWebIF.username`

**Typ:** `string`  
**Pflicht:**   

OpenWebIF authentication username (if required). Environment variables like ${USER} are expanded.

**Beispiel:**

```yaml
root
```

## `picons`

**Typ:** `object`  
**Pflicht:**   

Picon/channel logo configuration

**Felder:**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|:---:|---|
| `baseUrl` | `string` |  | Base URL for picon/logo images. Environment variables are expanded. |

### `picons.baseUrl`

**Typ:** `string`  
**Pflicht:**   

Base URL for picon/logo images. Environment variables are expanded.

**Beispiel:**

```yaml
http://receiver.local/picon
```

## `version`

**Typ:** `string`  
**Pflicht:** ✓  
**Default:** `1`  

Configuration version identifier

**Beispiel:**

```yaml
1
```

