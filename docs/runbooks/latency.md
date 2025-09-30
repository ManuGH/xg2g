# Runbook: High Refresh Latency (p95/p99)

**Alerts**: `HighLatencyP95`, `HighLatencyP99`

## Immediate checks
- Review recent refresh metrics in Grafana (panel: Refresh Duration).
- Check OpenWebIF responsiveness (`curl -w '%{time_total}' http://<OWI>/api/...`).
- Inspect pod CPU/memory usage (`kubectl top pods -n xg2g`).

## Probable causes
- OpenWebIF backend slow or overloaded.
- Network latency between xg2g and receiver.
- Large channel/bouquet causing longer processing.
- Recent code change introduced expensive operations.

## Mitigation steps
1. Trigger manual refresh to reproduce latency:
   ```bash
   kubectl -n xg2g port-forward deploy/xg2g 8080:8080
   curl -sS -X POST http://localhost:8080/api/refresh \
     -H "X-API-Token: <TOKEN>" -w '\nTotal: %{time_total}s\n'
   ```
2. Inspect application logs for warnings about slow OpenWebIF calls.
3. Validate OpenWebIF health; restart or fail over if necessary.
4. Consider reducing concurrent refresh frequency until root cause is addressed.

## Post-incident
- Update capacity/refresh schedule documentation.
- Add profiling or tracing if latency spikes recur.
