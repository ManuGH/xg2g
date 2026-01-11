# API Reference: VOD Playback

## Overview

The VOD Playback API adheres to the **Thin-Client** principle. The backend is the source of truth for all playback logic, including stream resolution, format selection, and error handling. The UI is a "dumb" renderer of the DTOs provided by this API.

## Endpoints

### `GET /api/v3/vod/{recordingId}`

* **Scope**: `v3:admin` (or `v3:read` pending policy update).
* **Purpose**: Resolve playable assets for a given Recording ID.

#### Success Response (200 OK)

Returns a `VODPlaybackResponse` DTO.

```json
{
  "stream_url": "/api/v3/recordings/123/stream.mp4",
  "playback_type": "mp4",
  "duration_seconds": 3600,
  "mime_type": "video/mp4",
  "recording_id": "123"
}
```

* **Valid Playback Types**: `hls`, `mp4`.
* **Stream URL**: Can be relative (to API root) or absolute. Clients must support both.

#### Error Response (RFC 7807)

Returns a structured Problem Details JSON.

```json
{
  "type": "vod/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "Recording not found or file missing",
  "instance": "/api/v3/vod/123",
  "request_id": "50a952d5-74ae-4e9e-80fa-cfa12a7eb570"
}
```

* **Correlation**: `request_id` in the body MUST match the `X-Request-ID` response header.
* **Standard Types**:
  * `vod/not-found`: Recording ID unknown.
  * `system/internal`: Backend configuration error.

## Observability

* **Canonical Header**: `X-Request-ID` is the authoritative correlation identifier.
* **Logging**: All VOD requests and errors are logged with this ID.
