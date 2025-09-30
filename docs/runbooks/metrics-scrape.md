# Runbook: Metrics Scrape Issues

**Alerts**: `ServiceDown`, `MetricsScrapeStalled`

## Immediate checks
- Verify Prometheus can resolve the service IP (`kubectl -n xg2g get svc xg2g`).
- Confirm port-forwarded `/metrics` endpoint responds:
  ```bash
  kubectl -n xg2g port-forward deploy/xg2g 9090:9090
  curl -sS http://localhost:9090/metrics | head
  ```
- Check for Prometheus scrape errors in `prometheus` logs (target marked down).

## Probable causes
- Pod crashlooping or not Ready.
- NetworkPolicy or firewall blocking port 9090 within cluster.
- Prometheus scrape configuration missing annotations or ServiceMonitor.

## Mitigation steps
1. Confirm pod status: `kubectl -n xg2g get pods`.
2. Ensure Service has scrape annotations / ServiceMonitor deployed.
3. Restart Prometheus or re-apply scrape config if configuration drifted.
4. For ServiceDown: restart deployment or roll back image if crash caused by release.

## Post-incident
- Add alert suppression window if maintenance occurs.
- Update monitoring documentation with any configuration tweaks.
