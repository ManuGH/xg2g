# Security Walkthrough

## Token URL Gate
- Denylisted query keys: token, access_token, auth, jwt, bearer (case-insensitive).
- CI runs `git grep -n -i -E '(\?|&)(token|access_token|auth|jwt|bearer)='` on every PR and main push.
- This gate only asserts the denylisted keys are absent; it does not prove other secrets are never present.
