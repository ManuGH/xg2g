# R5 Production Smoke Matrix

E2E verification cases for Phase R5 Deployment Readiness.

| Case ID | Device / Profile | Strategy | Expected Mode | Protocol |
| :--- | :--- | :--- | :--- | :--- |
| **S1** | Apple TV (4K) | DirectPlay | `direct_play` | mp4 |
| **S2** | Safari iOS (Cellular) | Force DS (rng:false) | `direct_stream` | hls |
| **S3** | Web (Generic HLS) | Transcode (VP9) | `transcode` | hls |

### Verification Procedure

1. Run `make gate-decision-all`.
2. Post each case to `/api/v3/recordings/{id}/playback-info`.
3. Verify `decision_schema` telemetry in logs matches the payload style.
