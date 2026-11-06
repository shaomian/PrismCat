# Body compression and chunk deduplication

Optional hybrid storage reduces repeated Agent history in detached bodies (Blobs). Reads reconstruct the original bytes; proxy traffic, UI APIs and logical references are unchanged.

## Enable

Configure and restart:

```yaml
storage:
  body_chunking: true
```

Disabled by default. New Blobs use whole-body Zstandard compression below **128 KiB**, and shared content-defined chunks (CDC) plus compression at or above that size. Incompressible bodies or chunks retain their original bytes.

Existing `logging.detach_body_over_bytes` and `logging.body_preview_bytes` settings still control detachment. **Inline SQLite bodies and previews are not reduced**, and historical files are not migrated. Disabling the option still allows reading all existing representations.

## Storage and cleanup

- References remain `sha256:<hash>` of the complete original bytes. Compressed reads verify SHA-256.
- Legacy files remain at `<body-dir>/<prefix>/<hash>`. Small compressed bodies use `<hash>.zstd`; large bodies use `<hash>.manifest` and shared `chunks/<prefix>/<chunk-hash>.chunk` files.
- CDC boundaries: 16 KiB minimum, approximately 64 KiB target, 256 KiB maximum. Manifests reference chunks directly, never previous logs.
- Files are atomically published and synced. Directory syncs are batched per body and phase, persisting dependencies before publishing the manifest.
- GC retains live and recent unpublished dependencies, rechecks age before deletion and stops on malformed live manifests. Shared chunks are removed only after all roots expire and the grace period passes.
- Storage accounting uses actual file sizes; body sizes remain logical sizes.

Full backups reconstruct original bytes into the existing format-1 ZIP; restores install complete raw files. Backup estimates use reconstructed sizes. Older programs cannot read the new local layout directly: export a full backup and restore into a new directory before downgrading.

## Verification and tradeoffs

Repeated histories can share bytes. Large unrelated bodies may be slower and occupy more space than whole-body compression. The 128 KiB threshold is an offline Agent-sample compromise, not a universal optimum. Compression, hashing and multiple files add work to the asynchronous log writer; enable according to your workload.

```sh
go test ./internal/storage -run '^$' -bench BenchmarkBlobStorageDataset -benchtime=1x -count=3
```

`raw-sync`, `zstd-sync` and `hybrid-sync` use equivalent per-file sync and body-scoped directory sync. This does **not** measure the default buffered raw writer's latency. Compare `stored-B`, `write-ms` and `read-ms`. File-size totals exclude filesystem allocation and directory overhead. Synthetic growing prefixes and small SSE samples exercise sharing rather than predict production compression; reads use warm caches, not cold disks or end-to-end proxy traffic.

Optionally set `PRISMCAT_BLOB_BENCH_DB` to an absolute database path to read the latest 100 inline request bodies, capped at 256 MiB. Only aggregate metrics are printed and temporary files are removed. Regression tests cover asynchronous SQLite persistence, body downloads/SSE/export, mixed-format backup/restore, shared-chunk cleanup and concurrent access.

Online historical migration, dictionary training and packed chunk containers are outside this change; search is separate.
