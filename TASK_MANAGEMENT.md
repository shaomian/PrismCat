# Archive, delete and manual cleanup

Trace names, pin/favorite indicators and custom labels share the title area. Lists show two labels and a +N expansion; details show all labels. Click a label to filter exactly, or click the active label again to clear it; the trace ID, copy icon and source badges align on the second line. Hovering the left task information area or focusing it with the keyboard reveals the copy icon on desktop; touch devices keep it visible. Space remains reserved so text and badges never shift. The icon has a Copy Trace ID tooltip and briefly changes to a checkmark after success. The right-hand More menu contains edit, pin/unpin from top, favorite/unfavorite, archive/restore and delete, without duplicate copy or shortcut actions. Hover or keyboard focus reveals the menu entry on desktop; touch devices keep it visible. An open menu stays visible.

Review cards reveal edit, archive (or restore) and delete side by side on hover or keyboard focus; touch devices keep all three visible. Capture archive, restore and delete actions live in a single More menu, without a duplicate archive shortcut. Custom templates retain an edit shortcut and a management menu. Deletion still requires an impact preview and confirmation.

Click task content to open details. Holding over a name briefly previews existing summaries without loading full logs, diagrams or reports or calling AI. Active captures retain a recording indicator; Stop capture appears on hover and stays visible on touch devices.

Trace details display the name, ID, labels and notes, with contextual copy next to the ID and edit next to the name. Details omit the duplicate management menu. The editor contains name, labels and notes. Enter or English/Chinese commas add labels; each label can be removed. Duplicate/empty labels are removed and the existing 20-label limit applies. Save includes the last unfinished label draft. Whole merges live in the list menu or Batch manage; selected requests move or split through Request view → Organize requests. Explicit client traces support splitting only. Version conflicts preserve input and require cancelling and reopening to review the latest data.

Pinning to the top is independent of favoriting. Pinned tasks come first, newest pin first; ordinary tasks use their last request time. The server orders before pagination and still applies search and time filters. Top positions persist across refreshes and restarts. Pinning from the list returns to page one; unpinning restores ordinary ordering. Pinning does not protect logs from cleanup; favoriting retains the complete trace without changing its order. Archiving removes the top position and preserves the favorite. Restored tasks return to ordinary ordering and can be pinned again.

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

The trace toolbar contains a search field and one Filters button. It defaults to all unarchived history. Search matches names, IDs, labels and notes after typing or immediately on Enter. Combine time range, entry, archive status, labels, and error/pinned conditions in the popover; all selected conditions must match. The badge counts extra filters, excluding the search term. Clear filters restores default conditions while preserving your search.

Manual trace processing status is no longer used in the editor, details or the To review filter; stored legacy fields remain compatible. Custom labels remain distinct from read-only system badges such as Manually split. Review and clear the active exact label in Filters; all conditions must match.

## Log favorites

Use Favorite / Favorited in log details to mark requests worth revisiting, and filter logs by favorite status. Hover or focus the button for retention details: a favorite is protected from automatic cleanup, together with its entire trace when grouped. Other retention rules still apply after removing a favorite.

Notes and labels remain in the collapsible section. To do / Done buttons, list markers and filters have been removed. Existing records remain favorited; removing a favorite also clears the legacy review status without removing notes or labels.

## Trace list and preview

The list shows task, requests, upstreams, duration, tokens, and latest request. Latest request includes month, day, hour, and minute in the browser local timezone. Errors appear beside the task name only when present. Source badges, favorites, pinned tasks, labels, and the copy-ID action remain available.

Hover briefly or focus a task name with a desktop keyboard to see its full name, note excerpt, upstreams, and first/latest request timestamps including year and seconds. Empty notes are omitted and long upstream names wrap. Request counts, duration, total tokens, and labels are not repeated. Previews use only loaded list data; they never fetch bodies or call AI. Leaving the name or entering its actions closes the preview. Touch users can open details for this information.

Long trace IDs in the list show their leading and trailing characters; short IDs remain intact. This changes presentation only. Hover or focus an ID to read the full value; its adjacent copy action copies the original ID. IDs and source badges use content-based spacing instead of a fixed-width ID area. Navigation, search, grouping, and details continue using the original ID.
