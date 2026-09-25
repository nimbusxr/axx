---
title: Check logs
description: Prove that something did not happen by asserting the log entry that says so, reading logs from files or receiving them over UDP, TCP or HTTP.
---

It is hard to prove that something did not happen: waiting and seeing nothing proves only that nothing arrived yet. Flip it to a positive. Have your service log its decision ("registration refused; not announced", "line ML-KES-0413-1 rejected: duplicate reference") and assert that entry. The `logs` steps read what your services log, whether it lands in a file or is sent over the network.

## Register a log

A log is where a service's lines are. Register it in the `Background`, with a `url`:

```gherkin
Background:
  Given the parcels log with the following properties:
    | url | udp://0.0.0.0:5140 |
  And the console log with the following properties:
    | url | file://.axx/logs/apps.log |
```

| url | Axx |
| --- | --- |
| `file:///var/log/app.log`, `file://logs/app.log` | reads what is appended to the file; a relative path is relative to `axx.yaml` |
| `udp://0.0.0.0:5140` | listens; each datagram is one or more lines |
| `tcp://0.0.0.0:5150` | listens; newline-delimited or octet-counted syslog (RFC 6587) messages |
| `http://0.0.0.0:5160/logs`, `https://0.0.0.0:5161/logs` | listens; the body of each POST or PUT to that path (https uses a self-signed certificate) |

A log only counts what it receives after the scenario registers it. Scenarios run in parallel and share logs, so match on data unique to the scenario: a reference, an order id.

For the network schemes Axx is the log service: your service, or the thing that ships its logs, sends lines to Axx. Axx opens those listeners before it starts the apps, for the log steps of the scenarios in the run, so nothing sent during startup is lost; `axx up` keeps them open between runs. Containers reach them at `host.docker.internal` (on Linux, add `extra_hosts: ["host.docker.internal:host-gateway"]` to the service).

## Point your logs at Axx

**The console of the apps Axx starts** needs nothing: Axx writes it to `.axx/logs/apps.log`, every compose container's lines prefixed with its name, so `file://.axx/logs/apps.log` reads it.

**A log file** a service writes, for example to a mounted volume:

```yaml title="infra/compose.yaml"
  app:
    volumes:
      - ./logs:/var/log/parcels
```

```gherkin
Given the parcels log with the following properties:
  | url | file://../infra/logs/app.log |
```

**Syslog over UDP or TCP.** An app's syslog handler (Python's `SysLogHandler`, logback's `SyslogAppender`) can send to `host.docker.internal:5140`, and so can Docker for any container's console, without changing the app:

```yaml title="infra/compose.yaml"
  app:
    logging:
      driver: syslog
      options:
        syslog-address: tcp://host.docker.internal:5150
```

The example's service sends every log line as a UDP datagram when `PARCELS_LOG_UDP` is set, which is what its `parcels` log receives.

**A log forwarder over HTTP**, such as Fluent Bit or Vector posting to Axx:

```ini title="infra/fluent-bit.conf"
[OUTPUT]
    Name   http
    Match  *
    Host   host.docker.internal
    Port   5160
    URI    /logs
    Format json_lines
```

## Assert entries

Patterns are regular expressions (Java syntax), searched in the log's text rather than matched against whole lines. Every step waits for its entries: 10 seconds, or the time `within` gives.

```gherkin
Then the parcels log has an entry matching 'msg="registration refused; not announced" reference=PX-EVT-4003'
Then within 30s the parcels log has an entry matching 'depot notified reference=PX-DSP-2001'
```

Several entries in one log; each row needs a match of its own, so the same pattern twice needs two matches:

```gherkin
Then the console log has entries matching:
  | msg="manifest line processed" line=ML-FJORD-0101-1 status=REJECTED reason="weight exceeds 30000 g" |
  | msg="manifest line processed" line=ML-FJORD-0101-2 status=REJECTED reason="unknown service level"  |
```

A number of matches, for example one per retry; more than that fails:

```gherkin
Then the parcels log has 2 entries matching 'msg="storing parcel failed, retrying" reference=PX-DBF-3002'
```

Entries in different logs, for example your service's decision and the call its dependency received:

```gherkin
Then the logs have entries matching:
  | parcels | msg="registration refused; not announced" reference=PX-ADR-1104 |
  | console | Request received:\n.*GET /v1/postcodes/DE/00012                  |
```

## Multi-line entries

Patterns match across the whole text, so an entry can span lines: a stack trace, or WireMock's request log above. `^` and `$` match at the start and end of each line, `\n` matches a line break, and `(?s)` lets `.` match line breaks too:

```gherkin
Then the parcels log has an entry matching '(?s)registration failed reference=PX-7.*IllegalStateException: address service unavailable'
```

In a table cell, Gherkin turns `\n` into a real line break, which matches a line break just the same.

When a step fails, it shows what the log received since the scenario registered it, so you can see what your service said instead.
