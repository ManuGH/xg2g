# ADR-033: Capability-Driven Playback Action for Live Ingest

- **Status:** Proposed
- **Date:** 2026-08-22
- **Builds on:** ADR-028 (Playback Capability Claims and Verified Truth)

## Context

A bouquet audit of all 144 services on the reference receiver found 31 that carry
MPEG Layer II as their only audio. Native iOS has no MPEG Layer II decoder — measured
on the platform itself through `kAudioFormatProperty_DecodeFormatIDs`, which reports
AC-3, E-AC-3, AAC and MPEG Layer III as decodable and Layer II as absent. Those 31
channels therefore play a picture and no sound.

Today's live readiness criterion asks whether an audio elementary stream is named and
whether its packets arrive clear. Both hold on all 31. A readiness signal built on it
would report READY on every one of them, which is the failure the readiness work
exists to prevent: a channel declared presentable that cannot be presented.

The naive repair is a rule that MPEG Layer II is not ready. It is wrong. Whether a
codec can be presented is not a property of the codec, and not a property of the
channel; it is a relation between what the transport carries and what the requesting
client can decode. Another client decodes Layer II perfectly well, and the same
question will be asked again for the next codec and the next client generation.

## Decision

**The live path does not gain a capability engine. It gains access to the two that
already exist.**

This was the substantive finding while designing this: the decision machinery is
already built, already generic, and already free of any channel identity.

- `playbackcompat` is the capability authority. ADR-028 established
  `Raw → Verified → Effective`: a client states what it supports, server policy may
  narrow that statement but never widen it, and only `Effective` may enter a playback
  decision. It already carries a rule of exactly the shape needed here — MP2 removed
  for native Android clients, because older app versions advertised support they did
  not have.
- `playbackplanner` is the decision authority. `PlaybackEvidence` pairs a `SourceTruth`
  (container, video codec, audio codec, dimensions, interlacing) with a
  `ClientEvidence` (supported containers, video codecs, audio codecs, limits), and
  `resolveMediaTargets` already emits a per-track plan. `isAudioCodecCompatible` reads
  the source's audio codec, applies the `playbackcompat` veto, and checks membership
  in the client's supported set. No service reference, no provider, no channel name
  appears anywhere in it.
- `backend/internal/stream/` references neither package. That is the entire gap.

### Playback actions

The four actions are a naming of what the planner already computes, plus one new
terminal state:

| Action | Video | Audio | Condition |
|---|---|---|---|
| `DIRECT` | copy | copy | every transport codec is in the client's effective set |
| `AUDIO_TRANSCODE` | copy | transcode | video compatible, audio not, audio convertible |
| `FULL_TRANSCODE` | transcode | transcode | video not compatible and transcode permitted |
| `UNPRESENTABLE` | — | — | no action can produce a presentable stream |

`AUDIO_TRANSCODE` is not new: `resolveMediaTargets` already sets
`plan.Video = TrackPlan{Mode: "copy"}` inside a transcode plan when
`isVideoCodecCompatible` holds. What is new is `UNPRESENTABLE`, and using any of it
for live.

MPEG Layer II to AAC must not appear as a rule anywhere. It is the consequence of one
generic rule — *an audio codec outside the client's effective set, for which a
conversion target inside that set exists, is transcoded* — evaluated against a
particular transport and a particular client. A client that decodes Layer II gets
`DIRECT` for the same channel. A future codec is handled without touching this code.

### `UNPRESENTABLE` is a safety net, not the answer for convertible codecs

A convertible codec resolves to `AUDIO_TRANSCODE`. `UNPRESENTABLE` is reserved for
what no action repairs: no audio stream at all, an audio codec with no conversion
target, a video codec the client cannot decode where transcoding is denied by policy
or unavailable.

It matters that this state is terminal and early. Presentation readiness for such a
channel never becomes true, so a preparation waiting for it would hold a tuner until
timeout — worse than today. The decision is available as soon as the PMT is complete,
so the failure path resolves in a few hundred milliseconds rather than seconds, and
the tuner is released immediately.

## Where the decision sits

`SourceTruth` for live costs nothing to obtain. Elsewhere it comes from probing; in
the live ingest the ring already assembles PAT and PMT, and already reports the video
codec, the audio PIDs and their stream types as readiness facts. The evidence needed
for the decision is a by-product of work that runs anyway for transport readiness.

The decision is made **once per ingest, when the PMT completes** — not per packet, not
per client read. This is what keeps the compatible case free:

```
tune → first PAT → complete PMT → SourceTruth
                                     ↓
                    playbackcompat.Resolve(client claims) → Effective
                                     ↓
                            playbackplanner → action
                                     ↓
        DIRECT ────────────► existing path, byte for byte unchanged
        AUDIO_TRANSCODE ───► derived variant, fed from the master ring
        UNPRESENTABLE ─────► terminal, tuner released
```

For `DIRECT` there is no added work in the data path at all: no branch per packet, no
copy, no inspection. The ring, the normalizer and primed attach behave exactly as they
do today. The cost of the decision is one evaluation per ingest against data already
parsed.

### The transcode sits behind the ring, never in front of it

The master ring always holds the receiver's original transport, bit for bit. A
variant is a *consumer* of that ring, not a stage in front of it:

```
Vu+ ──► session ──► normalizer ──► MasterRing ──┬──► DIRECT subscribers
                                                 └──► variant (audio transcode) ──► its subscribers
```

Consequences worth stating:

- One upstream and one tuner regardless of how many variants exist. The tuner budget
  is unaffected by this feature.
- A channel nobody needs converted never starts a transcoder.
- The passthrough path is not merely cheap, it is untouched — which is what makes this
  safe to add to a pipeline whose behaviour was measured this carefully.

The variant needs its own random access index: video frames are copied unchanged, but
rewriting the PMT and replacing audio PES shifts every byte offset. It re-uses the
existing classifier; nothing new is introduced there.

### Upstream sharing is never fragmented by a variant

There are two keys, and conflating them would undo the session-sharing work:

```
UpstreamSessionKey = ServiceRef + TargetProgram      ← one dial, one lease, one tuner
    └── one normalizer, one master ring (the receiver's original bytes)
            ├── DIRECT subscribers            read the master ring
            └── VariantKey = capability class ← one shared variant pipeline
                    └── variant subscribers   read the variant ring
```

The upstream key never mentions a variant. Adding one there would let two output
formats of the same programme open two dials on the receiver, which is precisely what
the session sharing exists to prevent and what the tuner budget cannot absorb.

`VariantKey` is derived from the *capability class the decision depends on* — the
resolved action plus its target codecs — never from a client, device or session
identity. Two Sterling clients resolve to one key and share a single conversion. A
client whose effective set contains the source codec resolves to `DIRECT` and reads
the master ring directly, at the same time, from the same upstream.

One receiver stream and one tuner, whatever mix of clients is watching.

### Decision identity is a fingerprint, not a version number

The PMT `version_number` is five bits. It wraps after 32 changes, so a cache keyed on
it alone can eventually serve a stale decision for a genuinely different stream — the
worst failure this design can have, because the mismatch is silent.

The cache key is a fingerprint over the normalised facts the decision actually reads:
video PID and codec, and for every audio stream the PID, stream type, resolved codec
and language. `version_number` stays as a cheap change hint, not as identity: a bump
prompts re-evaluation, and the fingerprint decides whether anything really changed. A
change in the fingerprint must invalidate the decision unconditionally.

The same fingerprint belongs in the readiness observer's `streamIdentity`, which today
holds only programme number, PMT version, video PID and video codec. Presentability
can change without video changing at all.

### The time base for an audio conversion

Measured on the wire rather than assumed. ATV HD carries MPEG-1 Layer II, 160 kbps,
**48 kHz**, joint stereo, four frames per PES, with PES timestamps a constant 8640
ticks apart.

At 48 kHz both frame sizes are exact in the 90 kHz timestamp clock:

| | samples/frame | 90 kHz ticks | exact |
|---|---|---|---|
| MPEG-1 Layer II | 1152 | 2160 | yes |
| AAC-LC | 1024 | 1920 | yes |

Drift is therefore not inherent to the conversion. It can only be introduced by
implementing it wrongly, which makes the rules below non-negotiable rather than
best-effort.

**Timestamps are computed from a sample count, never accumulated.** For output frame
`n` after an anchor:

```
PTS(n) = anchor + (totalSamplesOut(n) * 90000) / sampleRate      // int64, exact at 48 kHz
```

Incremental addition of a per-frame delta is forbidden even where the delta is exact,
because it makes a single rounding error permanent. For a rate where the division is
not exact (44.1 kHz is the only realistic one), the remainder is carried so the error
stays bounded below one tick instead of accumulating.

**The anchor preserves the relationship to video and PCR.** Video packets and PCR are
copied untouched, so the offset between the first audio presentation time and the
video timeline is the only thing that can move. The anchor is the PTS of the source
frame whose samples begin the first emitted output frame, corrected for encoder
priming — an AAC encoder emits lead-in samples that carry no source audio, and
ignoring them shifts the whole programme by roughly 21 ms per priming frame. Either
the priming output is dropped and the anchor advanced, or the anchor is moved back by
the priming duration; the acceptance test is the same either way: first-audio-PTS
minus first-video-PTS is unchanged from the source within one frame.

Frame boundaries do not align between the two codecs — 1152 and 1024 share only a
factor of 128, so nine source frames span 10.125 output frames and no periodic
realignment exists. This is why the encoder buffers, and why the sample count, not the
frame count, is the unit of the timestamp arithmetic.

**Discontinuity, PMT change and zap all re-anchor.** A discontinuity indicator, or a
source PTS jump beyond a threshold, flushes the encoder, resets the sample counter and
sets a new anchor; carrying a counter across a discontinuity would place the audio at
a time the video no longer occupies. A PMT change whose fingerprint alters the audio
configuration retires the variant and builds a new one rather than adapting in place.
A zap is a new upstream generation and therefore a new variant instance.

**The variant rewrites the PMT and carries its own preamble.** Stream type becomes
0x0F, the language descriptor is preserved, codec-specific descriptors of the source
format are dropped, and the CRC is recomputed. The audio PID is kept so the structure
stays comparable with the source. PCR PID and video PID are untouched. The variant
serves its own PAT/PMT preamble on attach, exactly as the master ring does, since a
client joining it must configure a decoder for the converted stream.

**The variant indexes its own random access points.** Video access units are copied
bit for bit, so the entry points fall on the same pictures — but rewriting the PMT and
replacing audio PES moves every byte offset, so the offsets cannot be inherited. The
existing classifier runs over the variant ring unchanged; no second classification
rule is introduced.

**Lifecycle.** The variant is itself a subscriber of the master ring and holds a
reference to it, so the upstream cannot be closed while a variant is alive. Variant
subscribers are reference-counted like session leases. When the last one leaves, the
variant is retired after the same warm-hold the sessions use, and its encoder is
released; the master ring and the upstream survive as long as any DIRECT subscriber
remains. A channel nobody needs converted never starts an encoder at all.

**Where the encoder runs is an open question, not a detail.** `backend/internal/stream/`
spawns no processes today — the live ingest is pure Go. An audio conversion therefore
introduces either a subprocess per variant (isolated failure domain, no cgo, but a
process boundary in a path that currently has none) or an in-process decoder and
encoder (no boundary, but a cgo dependency, since no maintained pure-Go AAC encoder
exists). The recommendation is the subprocess, on the grounds that it fails
independently of the ingest and can be killed and restarted without touching the
master ring — but it must be chosen deliberately, because it is the first process the
live path would own.

## Effect on readiness

Readiness splits along the line the failure exposed:

- **Transport readiness** is client-agnostic and stays where it is: PAT and PMT
  complete, video parameter sets and a random access point present, at least one audio
  elementary stream whose codec is *named*, and clear transport on both.
- **Presentation readiness** is the relation: transport readiness for the stream the
  client will actually receive — the original under `DIRECT`, the variant under
  `AUDIO_TRANSCODE`.

Only presentation readiness may trigger a commit. Under `AUDIO_TRANSCODE` it is
answered about the converted audio, so it arrives later by the transcoder's start-up —
a cost paid only by channels that need it.

`streamIdentity` in the readiness observer currently holds programme number, PMT
version, video PID and video codec. It must gain the audio side: presentability can
change without video changing, and a change there invalidates the timings and the
decision alike.

## Consequences and risks

- **Audio/video synchronisation is the real risk, not CPU.** An MPEG Layer II frame is
  1152 samples (24 ms at 48 kHz); an AAC frame is 1024 (21.33 ms). Frame boundaries no
  longer align with the source, so presentation timestamps must be recomputed rather
  than carried over. The normalizer's PCR pacing and the client's audio lead were both
  tuned against unconverted audio and must be re-measured for the variant path.
- The PMT must be rewritten for the variant (stream type, and the PID if the codec
  changes) while PCR and video PIDs stay untouched.
- Transcoding is generation loss. Audio-only conversion at broadcast bitrates is
  modest, and video remains bit-exact.
- Roughly 1–2% CPU per converted stream, against a full transcode's order of
  magnitude more. Heat and quality both favour this.
- An incorrectly classified client family becomes unnecessarily conservative — the
  risk ADR-028 already names, inherited here.

## What must not be built

- A second capability engine in the live path.
- Any rule naming a codec pair, a channel, a service reference, or a provider.
- A decision in the per-packet path.

## Mandatory evidence before AUDIO_TRANSCODE is enabled by default

Decision correctness:

- The measured MPEG-Layer-II-only services resolve to `AUDIO_TRANSCODE` for the native
  iOS client and to `DIRECT` for a client whose effective set contains that codec, with
  no channel-specific input anywhere.
- The remaining services resolve to `DIRECT`, and the bytes served to them are
  identical to today's — compared, not assumed.
- A service with no classifiable audio resolves to `UNPRESENTABLE` before the tuner is
  held longer than PMT completion.
- Live, DVR and recordings reach the same decision from the same evidence.

Conversion correctness:

- On a converted channel, video is bit-identical to the source and the audio is
  audible.
- Thirty minutes of continuous conversion with no measurable audio/video drift.
- Audio lead stable, zero underruns, zero PES errors over the same run.
- First-audio-PTS relative to first-video-PTS unchanged from the source, within one
  frame, across a re-anchor.

Transition correctness:

- Zaps across `AC-3 DIRECT → Layer II AUDIO_TRANSCODE → AC-3 DIRECT` leave no state of
  the previous action behind.
- A PMT bump that adds a decodable track to a previously undecodable service moves the
  decision back to `DIRECT` when that is the better path for the client.
- A discontinuity mid-programme re-anchors without a step in the audio timeline.

Sharing correctness:

- Two Sterling clients on one programme share a single variant pipeline.
- A Sterling client and a client that decodes the source codec share one upstream and
  one tuner while reading different outputs.
- **No playback variant, in any combination, causes a second dial to the receiver.**

## Open question for the decision, not for implementation

Whether `AUDIO_TRANSCODE` should be the default for Sterling on day one, or whether it
should ship behind an operator switch with `UNPRESENTABLE` plus a visible reason as the
initial behaviour. The second is smaller and reversible; the first is what makes the 31
channels usable. This ADR does not decide it.
