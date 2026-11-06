# Large body deduplication

Optional content-defined chunking (CDC) and per-chunk Zstandard compression let large detached bodies share identical byte ranges. Reading reconstructs the original bytes and verifies their full SHA-256. Proxy traffic, payloads and UI protocols are unchanged.

## Configuration

```yaml
storage:
  body_chunking: true
```

Restart after changing this setting. It is **disabled by default** because chunking adds hashing, filesystem and decoding costs. Only newly stored detached bodies of at least 128 KiB use this format. Small bodies, existing files and SQLite inline previews are not rewritten.

Existing `logging.detach_body_over_bytes` and `logging.body_preview_bytes` settings still determine which bodies are detached. This option does not move historical inline bodies out of SQLite or shrink their previews.

Disabling the option stops new chunked writes; existing chunked bodies remain readable. Older programs cannot directly read the new local layout. Before downgrading, export a full backup and restore into a new directory.

## Compatibility and cleanup

- Public references remain the SHA-256 of the complete original bytes.
- Legacy bodies stay at `<body-dir>/<prefix>/<hash>`. New manifests use `<hash>.manifest`; chunks use `chunks/<prefix>/<chunk-hash>.chunk`.
- Chunk boundaries are content-defined: 16 KiB minimum, approximately 64 KiB target and 256 KiB maximum. Incompressible chunks retain their original bytes.
- Manifests reference chunks directly, not earlier logs. Chunks are persisted before a manifest is published.
- GC marks live dependencies and recent manifests, rechecks age under the deletion lock, and refuses to proceed with malformed live manifests. Storage accounting uses actual file sizes; log body sizes remain logical sizes.
- Full backups reconstruct original bodies into the existing format-1 ZIP. Restores install complete legacy files. Backup space estimates use reconstructed sizes.

## Measurement

```sh
go test ./internal/storage -run '^$' -bench BenchmarkBlobStorageDataset -benchtime=1x -count=3
```

Compare `stored-B`, `write-ms` and `read-ms`: `raw` is the original file baseline, `default` exercises the disabled option, `zstd` compresses each body independently, and `cdc-zstd` chunks and compresses. Experimental files are temporary and all readbacks are checked byte for byte.

Optionally set `PRISMCAT_BLOB_BENCH_DB` to an absolute database path to read the latest 100 inline request bodies, capped at 256 MiB. The database is read-only and only aggregate measurements are printed.

Initial measurements show substantial space savings for growing shared prefixes, with slower writes and reads. Mixed small-body samples may occupy more space than independent compression. See [the recorded measurements](BLOB_STORAGE_CN.md) for details. Historical online migration, dictionary training and packed chunk containers are outside this change; search is separate.
