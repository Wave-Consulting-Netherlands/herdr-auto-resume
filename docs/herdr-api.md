# Herdr socket API notes

Phase 6 uses Herdr 0.7.5, protocol 17, over newline-delimited JSON on a Unix socket.
The default path is `~/.config/herdr/herdr.sock`; the watcher accepts an explicit
`--socket` path or that default and deliberately ignores all `HERDR_*` environment
variables.

## Request transport

Every ordinary request gets a fresh Unix connection. The client sends one request and
reads one response before closing:

```json
{"id":1,"method":"pane.read","params":{"pane_id":"w1:p1","source":"detection","lines":200,"strip_ansi":true}}
```

Responses echo the request id and use either `result` or `error`. `pane.read` returns
the text at `result.read.text`; `pane.list` and `session.snapshot` expose pane
`terminal_id`, which is the durable identity used by persistent jobs.

`events.subscribe` is the exception: the acknowledgement
`result.type == "subscription_started"` leaves that connection open for pushed frames:

```json
{"id":2,"method":"events.subscribe","params":{"subscriptions":[
  {"type":"pane.agent_status_changed","pane_id":"w1:p1"},
  {"type":"pane.output_matched","pane_id":"w1:p1","source":"detection",
   "match":{"type":"regex","value":"..."},"lines":200,"strip_ansi":true},
  {"type":"layout.updated"}
]}}
```

Pushed frames have no id: `{"event":"pane.output_matched","data":{...}}`.
The client accepts both dot and underscore event spellings, coalesces trigger events,
and retains pane-move/resync events while consumers are slow. Reconnect performs
`ping`, `session.snapshot`, resubscribe, then emits a resync event.

## Appendix A probe record

The following ground truth was recorded against the running 0.7.5 server on
2026-07-31 and is encoded by the fake Unix-socket harness and client tests:

- ordinary connections close after the first response; pipelining is reset;
- event subscriptions stay open after acknowledgement;
- `pane.output_matched` can fire immediately at subscribe time and did not refire
  during an 8-second persistently matching redraw;
- pane moves change public pane ids while preserving `terminal_id`;
- lifecycle frames may use dot or underscore event kinds.

P1 (output-match refire), P2 (live lifecycle envelope/data shapes), and P3
(Herdr-client detach/reattach) remain orchestrator-owned live probes. They must be
run against a real scratch server before changing the recycle policy or declaring the
live acceptance complete.

Reference: [Herdr Socket API](https://herdr.dev/docs/socket-api/).

## Probe results (2026-07-31, herdr 0.7.5, live)

- Request ids MUST be JSON strings; numeric ids → `invalid_request` with empty echoed id.
- Subscription event envelope confirmed `{"event":"<dot-form kind>","data":{…}}`.
- `pane.output_matched` fires at-subscribe when current window content matches and does
  NOT refire within the same subscription; a NEW subscription re-matches current content.
  Subscription recycling is therefore the primary re-arm mechanism.
- `events.subscribe` replays a burst of historical lifecycle events (created/layout for
  panes that may no longer exist) — treat as refresh triggers only, never as state.
- Detach/reattach of the interactive client: no effect observed on server-side
  subscriptions (soak will confirm long-idle behavior).
