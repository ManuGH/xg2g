# Runbook: Refresh Failures

**Alerts**: `RefreshFailures`, `NoRecentRefresh`

## Immediate checks
- Confirm if `/api/refresh` is returning 4xx/5xx via pod logs.
- Check the `xg2g_last_refresh_timestamp` metric and `channels` gauge in Grafana.
- Validate secrets/config maps for `XG2G_API_TOKEN`, `XG2G_OWI_BASE`, `XG2G_BOUQUET`.

## Probable causes
- OpenWebIF unreachable or responding with errors.
- Invalid API token configured in secret.
- Data directory unwritable or disk full.
- Application bug released in recent deploy.

## Mitigation steps
1. Refresh manually to gather error details:
   ```bash
   kubectl -n xg2g port-forward deploy/xg2g 8080:8080 &
   curl -v -X POST http://localhost:8080/api/refresh \
     -H "X-API-Token: <TOKEN>"
   ```
2. If token mismatch, rotate `xg2g-secrets` secret and redeploy.
3. Verify connectivity to OpenWebIF host (`nc -vz 10.10.55.57 80`).
4. Roll back to previous image if regression introduced.

## Post-incident
- Document root cause and resolution.
- Add automated smoke tests or synthetic refresh if gap identified.
