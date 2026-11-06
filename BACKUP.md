# Backup and migration

PrismCat supports full online backups in the console and offline CLI export, validation, restore and rollback. Same-version restoration and verified upgrades from older backups are supported. Restoration replaces destination data rather than merging instances.

## Version compatibility

- The archive remains `format_version: 1`. An optional `v` prefix on an application version is ignored.
- Same-version restores remain supported, including exactly matching development or prerelease labels.
- **2.4.1** is the currently verified cross-version source. Its backups can be migrated by this restore implementation in a newer stable release within the same major version. A frozen archive exported before the compatibility change is retained for regression tests; tests do not simulate old data by relabeling a newly generated archive.
- Other older source versions are not automatically accepted. Downgrades, cross-major restores, and restores between different development or prerelease versions are not enabled.
- Each additional cross-version source requires its own old-archive regression coverage. No new data-version field or ZIP format was introduced.

`backup inspect` checks the original archive's integrity and version admission. `backup restore --dry-run` also runs current database migrations and state recovery on a staging copy, reporting `target_app_version` and `upgrade_required`. Run the latter before installing data. The source archive remains unchanged; migration errors stop restoration before destination replacement.

## Online backup

Open **Settings → System → Backup and migration**, create a full backup, then download the completed ZIP and keep a copy on another device. Server copies are stored in `backups/` beside the active configuration file. Deleting a server backup does not affect application data.

An archive contains a consistent SQLite snapshot, every referenced raw body, portable configuration and a SHA-256 manifest. It preserves log IDs, annotations, tasks, Trace associations, experiments, review templates and reports. Online backups include persisted snapshot data; queued and in-flight updates may be absent.

Archives are **not encrypted** and may include request contents, configured credentials, analysis API keys and stored console passwords/hashes. Downloads require the existing console login. Environment-only console passwords are excluded. The archive records `PRISMCAT_*` variable names for manual migration and retains effective non-secret settings in the exported configuration.

Creation uses additional disk space and I/O. Body GC is blocked while snapshot bodies are copied; forwarding remains available. Missing or corrupt bodies fail the backup instead of publishing an incomplete archive.

## Web import and restore on restart

Open **Settings → System → Backup and migration → Import backup**, choose a complete ZIP and select **Upload and check**. The server checks integrity, compatibility, migrations and disk space on a staging copy without replacing live data. Review the source/destination versions, backup time and record counts, then select **Confirm restore on restart**.

Confirmation only schedules restoration. Stop business traffic and manually restart PrismCat, such as restarting its Docker container or system service. Restoration runs before opening the database or starting the proxy and cleanup workers. No restore command is needed, and the browser does not interrupt the service. A scheduled restore can be cancelled before restarting.

Business data is replaced as a whole. Existing `server` settings, including access, login and routing entry settings, are retained; environment overrides still apply at startup. Automatic cleanup stays paused for inspection. Existing data is retained as a coordinated rollback copy, with the CLI rollback command available for recovery.

Web import initially supports the standard layout: `config.yaml`, `prismcat.db` and `blobs/` in one data directory, including Docker volumes. The mount point is not renamed. Use the CLI to restore into a new directory for custom configuration filenames or external database/body locations.

Uploads stream to private storage, with an **8 GiB** limit and **30-minute** read deadline. Reverse proxies must also allow the file size and upload duration and preserve the browser-facing Host for same-origin validation. Larger archives can use the local CLI. Uploaded ZIPs, state and staging files live under `backups/imports/` beside the configuration, outside log eviction and subsequent backup payloads. Unconfirmed checks expire after 24 hours; confirmed operations do not expire while awaiting restart.

Export and upload validation are mutually exclusive. You can close the dialog during validation and return later. Uploaded files are removed after cancellation, failure or restoration; rollback copies remain. Interrupted uploads/checks are marked failed at startup and are never applied automatically. Interrupted installation uses the existing journal to attempt rollback; startup stops if consistency cannot be recovered, retaining CLI recovery rather than silently replaying an import.

## Final migration

Stop new source traffic and the source service, verify shutdown writes succeeded, then create a final backup. Run these commands from the PrismCat working directory; use `prismcat.exe` on Windows.

```sh
prismcat backup create --config data/config.yaml --file data/backups/final.zip
prismcat backup inspect --file data/backups/final.zip
```

Copy the archive and a compatible destination PrismCat version to the destination. Stop the destination service, preview and restore:

```sh
prismcat backup restore --file final.zip --target data --dry-run
prismcat backup restore --file final.zip --target data
prismcat -config data/config.yaml
```

`--target` accepts an absolute or working-directory-relative data directory. Restore installs `config.yaml`, `prismcat.db` and `blobs/` inside it and rewrites storage paths to destination absolute paths. Run restoration on the final destination rather than moving the restored directory afterwards. A Docker volume can be the target; its mount point is not renamed.

For an existing target, preview first and add `--replace`. Existing configurations pointing to a different storage layout must be restored into a new directory instead, avoiding silent replacement of configuration for external data.

To retain the destination's entire `server` section, including listening and login settings:

```sh
prismcat backup restore --file final.zip --target data --replace --keep-server-config data/config.yaml
```

Business routing, rules and analysis settings still come from the archive. Preview output includes record counts, target layout, server settings and environment variable names to check. Destination environment variables continue to override configuration at startup; check `PRISMCAT_DB_PATH` and `PRISMCAT_BLOB_DIR` in particular.

Restoration validates archive paths, sizes, hashes, database integrity and body references, then initializes and normalizes an isolated staging copy. Persistent journals protect the installation. Exclusive locks prevent offline operations against running instances of this version; older versions do not recognize the lock and must also be stopped manually.

## Protection and limits

Optional body chunking does not change the ZIP format: exports reconstruct original bytes and restores install complete raw files. See [body storage notes](BLOB_STORAGE.md) for configuration, compatibility and measured tradeoffs.

Restoration sets `storage.cleanup_paused: true`. Automatic retention, capacity eviction, body GC and manual expired-log cleanup remain paused across restarts. Verify counts, tasks, experiments, reports and bodies, then select **Resume automatic cleanup** in **System → Storage management**. Changing retention settings does not remove this protection.

Deleted, unsaved, truncated or previously redacted content cannot be recovered. SSE/WebSocket connections do not resume; persisted in-flight requests are marked interrupted by restore. Captures and experiment runs are finalized, and running review reports become interrupted. Historical IDs and relationships remain; restoration does not invoke AI.

Trace analysis chat histories currently live in each browser's localStorage and are excluded. Persisted review reports are included. Browser preferences/cookies, deployment files, reverse proxies, certificates, DNS and service definitions require separate migration.

## Rollback

Original database/journal files, bodies and configuration remain in the returned `rollback_directory`. Installation errors attempt automatic rollback. If interrupted, startup refuses to open partially restored data; recover with:

```sh
prismcat backup rollback --target data
```

To revert a completed restoration, stop the service and use its printed rollback directory:

```sh
prismcat backup rollback --target data --from data/.prismcat-rollback-<id>
```

Explicit rollback replaces data created since restoration. After verifying the migration and retaining other backups, old rollback directories may be removed manually. Validation and restore allow up to 250,000 payload files and 1 TiB of extracted content, additionally bounded by available disk space. Keep enough free space to copy original data during rollback.
