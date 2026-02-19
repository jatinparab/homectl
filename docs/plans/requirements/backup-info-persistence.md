# Backup System Improvements

## 1. Backup Metadata Persistence

### Problem
- Restoring right after a backup is wasteful (data was just backed up from this node)
- Backing up right after a restore is wasteful (data was just restored, nothing changed)
- Backup timer resets on every agent/manager restart (in-memory `LastBackupAt` initialized to `time.Now()`)

### Solution: Metadata file
Persist a metadata file at `${APPDATA}/<app-name>/.homectl-backup-meta.json` containing:
```json
{
  "last_backup_at": "2026-02-14T02:00:00Z",
  "snapshot_id": "43583abc",
  "node": "k8s-node-1"
}
```

### Restore flow (executeStart)
1. Read local metadata file from `${APPDATA}/<app-name>/.homectl-backup-meta.json`
2. If no metadata file exists → restore from latest backup (first start / unknown provenance)
3. If metadata file exists → query `restic snapshots --json --latest 1 --tag <app>` for latest snapshot
4. If latest snapshot is newer than `last_backup_at` in metadata → restore (another node backed up newer data)
5. Otherwise → skip restore, start directly with local data
6. After restore completes → overwrite the local metadata file with the actual snapshot timestamp from restic (the restored metadata file is always stale by one backup cycle, so always replace it)

### Backup flow (executeBackup)
1. Stop app
2. Run restic backup
3. On success: update metadata file with `last_backup_at = now()`, snapshot ID, and node name
4. On failure: update `LastBackupAt` in memory to `now()` (defer retry by 1 hour) but do NOT write metadata file
5. Start app again (handled by reconcile)

### Agent startup
- For each app with a metadata file, initialize `rt.Backup.LastBackupAt` from the persisted value
- If no metadata file, keep current behavior (`time.Now()` to defer first backup by 1 hour)

### Why the metadata file is inside the backup dir
The file lives at `${APPDATA}/<app-name>/` so it gets included in restic backups. However, the restored metadata is always one cycle stale (it reflects the *previous* backup's timestamp, not the one that just completed). That's why step 6 in the restore flow always overwrites it with the actual snapshot timestamp queried from restic.

## 2. Backup-Sensitive Apps (Active Usage Protection)

### Problem
Backups stop the app. If a user is actively using the app (e.g., watching a movie on Jellyfin), the backup interrupts them.

### Solution: Active connection check for backup-sensitive apps
Apps can opt in via `homectl-config.json`:
```json
{
  "backup_sensitive": true
}
```

The `backup_sensitive` flag is sent from the manager to the agent via the desired apps API response.

### shouldBackup logic for backup-sensitive apps
- `< 1h` since last backup → don't backup (normal interval)
- `1h - 24h` since last backup → check for active TCP connections on the app's primary port (`rt.Port`). If connections exist, defer. If none, backup.
- `> 24h` since last backup → force backup regardless of connections (safety net)

### Active connection check
Run `ss -tn state established dst :<port>` on the host. If any established connections exist, the app is in active use — defer the backup.

Only the first exposed port is checked (the same one the sensor tracks in `rt.Port`).

### Non-backup-sensitive apps
Unchanged behavior: backup after 1 hour, no connection check.

## 3. API Changes

### Manager → Agent: Desired apps response
Add `backup_sensitive` flag to the desired apps payload so the agent knows which apps need connection checking. The manager reads this from each app's `homectl-config.json`.

### Restic interface
Add a method to query latest snapshot info:
```
LatestSnapshot(appID) → (snapshotTime, snapshotID, error)
```
Used by the restore flow to compare against local metadata.
