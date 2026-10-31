# Reading an unlog summary

The summary has six sections, always in this order. With the default text
output, filter stats follow on stderr:

1. Overview
2. Deployments
3. Critical Errors
4. Error Chains
5. Rate Anomalies
6. Context

The summary has a fixed size budget, split between the sections. Each section
keeps its highest-ranked lines and prints them in time order. Severity raises
a line's rank, and so do being in a burst, in a chain or a deployment, failing
together with other services, and repeating. A section with nothing in it says
`(none)`.

The summary describes the lines that survived the filters: by default,
WARN and above, plus deployment lines at any level. Lines below `--level`
are read and counted, but nothing else about them appears.

## One line

```text
2024-01-15T14:02:20Z–14:02:21Z [ERROR] app.log: request timeout: upstream unavailable {×2 http=503 with=deploy.log}
```

One line stands for every repeat of one error from one source and service.

- **`2024-01-15T14:02:20Z–14:02:21Z`:** the first and last occurrence, in UTC.
  - The end shows only the time when it falls on the same day, and the full
    date otherwise.
  - It is left out when the error occurred once.
  - `-` means the log lines carried no timestamp.
- **`[ERROR]`:** the level: `FATAL`, `ERROR`, `WARN`, `INFO`, `DEBUG` or
  `TRACE`. `UNKNOWN` means unlog couldn't read a level from the line, either
  because the source has none or because it uses one unlog doesn't know (such
  as Postgres `LOG:`). An error that repeated at several levels shows the
  highest.
- **`app.log`:** where it came from.
  - The source is a file path, or `stdin`.
  - A service the line names follows in brackets, as in `app.log[app-pod]`.
    It comes from a `service`, `app`, `container` or `pod` field, or from a
    Docker Compose or Kubernetes prefix.
  - Replicas share their service: `web-1` and `web-2` are `web`, and pod
    `api-7d9f8b6c4-x2k4p` is `api`.
- **The message:** the text of the first occurrence.
  - UUIDs, IPs, timestamps, paths and similar variable parts are normalized to
    group the repeats, so later occurrences may differ in those parts.
  - A very long message ends with `…[N more bytes]`.

The tags in `{…}` appear in this order:

| Tag | Means |
|---|---|
| `deploy` | a deployment, restart, scaling or rollback line |
| `spike` | some of its occurrences fell inside a burst (see Rate Anomalies) |
| `×N` | it occurred N times between the first and last time |
| `http=503` | the HTTP status, from a field such as `status` or from the message |
| `err=TYPE` | the error type, from a field such as `error` or `exception`, or else the first word after `Error:`, `Exception:`, `FATAL:`, `Panic:` or `Caused by:` in the message; often a guess, see the limits at the end |
| `trace=ID` | the trace or request id, from a field such as `trace_id` or `request_id` or from the message; grep the raw logs for it |
| `with=A,B,+N` | other sources or services whose errors started within 5 seconds of this one, before or after: failures that happened together. It names two, then counts the rest |

## Overview

- **`Window:`** the first and last time of the lines in this summary, and how
  long that is. It isn't the time range of the input: logs running from 08:00
  to 18:00 with one error at 10:00 give `10:00:00Z — 10:00:00Z (0s)`. It says
  `unknown (no timestamps)` when no kept line had a timestamp.
- **`Sources:`** how many sources have a line in this summary, with the first
  five names. A file with nothing above `--level` isn't counted; `-v` lists
  every file that was read.
- **`Events:`** occurrences of FATAL, ERROR and WARN lines. `(N distinct)`
  appears when repeats were merged: `13 error (8 distinct)` means 8 different
  errors that occurred 13 times in all.
- **`Last deployment:`** the latest deployment line at or before the first
  error, and how long before that error it was. See the limits at the end.
- **`Error chains detected:`** the names of the chains under Error Chains.
- **`Bursts:`** how many bursts were found. Rate Anomalies may list fewer when
  they don't fit, and then says how many it left out.

## Deployments

Deployment, restart, scaling and rollback lines, at any level, in time order,
as many as fit. Read it as "what changed before things broke". A deployment
line at ERROR level, such as a rollback or a restart after a crash, also
appears under Critical Errors. Different versions stay on separate lines.

## Critical Errors

FATAL and ERROR lines that aren't part of a chain.

## Error Chains

A chain is a known failure pattern with ordered steps. db-connection-exhaustion,
for example, runs from deadlocks or slow queries, to an exhausted connection
pool, to failing queries. The built-in patterns are:

- db-connection-exhaustion, oom-cascade, deployment-failure, circuit-breaker,
  cascade-failure;
- disk-full, certificate-expiry, dns-failure, rate-limiting, queue-backlog.

A chain is reported when two different errors matched two of its steps in
order. Steps may be skipped, and an error can belong to more than one chain.

```text
db-connection-exhaustion: 3 of 3 steps, 2025-06-15T14:37:30Z–14:41:12Z
  1. 2025-06-15T14:37:30Z–16:55:39Z [WARN] app.log[orders]: deadlock detected on table orders {spike ×439} (also from 5 other sources)
  2. 2025-06-15T14:37:30Z–16:55:39Z [ERROR] app.log[orders]: connection pool exhausted, 0 available connections {×304} (also from 3 other sources)
  3. 2025-06-15T14:37:31Z–16:55:39Z [ERROR] app.log[orders]: query failed: cannot execute SELECT on orders {×373}
```

- **The header:** the pattern's name, how many of its steps occurred, and the
  span from the chain's first line to the time it last advanced. A suffix
  such as `-2` marks a separate, later instance of the same pattern.
- **Each line** starts with the number of the step it matched, then reads like
  any other line.
- **Lines are in time order,** so `1, 3, 2` means step 3 came before step 2
  in these logs. That order is itself a finding.
- **`(also from N other sources)`:** the same error, at this step, from N more
  sources or services, merged into this line. Its `×N` counts all of them.
- **`… and N more at step K`:** step K had more errors than the summary lists,
  which is three per step.
- **`(N entries not shown)`:** added to a header when the chain didn't fit
  the budget. Re-run unlog with `--since` and `--until` around its span to see
  its errors.

## Rate Anomalies

Bursts are stretches where one source or service logged far above its normal
rate.

```text
2025-06-15T14:48:18Z–14:48:30Z app.log[notification-service]: 22 events in 13s (normally quiet)
  starting application on port 8080 {×8}
  Error: request processing failed with status=500 {×6}
```

- **The header** gives the span, the origin, the events in the burst and its
  length. It ends with the normal rate: `(normally quiet)`, or
  `(normal ~0.4/s, 12× above)`.
- **The indented lines** are up to three messages that made up the burst, with
  their counts inside it.

Errors that fell inside a burst carry `spike` everywhere else in the summary.

## Context

Lines below ERROR that are neither a deployment nor in a chain. That means
warnings by default, plus INFO and DEBUG lines when `--level` is lower, and
UNKNOWN lines, whose level unlog couldn't read. They are the background to judge
the errors against, so check whether any of them began before the first
error.

## Filter stats (stderr)

- **`Ingested`:** log entries read; a multi-line entry such as a stack trace
  counts once.
- **`Dropped`:** entries filtered out, or merged into another line.
- **`Survived`:** the distinct lines kept.
- **`Unique signatures`:** the distinct message groups.

`Ingested = Dropped + Survived`. `-v` adds a breakdown by filter and per file.

## Limits that affect conclusions

- **A merged line keeps only its first and last time.** When it happened in
  between, and how often at each moment, isn't visible: `×400` from 10:00 to
  23:00 may be a steady trickle or two bursts. For the same reason,
  `Last deployment:` can date a deployment message that kept repeating past
  the first error by its first occurrence. Grep the raw logs for exact times.
- **The summary has a fixed size.** A section shows its highest-ranked lines
  and says `... (N more entries omitted)` or `… (N more lines)` for the rest,
  so a missing line doesn't prove that something didn't happen. Re-run unlog
  with `--since` and `--until` to zoom in, or grep the raw logs.
- **A message is one example of its group.** Messages are grouped after their
  UUIDs, IPs, timestamps and paths are normalized.
- **`err=` is often a guess.** Without an error field, unlog takes the first
  word after `Error:` or `FATAL:`, so it can be a plain word: Postgres
  `FATAL:  out of memory` gives `err=out`. Read the message rather than
  relying on the tag.
- **Every time is UTC.** Timestamps that carry no zone are read as UTC unless
  `--tz` names another zone.
