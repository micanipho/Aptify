# ADR-0005: SSE, with replay from the event log

**Status:** Accepted · 2026-09-05 · Revised 2026-09-26 (multi-instance fanout)

## Context

Users watch a multi-minute run. If it does not visibly move, they leave. If a refresh
loses the run, they stop trusting the progress view entirely — which makes every later
investment in it worthless.

## Decision

Server-Sent Events, not WebSockets. The stream is one-directional, it works through
proxies, the browser reconnects natively, and there is no second protocol to operate.
Cancellation goes over an ordinary POST.

Each run event carries its gapless sequence number as the SSE event id. On reconnect the
browser sends `Last-Event-ID` and the server replays from the log before subscribing to
live events.

The ordering matters: **subscribe first, then replay**, dropping anything at or below
the last id already sent. Replaying first would lose an event that landed during the
replay; not deduplicating would send it twice.

## Consequences

- A refresh mid-run resumes rather than restarting. This is the entire point.
- The event log is load-bearing for the UI as well as for correctness, which gives the
  append-only rule a consequence a reviewer can feel rather than a principle they have
  to take on faith.
- Past one backend instance, the stream tails the event log (a Postgres `LISTEN`/`NOTIFY`
  wake-up, then a read after the last `seq`) rather than adding Redis as a second transport
  that can disagree with the log. The bus is never authoritative, so the swap changes no
  semantics.
- In Go this is stdlib `net/http` with `http.Flusher`; the handler must flush after every
  frame and on every heartbeat, or buffering defeats the stream.
- Heartbeat comment frames every 15 seconds stop intermediaries closing an idle stream
  during a long install, and `x-accel-buffering: no` stops proxies buffering it into
  uselessness.
