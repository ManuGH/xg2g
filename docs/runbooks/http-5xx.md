# Runbook: HTTPErrorRateHigh

**Alert**: `HTTPErrorRateHigh` – HTTP 5xx rate > 0.1 req/s for 5 minutes.

## Immediate checks
- Confirm the alert in Alertmanager and note recent deploys/releases.
- Inspect application logs (`kubectl logs deploy/xg2g`) for stack traces.
- Check `/readyz` and `/healthz` via port-forward to confirm service health.

## Probable causes
- Downstream OpenWebIF unavailable or returning 5xx.
- Misconfigured API token/credentials causing auth failures.
- Recent code change introducing regressions.

## Mitigation steps
1. Port-forward the deployment:
   ```bash
   kubectl -n xg2g port-forward deploy/xg2g 8080:8080
   ```
2. Trigger `/api/refresh` with a valid token to reproduce error.
3. If OpenWebIF is down, escalate to receivers/TV ops team; consider throttling refresh jobs.
4. Roll back to last known good image if issue is caused by recent deploy.

## Post-incident
- File an issue with root cause summary.
- Add regression test or guardrail where applicable.
