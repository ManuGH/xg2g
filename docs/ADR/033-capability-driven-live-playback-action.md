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

### The session key must carry the variant

Live sessions are keyed by `{ServiceRef, TargetProgram}` and shared by every viewer of
that service. With variants that key is no longer sufficient: a client that decodes
Layer II would be handed a converted stream, or the reverse.

The key gains a variant discriminator derived from the *effective capabilities that
the decision actually depends on* — not from a client or device identity. Two iPhones
resolve to the same discriminator and share one ring; an iPhone and a Layer-II-capable
client resolve to two, sharing the upstream but not the variant.

### A cache keyed on PMT version is not a channel list

The action for a given service is stable while its PMT version is, so it may be
cached on `{service, PMT version, variant}` and reused on the next zap to the same
channel. That cache invalidates itself: a broadcaster changing the audio layout bumps
the PMT version, which is already how the ring detects re-identification.

This is a cache, not configuration. It holds no channel names, is never edited, and is
empty on start. A user with a different 300-channel bouquet gets correct decisions with
no list to maintain.

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

## Acceptance criteria

- The 31 measured Layer-II services resolve to `AUDIO_TRANSCODE` for the native iOS
  client and to `DIRECT` for a client whose effective set contains `mp2`, with no
  channel-specific input.
- The remaining 89 resolve to `DIRECT`, and their served bytes are identical to today.
- A service with no classifiable audio resolves to `UNPRESENTABLE` before the tuner is
  held for longer than PMT completion.
- Live, DVR and recordings reach the same decision for the same evidence, through
  `playbackcompat` and `playbackplanner` rather than parallel copies.

## Open question for the decision, not for implementation

Whether `AUDIO_TRANSCODE` should be the default for Sterling on day one, or whether it
should ship behind an operator switch with `UNPRESENTABLE` plus a visible reason as the
initial behaviour. The second is smaller and reversible; the first is what makes the 31
channels usable. This ADR does not decide it.
