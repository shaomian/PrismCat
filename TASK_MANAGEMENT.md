# Archive, delete and manual cleanup

Traces, stopped capture runs, review cards and custom templates have a lightweight Manage menu for archive, restore or delete actions. Click a trace, capture or review's name or content to open its details. Active captures emphasize Stop capture and show their management menu after recording stops.

Selection checkboxes stay hidden during normal browsing. Click Batch manage to select items on the current page; individual management menus are hidden in this mode. Active captures cannot be selected, and Select this page includes only stopped captures. Exit selection hides the checkboxes and clears the selection; changing filters or pages also exits selection.

Archiving hides an item from the default list without deleting its data or associations. For traces, open Filters and choose Archived; other lists have an archive-status filter. Restore the item from that list. Archive does not release pinned/saved or review retention protection, and does not reclaim disk space. Built-in templates remain available; archived custom templates are excluded from new-review pickers. Existing reviews keep template snapshots.

## Deletion scope

| Action | Removed | Preserved |
| --- | --- | --- |
| Delete traces and logs | Selected traces, their request logs, annotations and assignment records | Other traces, captures and reviews |
| Delete capture | Capture record | Original logs and trace membership |
| Delete review | Card, participating task associations, AI reports and this card's retention protection | Original traces, captures, logs and templates |
| Remove from review | One participating task association | Original task and existing report snapshots |
| Delete template | Custom template | Existing review and report snapshots |

Deletion is permanent. A preview shows affected item, log, task and report counts. Active recordings, analyses and recent unfinished requests must finish or stop first. Reviews referencing a trace or capture block source deletion; the preview links to those cards. Archive the source or resolve its references first. Historical report evidence remains a reference even after a current participating task is removed.

Batches are checked and applied atomically. One blocked item prevents the entire batch. Changes after the preview require reviewing the scope again.

## Manual expired-log cleanup

Use **Settings → Resource usage → Storage usage → Clean expired logs now**. Preview the retention cutoff, eligible logs/traces and retained count before confirming.

The background job reuses existing whole-trace retention rules, including pinned/saved logs, retained review evidence, active captures and recent unfinished requests. Retention set to zero disables this action. Preview tokens expire and configuration changes require a new preview. Protection is checked again during execution.

The page polls only while cleanup runs. Existing background garbage collection handles detached bodies; database and body-file usage may not shrink immediately. This action does not delete capture records, review cards, reports or templates. Existing automatic time-, size- and blob-based cleanup remains in effect. Deleting a card releases only that card's retention protection; other protections still apply.

## Trace filters

The trace toolbar contains a search field and one Filters button. It defaults to all unarchived history. Search applies after typing or immediately on Enter. Combine time range, entry, archive status, and error/pinned/to-review conditions in the popover; all selected conditions must match. The badge counts extra filters, excluding the search term. Clear filters restores default conditions while preserving your search.
