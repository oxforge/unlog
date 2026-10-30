# Reading an unlog summary

The summary has six sections, always in this order, followed by filter stats
on stderr:

1. Incident Overview
2. Deployments
3. Critical Errors
4. Error Chains
5. Rate Anomalies
6. Context

The summary has a fixed size budget, split between the sections. Each section
keeps its highest-ranked lines and prints them in time order. Severity raises a line's rank, and so do being in a
burst, in a chain or a deployment, failing together with other services, and
repeating. A section with nothing in it says `(none)`.

## One line

```text
2024-01-15T14:02:15Z–14:02:20Z [ERROR] deploy.log[api]: pod app-pod-2 failed readiness probe: HTTP 503 {×2 http=503 with=app.log,app.log[app-pod]}
```

One line stands for every repeat of one error from one source and service.

- **`2024-01-15T14:02:15Z–14:02:20Z`:** the first and last occurrence, in UTC.
  - The end shows only the time when it falls on the same day, and the full
    date otherwise.
  - It is left out when the error occurred once.
  - `-` means the log lines carried no timestamp.
- **`[ERROR]`:** the level: `FATAL`, `ERROR`, `WARN`, `INFO`, `DEBUG` or
  `TRACE`, and `UNKNOWN` when the source has no levels. An error that repeated
  at several levels shows the highest.
- **`deploy.log[api]`:** where it came from.
  - The source is a file path, or `stdin`.
  - The part in brackets is the service the line names, through a `service`,
    `app`, `container` or `pod` field, or a Docker Compose or Kubernetes
    prefix.
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
| `http=503` | the HTTP status in the message |
| `err=TYPE` | the error type named in the message |
| `trace=ID` | the trace or request id in the message; grep the raw logs for it |
| `with=A,B,+N` | other sources or services whose errors started within 5 seconds of this one, before or after: failures that happened together. It names two, then counts the rest |

## Incident Overview

- **`Window:`** the first and last timestamp in the logs, and how long that
  is. It says `unknown (no timestamps)` when no line had a timestamp.
- **`Sources:`** how many inputs, with the first five names.
- **`Events:`** occurrences of FATAL, ERROR and WARN lines. `(N distinct)`
  appears when repeats were merged: `13 error (8 distinct)` means 8 different
  errors that occurred 13 times in all.
- **`Last deployment:`** the latest deployment line at or before the first
  error, and how long before that error it was. See the limits at the end.
- **`Error chains detected:`** the names of the chains under Error Chains.
- **`Bursts:`** how many bursts are listed under Rate Anomalies.

## Deployments

Every deployment, restart, scaling and rollback line, at any level, in time
order. Read it as "what changed before things broke". A deployment line at
ERROR level, such as a rollback or a restart after a crash, also appears under
Critical Errors. Different versions stay on separate lines.

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
UNKNOWN lines when a source has no levels. They are the background to judge
the errors against, so check whether any of them began before the first
error.

## Filter stats (stderr)

- **`Ingested`:** lines read.
- **`Dropped`:** lines filtered out, or merged into another line.
- **`Survived`:** the distinct lines kept.
- **`Unique signatures`:** the distinct message groups.

`Ingested = Dropped + Survived`. `-v` adds a breakdown by filter and per file.

## Limits that affect conclusions

- **A merged line keeps only its first and last time.** When it happened in
  between, and how often at each moment, isn't visible: `×2` from 10:00 to
  23:00 looks like steady repeats. For the same reason, `Last deployment:`
  can date a deployment message that kept repeating past the first error by
  its first occurrence. Grep the raw logs for exact times.
- **The summary has a fixed size.** A section shows its highest-ranked lines
  and says `... (N more entries omitted)` or `… (N more lines)` for the rest,
  so a missing line doesn't prove that something didn't happen. Re-run unlog
  with `--since` and `--until` to zoom in, or grep the raw logs.
- **A message is one example of its group.** Messages are grouped after their
  UUIDs, IPs, timestamps and paths are normalized.
- **Every time is UTC.** Timestamps that carry no zone are read as UTC unless
  `--tz` names another zone.
