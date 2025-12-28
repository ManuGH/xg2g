v3.0 contract baseline fixtures for /api/v3.

These files represent the current runtime output shape and are used by
internal/api/contract_v3_test.go to detect drift.

Canonical baseline: fMP4 HLS output (.m4s + init.mp4).
TS fixtures exist only as a legacy regression sentinel; they are not canonical.

Known validation deferral: /sessions/{id} response body schema validation is
temporarily skipped until PR1 spec sync (OpenAPI mismatches runtime shape).
