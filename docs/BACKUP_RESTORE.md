# Backup and Restore

xg2g supports backing up and restoring critical application data for disaster recovery.

## What Gets Backed Up

- **Configuration**: All settings and API tokens
- **Generated Files**: M3U playlist, XMLTV EPG data
- **Application State**: Last refresh timestamp, channel mappings

## Quick Start

### Create Backup

```bash
# Backup to file
docker exec xg2g xg2g backup --output /data/backup.tar.gz

# Or using docker cp
docker cp xg2g:/data/playlist.m3u ./backup/
docker cp xg2g:/data/xmltv.xml ./backup/
```

### Restore Backup

```bash
# Restore from file
docker exec xg2g xg2g restore --input /data/backup.tar.gz

# Or copy files manually
docker cp ./backup/playlist.m3u xg2g:/data/
docker cp ./backup/xmltv.xml xg2g:/data/
```

## Manual Backup

For simple deployments, manually back up these files from the data directory:

```bash
/data/
├── playlist.m3u    # M3U playlist
├── xmltv.xml       # EPG data
└── config.yaml     # Configuration (optional)
```

## Docker Volume Backup

If using Docker volumes:

```bash
# Backup volume
docker run --rm \
  -v xg2g-data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/xg2g-backup.tar.gz -C /data .

# Restore volume
docker run --rm \
  -v xg2g-data:/data \
  -v $(pwd):/backup \
  alpine tar xzf /backup/xg2g-backup.tar.gz -C /data
```

## Kubernetes Backup

Use a CronJob for automated backups:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: xg2g-backup
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: alpine
            command:
            - sh
            - -c
            - |
              tar czf /backup/xg2g-$(date +%Y%m%d).tar.gz -C /data .
              find /backup -name "xg2g-*.tar.gz" -mtime +7 -delete
            volumeMounts:
            - name: data
              mountPath: /data
            - name: backup
              mountPath: /backup
          volumes:
          - name: data
            persistentVolumeClaim:
              claimName: xg2g-data
          - name: backup
            persistentVolumeClaim:
              claimName: xg2g-backup
          restartPolicy: OnFailure
```

## Best Practices

1. **Regular Backups**: Schedule daily backups during low-traffic periods
2. **Retention Policy**: Keep last 7 days, monthly archives
3. **Test Restores**: Periodically test backup restoration
4. **Off-site Storage**: Copy backups to remote location
5. **Encryption**: Encrypt backups containing sensitive data

## Recovery Time Objective (RTO)

- Manual file restore: < 5 minutes
- Full container recreation with backup: < 10 minutes
- Fresh refresh without backup: 5-30 minutes (depends on channel count)

## Notes

- Backups do not include cached data (regenerated on demand)
- API tokens in config should be handled securely
- After restore, trigger a refresh to update EPG: `POST /api/refresh`
