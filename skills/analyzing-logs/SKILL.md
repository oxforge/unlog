---
name: analyzing-logs
description: Use when asked to analyze, triage, investigate or find errors in logs (log files of any size, directories of logs, archives, or log output piped from kubectl logs, docker compose logs, journalctl and the like), before grepping them or writing a script to read them
---

# Analyzing logs with unlog

unlog reads raw logs at disk speed and prints a short summary. Every distinct
error appears once, with when it started and stopped and how often it
occurred. The summary also shows what was deployed or restarted around the
errors, known failure chains in the order they happened, bursts, and which
services failed together. The same input always gives the same summary.

Run it before anything else. A grep or a script you write finds only what you
thought to look for, and it loses the order of events, the deployments and
the warnings that explain an error.

This skill needs unlog 0.4.0 or later.

## When not to use

- **A plain lookup the user asked for:** "show me the line with trace id
  7f3a9c", "how many 502s are in access.log". Grep answers it directly.
- **Logs unlog already summarized in this conversation:** work from that
  summary instead of running unlog again on the same input.

## 1. Check unlog

Run `unlog version`.

- **Not found:** tell the user that unlog isn't installed, give the install
  command, and ask before installing anything. Don't analyze the logs some
  other way in the meantime.
  - On macOS or Linux:
    `curl -fsSL https://raw.githubusercontent.com/oxforge/unlog/main/install.sh | sh`
    (installs to `~/.local/bin`).
  - With Go 1.27 or later: `go install github.com/oxforge/unlog@latest`.
- **A release older than 0.4.0**, such as `unlog 0.3.0`: say so and suggest
  upgrading with the same command. Then carry on, warning that its output may
  not match [reading-the-summary.md](reading-the-summary.md).
- **0.4.0 or newer, `dev`, or a version built from a checkout**, such as
  `unlog 0.4.1-0.20261101093000-1a2b3c4d5e6f`: go ahead.

## 2. Run unlog on everything

Give unlog every path the user pointed at, as they are. That includes files,
directories (searched recursively, rotated and compressed logs included), and
`.gz` and `.tar.gz` archives:

```bash
unlog /var/log/app/ worker.log
```

Logs that come from a command go straight in through a pipe. Don't save them
to a file first:

```bash
kubectl logs deployment/api --since=1h | unlog -
journalctl -u nginx --since "2024-01-15 10:00" | unlog -
```

- **Don't grep, filter, sample or `head` the input first.** unlog handles the
  volume, and filtering first removes the deployments, warnings and ordering
  the summary is built from.
- **Output:** the summary goes to stdout, and a few filter stats to stderr.
  When stdout isn't a terminal, the output has no color.
- **`--level`** is `warn` by default.
  - Use `--level info` or `--level debug` when the errors need more of what
    led up to them.
  - Use `--level error` when warnings drown the errors.
  - Deployment lines are kept at any level.
- **`--since` and `--until`** take a time (`2024-01-15T14:00:00Z`) or an age
  (`2h`, `30m`).
- **`--tz Europe/Berlin`** sets the zone of timestamps that carry none. The
  default is UTC.
- **Exit code 1** means there is no summary. Read the error: it names the path
  or flag that's wrong. `-v` lists the files that were skipped and why.

## 3. Read the summary

[reading-the-summary.md](reading-the-summary.md) explains every section, line
and tag. Read it before you interpret your first summary.

## 4. Look further only when the summary raises a question

The summary is capped in size and merges repeats. Look at the raw logs when it
points at something it can't settle by itself:

- **grep the raw files** for a `trace=` id, an exact message, or the minutes
  around the first error;
- **re-run unlog** with `--since` and `--until` around the incident to see
  more of it, or with `--level info` to see the lines leading up to it;
- **read the code or config** of the service that failed first, when the
  question is why it failed.

Logs that came from a command leave no file to grep. Run the command again with
absolute time bounds taken from the summary, and pipe it into grep or unlog:

```bash
kubectl logs deployment/api --since-time=2024-01-15T14:00:00Z | grep 7f3a9c
journalctl -u nginx --since "2024-01-15 14:00" --until "2024-01-15 14:10" | unlog -
docker compose logs --since 2024-01-15T14:00:00Z --until 2024-01-15T14:10:00Z api | unlog -
```

Tell the user that the second fetch may differ from the first: logs rotate,
and a pod that restarted may have lost its earlier logs.

Quote the raw lines you find when they back a fact.

## 5. Answer

- **The facts, always.** Say what happened, in time order:
  - which errors, in which services, and how often;
  - what was deployed or restarted around them;
  - which failures started together.

  Every fact comes from the summary or from a raw line you can quote.
- **A conclusion, only when the evidence supports one.** State it together with
  what it rests on. That evidence is the order of events, not their volume:
  the cause comes first and the effects follow it. Examples are a chain whose
  steps occurred in order, a deploy just before the first error, or the first
  service to fail with the others following. The loudest error isn't the
  cause just because it's the loudest.
- **Otherwise, just the facts.** Say plainly that the logs don't settle the
  cause. Don't guess.
- **When a conclusion depends on something outside the logs,** such as code,
  config or infrastructure, say so.
- **No fixes or recommendations unless the user asks for them.**
- **Size the answer to the question.** "Any errors in this file?" gets a few
  lines; an outage gets a short timeline and a conclusion.
