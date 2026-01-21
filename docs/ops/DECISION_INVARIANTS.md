# Decision Engine Invariants

Normative invariants that must always hold true.

## Invariant #9: MP4 Fast-Path Eligibility

IF `Mode == direct_play` THEN:

- `Container` MUST be `mp4` or `mov`
- `VideoCodec` MUST be supported by client
- `AudioCodec` MUST be supported by client
- `SupportsRange` MUST be `true`

## Invariant #10: Transcode Protocol

IF `Mode == transcode` THEN:

- `Protocol` MUST be `hls`
- `Protocol` CANNOT be `mp4`

## Invariant #11: Deny Output

IF `Mode == deny` THEN:

- `SelectedOutputUrl` MUST be empty
- `Outputs` list MUST be empty (or contain no playable URLs)
