# Runbook: Low /data Free Space

**Alert**: `DataVolumeSpaceLow`

## Immediate checks
- Validate filesystem metrics in Prometheus (`node_filesystem_avail_bytes{mountpoint="/data"}`).
- Check pod logs for write failures or disk warnings.
- Inspect node filesystem usage:
  ```bash
  kubectl -n xg2g describe pod <pod>
  kubectl debug node/<node> -- chroot /host df -h /data
  ```

## Probable causes
- Unexpected growth in generated playlists or XMLTV data.
- Logs or temporary files accumulating under `/data`.
- Node-level disk pressure impacting emptyDir volume.

## Mitigation steps
1. SSH or debug into node and remove stale files under `/var/lib/kubelet/pods/.../volumes/.../data`.
2. Consider migrating to a PersistentVolume with larger capacity.
3. Add retention/cleanup job for old artifacts.
4. If node disk pressure is global, cordon and drain node before remediation.

## Post-incident
- Adjust alert threshold if necessary.
- Document expected data footprint and cleanup schedule.
