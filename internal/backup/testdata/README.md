# Compatibility fixture

`legacy-v2.4.1.zip` was exported by `Create` before the cross-version compatibility
change, using the existing `TestCompleteBackupRestoreAndRollback` dataset and
`config.Version = "2.4.1"`. It is a real format-1 export from the pre-change
implementation, not a newer archive with its manifest version rewritten. The
backup feature was still uncommitted at the time of export; this is not claimed
to be an archive produced by the published v2.4.1 tag.

All data and credential values are synthetic test values. The archive includes
one SSE log with a binary detached body, annotations, Trace metadata, an active
capture/experiment run, a review template, a running report and its log evidence.
The source schema, JSON documents, configuration and manifest are frozen so
future migrations are tested against old data rather than newly generated data.

SHA-256: `88041ea0b11f1caf9fe8bb6ae74b76d99a9b441b1144e4fcb8443f8329329345`

Do not regenerate this file when the current schema changes. Add a new fixture
and verified source version when extending cross-version support.
