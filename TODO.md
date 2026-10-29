# TODO

## Pending

| Feature slug | Feature description |
| --- | --- |
| anchor-yearless-syslog | **Today:** RFC 3164 syslog lines carry no year (`Jan 15 10:29:55 db-01 postgres[1234]: ...`). `ingest/parser_syslog.go` takes the year from a reference date: the file's mtime (the archive entry's or gzip header's time, or now for stdin), stepping back a year when the result would be more than a day after it. When such a file is analyzed together with dated logs from a previous year (logs copied from an outage last December), its entries land in the wrong year and no longer line up with the other sources for auto-window, correlation and the timeline. **Wanted:** when a run mixes year-less and dated sources, year-less timestamps take the year that puts them closest to the dated sources' time range; mtime stays the fallback when nothing is dated. **Decided:** the mtime reference shipped first (review finding H3); this anchoring needs a fix-up pass after ingest and before the filter's time window and dedup, so it was deferred rather than done with H3. |
| error-type-from-level-prefix | **Today:** `enrich/fields.go` extracts `ErrorType` with `reErrorType`, whose prefixes include `Error:` and `FATAL:`. Postgres writes its level the same way (`FATAL:  out of memory: ...`), so the summary tags such entries `{err=out}` or `{err=the}`: the first word of the message, not an error type (visible on `testdata/incidents/db_connection/db.log`). **Wanted:** a level prefix at the start of a message is not read as an exception type; `err=` appears only for real type names (`java.lang.NullPointerException`, `ValueError`, `ECONNREFUSED`). **Decided:** nothing yet; found while fixing review finding H3. |

## Done

| Feature slug | Feature short description | Commit |
| --- | --- | --- |
