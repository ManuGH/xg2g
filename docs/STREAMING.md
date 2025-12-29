# Stream Resolution Standards (Core Invariants)

> **Critical operational standards** to prevent regression of stream resolution.
> All developers MUST adhere to these invariants.

---

## 1. NEVER Hardcode Stream Ports

**Rule**: The V3 streaming system MUST NOT guess or hardcode the stream port.

**Reason**: Enigma2 dynamically assigns ports based on channel configuration and receiver capabilities.

Hardcoding a specific port results in silent failures (black screen, no error) for channels using different port configurations.

**Correct Approach**: Always parse the URL returned by the Enigma2 WebAPI.

---

## 2. WebAPI is the Source of Truth

**Rule**: All stream requests MUST be resolved via the Enigma2 Web API (`/web/stream.m3u?ref=...`).

**Reason**: This API:

- Handles the "Zap" command (tune to channel)
- Returns the correct, active stream URL (including the dynamically assigned port)
- Ensures the receiver is tuned before streaming starts

**Implementation**: Use the `ResolveStreamURL()` method from the Enigma2 client. Never construct stream URLs manually.

---

## 3. Parse Returned URLs

**Rule**: Extract host, port, and path components from the WebAPI response URL.

**Reason**: The receiver provides the complete stream URL. Attempting to modify or reconstruct it can break playback.

**Implementation**: After calling `/web/stream.m3u`, parse the returned URL and use it directly with FFmpeg.

---

## Why These Invariants Matter

These rules capture hard-won operational lessons from stream resolution debugging:

1. **Port guessing was the original bug** - Early versions hardcoded ports, breaking certain channel configurations
2. **WebAPI is authoritative** - Only the receiver knows which port and path to use
3. **Direct URL usage is safest** - No reconstruction or modification needed

**Violation of these invariants will break stream resolution for certain channels.** All stream resolution code must follow this pattern.

---

## Implementation Checklist

When implementing stream resolution:

- [ ] Never hardcode stream ports
- [ ] Always call `/web/stream.m3u?ref=<serviceRef>` to get stream URL
- [ ] Parse the returned URL to extract host, port, and path
- [ ] Use the `ResolveStreamURL()` method (don't reimplement)
- [ ] Use returned URLs directly without modification
- [ ] Add tests for different channel configurations

---

## Common Pitfalls

### ❌ Wrong: Hardcoding port

```go
// WRONG - breaks channels with different port configurations
streamURL := fmt.Sprintf("http://%s:8001/%s", receiverHost, serviceRef)
```

### ✅ Correct: Using WebAPI

```go
// CORRECT - works for all channels
streamURL, err := client.ResolveStreamURL(ctx, serviceRef)
if err != nil {
    return err
}
// Use the URL directly with FFmpeg
```

---

## Version History

- **v3.0.0** (2025-12-24): Initial streaming standards
- **Unreleased**: Updated function names and removed implementation-specific details
