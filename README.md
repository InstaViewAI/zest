# Zest

An HTTP service that keeps a Slack channel in sync with escalated Zendesk
tickets. When a support agent escalates a ticket, engineering gets a Slack
message with full context; every comment and status change afterwards lands as
a reply in that same thread; and anything left untouched for a week is rolled
up into a scheduled digest.

It replaces a five-Zap Zapier build ("Zendesk to slack integration") with one Go
service.

**Contents**

- [How it works](#how-it-works)
- [The join key](#the-join-key) — the one idea everything rests on
- [The four flows](#the-four-flows)
- [Architecture](#architecture)
- [Project layout](#project-layout)
- [Configuration](#configuration)
- [Running and testing](#running-and-testing)
- [Zendesk and Slack setup](#zendesk-and-slack-setup)
- [API reference](#api-reference)
- [Extending it](#extending-it)

---

## How it works

Three of the four flows are **inbound webhooks**: Zendesk triggers call this
service, and it calls Slack. The fourth is a **cron job** inside the service
that calls both.

```
   an agent escalates                                    engineering sees
   a ticket in Zendesk                                   it in Slack
          │                                                     ▲
          ▼                                                     │
   ┌─────────────┐   webhook   ┌──────────────┐   chat.post  ┌──┴──────┐
   │   Zendesk   │────────────►│ ZEST service │─────────────►│  Slack  │
   │             │◄────────────│              │              │         │
   └─────────────┘  write back └──────┬───────┘              └─────────┘
                    the thread        │  ▲
                    pointer           │  │ every Sat 01:30
                                      └──┘ internal cron
```

The service holds **no state of its own** — no database, no cache. Everything it
needs to remember lives on the Zendesk ticket. That is the join key.

---

## The join key

Slack has no concept of a Zendesk ticket. When a comment arrives three days
after the alert, the service must answer one question: *which Slack message do I
reply under?*

Slack only tells you a message's identifier (`ts`) in the response to the call
that created it. That value exists for one instant, in memory. If you don't
store it, it is gone.

So two custom fields on the ticket hold it:

| Field | Holds | Example |
| --- | --- | --- |
| `slack_thread_ts` | timestamp of the parent Slack message | `1725200000.000100` |
| `slack_channel_id` | the channel the thread lives in | `C0123456789` |

Flow 1 writes both the moment the thread is created. Flows 2 and 3 read them
back. **Zendesk is the source of truth; Slack is the view.**

### Field *id* vs field *value*

Zendesk's API addresses custom fields by **numeric id**, never by name — you
cannot send `{"slack_thread_ts": "..."}`. This trips people up, so to be
explicit:

```
                       field id 17283910      field id 17283911
                       (slack_thread_ts)      (slack_channel_id)
        ticket #4242 → 1725200000.000100      C0123456789
        ticket #4243 → 1725209999.000200      C0123456789
        ticket #4244 → 1725300123.000700      C0123456789
                       ^^^^^^^^^^^^^^^^^
                       different per ticket
```

The **id** is the column. It is assigned once when you create the field in
Zendesk admin and never changes. The **value** is the cell — different for every
ticket, written at runtime, never configuration.

Because those ids differ between Zendesk accounts (your sandbox will not match
production), they can't be hardcoded. The service resolves them **by name at
boot** and logs what it found:

```
resolved Zendesk field "slack_thread_ts" to id 17283910
resolved Zendesk field "slack_channel_id" to id 17283911
```

If a field is missing or renamed, startup fails with the name it couldn't find.
Nothing about this needs configuring unless you renamed the fields.

---

## The four flows

### Flow 1 — Escalation (writes the pointer)

The entry point. Runs **once per ticket**; every other flow depends on it having
run.

```
 Zendesk                      ZEST service                       Slack
    │                              │                               │
    │ 1  POST /webhooks/zendesk/escalation                         │
    │─────────────────────────────►│                               │
    │    ticket_id, subject, …     │                               │
    │                              │ 2  chat.postMessage           │
    │                              │    (no thread_ts)             │
    │                              │──────────────────────────────►│
    │                              │ 3  ts + channel               │
    │                              │◄──────────────────────────────│
    │ 4  PUT /tickets/4242.json    │                               │
    │◄─────────────────────────────│                               │
    │    additional_tags: escalated                                │
    │    custom_fields[17283910] = ts                              │
    │ 5  200 {"thread_ts": …}      │                               │
    │◄─────────────────────────────│                               │
```

Step 3 is the only moment the timestamp exists anywhere. Step 4 is what makes it
survive. If step 4 fails, the service returns an **error** rather than a success
so Zendesk retries — otherwise the thread would be orphaned and no later update
could ever reach it.

### Flow 2 — Comment sync (reads the pointer)

Runs every time anyone comments on an escalated ticket.

```
 Zendesk                      ZEST service                       Slack
    │                              │                               │
    │ 1  POST /webhooks/zendesk/comment                            │
    │─────────────────────────────►│                               │
    │    ticket_id, comment, author│                               │
    │                              │                               │
    │                    ┌─────────┴──────────┐                    │
    │                    │ which thread?      │                    │
    │                    │ ts on payload → use│                    │
    │                    │ otherwise  → step 2│                    │
    │                    └─────────┬──────────┘                    │
    │ 2  GET /tickets/4243.json    │                               │
    │◄─────────────────────────────│                               │
    │    custom_fields[17283910]   │                               │
    │─────────────────────────────►│                               │
    │                              │ 3  chat.postMessage           │
    │                              │    + thread_ts  ◄── the point │
    │                              │──────────────────────────────►│
```

Step 2 is skipped when the Zendesk trigger maps the pointer onto the payload —
purely an optimisation that saves one API call.

**A ticket with no pointer is skipped, not failed.** The service replies
`{"skipped": true}` and posts nothing. That covers tickets tagged before this
system existed.

### Flow 3 — Status sync

Mechanically identical to flow 2 — same thread resolution, same skip behaviour.
Only the trigger condition and the rendered text differ.

| | Flow 2 · comment | Flow 3 · status |
| --- | --- | --- |
| Endpoint | `/webhooks/zendesk/comment` | `/webhooks/zendesk/status` |
| Fires on | comment is present | status changed |
| Required fields | `ticket_id`, `comment` | `ticket_id`, `status` |
| Posts | 💬 New Comment from … | ✅ Status Updated to: solved *(was pending)* |

### Flow 4/5 — Stale digest (never touches the pointer)

The odd one out: nothing calls it. It wakes itself on a schedule and finds
tickets by **tag**, not by thread.

```
      ┌── cron · Sat 01:30 ──┐
      ▼                      │
 ZEST service                │              Zendesk              Slack
      │  1  GET /search.json │                 │                   │
      │────────────────────────────────────────►│                   │
      │     type:ticket tags:escalated updated<cutoff               │
      │◄────────────────────────────────────────│                   │
      │  2  group by status, oldest first       │                   │
      │  3  chat.postMessage (no thread_ts) ────────────────────────►│
```

The cutoff is an absolute timestamp computed in Go (`updated<2026-08-25T…`), not
Zapier's relative `updated>168hours` — that syntax is a Zapier-ism, not Zendesk
search. Zero results still posts an all-clear, so a broken schedule looks
different from a quiet week.

Weekly is on by default; monthly ships off, matching the Zapier build.

### All four side by side

| Flow | Started by | Thread pointer | Slack call | Replaces |
| --- | --- | --- | --- | --- |
| **1** Escalation | Zendesk webhook | **writes it** | parent message | Zap 1 |
| **2** Comment | Zendesk webhook | reads it | threaded reply | Zap 2 |
| **3** Status | Zendesk webhook | reads it | threaded reply | Zap 3 |
| **4/5** Digest | internal cron | never touches it | channel message | Zaps 4, 5 |

> **Debugging note.** Because flows 2 and 3 *skip* rather than fail, a broken
> flow 1 shows up as **silence in Slack, not errors in a log**. If comments stop
> appearing, check that flow 1 wrote the custom fields before suspecting flow 2.

---

## Architecture

Four layers, each depending only on the one below it:

```
  ┌──────────────────────────────────────────────────────────────┐
  │ cmd/server            HTTPServer, *http.Server, graceful     │
  │                       shutdown, route registration           │
  ├──────────────────────────────────────────────────────────────┤
  │ pkg/middleware        Zendesk webhook signature verification │
  ├──────────────────────────────────────────────────────────────┤
  │ pkg/api/handlers      bind JSON → call service → render JSON │
  ├──────────────────────────────────────────────────────────────┤
  │ pkg/application/      escalation: the four flows             │
  │                       slack, zendesk: API clients            │
  └───────────────┬──────────────────────────┬───────────────────┘
                  ▼                          ▼
             Slack Web API             Zendesk Support API
```

The rules that keep the layers honest:

- **Handlers never build outbound HTTP requests.** They bind, call one service
  method, and render. Every handler is under 25 lines.
- **Services never touch `*gin.Context`.** They take a `context.Context` and
  plain arguments, so they are testable against a stub server with no HTTP
  machinery.
- **Errors carry their own status.** A service returns `*contracts.Error` with
  the HTTP status the API should surface; `respondError` maps it. That is why
  a Zendesk 404 surfaces as a 404 and an unreachable Slack surfaces as a 502.

`pkg/infrastructure/` holds things that are neither business logic nor
transport: configuration and the cron scheduler.

### Request path, end to end

```
  POST /api/v1/webhooks/zendesk/comment
        │
        ├─ middleware.VerifyZendeskSignature   HMAC check, restores the body
        │
        ├─ handlers.EscalationHandler.Comment  ShouldBindJSON → contracts.CommentWebhook
        │
        ├─ escalation.Service.HandleComment    resolve thread, build the message
        │      ├─ zendesk.Service.GetTicket    (only if the payload lacks the pointer)
        │      └─ slack.Service.PostMessage    thread_ts set
        │
        └─ 200 {"message": "...", "skipped": false}
```

---

## Project layout

```
cmd/
  main.go                    bootstrap: config → server → routes → run
  server/
    server.go                HTTPServer, *http.Server, graceful shutdown
    routes.go                route groups under BasePath
    webhook_routes.go        the three Zendesk webhooks + report trigger
    time_routes.go           per-domain route registration
    slack_routes.go
    zendesk_routes.go
    utils.go
pkg/
  api/handlers/              gin handlers, and the Handlers/Services wiring
  middleware/                Zendesk webhook signature verification
  application/
    escalation/              the four flows + the digest builder
    slack/                   Slack Web API client
    zendesk/                 Zendesk Support API client
  contracts/                 request/response DTOs, shared error type
  common/                    shared route-param names
  infrastructure/
    config/                  viper yaml config + secret loader, validated at boot
    scheduler/               cron runner for the recurring reports
scripts/
  stub/                      fake Slack + Zendesk for local testing
  e2e.sh                     drives all four flows against the stubs
```

---

## Configuration

Configuration follows the same layout as atlas: a config file plus a secret
file per environment, read with viper and merged at boot (secret values win).

```
config/
  config.local.yaml          local, non-secret values (committed)
  secret.local.sample.yaml   copy to secret.local.yaml (gitignored) and fill in
  config.dev.yaml  secret.dev.yaml
  config.stg.yaml  secret.stg.yaml
```

The process reads three environment variables:

| Variable | Notes |
| --- | --- |
| `ENVIRONMENT` | `local`/`dev` enable gin debug mode |
| `CONFIG_FILE_PATH` | e.g. `config/config.dev.yaml` |
| `SECRET_FILE_PATH` | e.g. `config/secret.dev.yaml`; optional |

In deployed environments `secret.<env>.yaml` holds `$VAR` placeholders that the
deploy substitutes, as in atlas; values written `"read from secret"` in a config
file are overwritten by the secret file.

Validation runs at boot, and the service will not start if a required key is
missing, so the problem shows up at startup rather than during an escalation.
Errors name the yaml key, e.g. `slack.channel_id`.

### Required keys

| Key | Notes |
| --- | --- |
| `slack.token` | `xoxb-…`. Needs scopes `chat:write` **and** `chat:write.customize` |
| `slack.channel_id` | A `C…` id, **not** a `#name` — a leading `#` is rejected at boot |
| `zendesk.subdomain` | e.g. `instaviewhomesupport` |
| `zendesk.email` | the agent account owning the API token |
| `zendesk.api_token` | Admin Center → Apps and integrations → Zendesk API |
| `escalation.thread_ts_field_id` | numeric id of the `slack_thread_ts` custom field |
| `escalation.channel_id_field_id` | numeric id of the `slack_channel_id` custom field |

### Optional keys

| Key | Default | Notes |
| --- | --- | --- |
| `server.name` | `zest` | |
| `server.host` | `""` | all interfaces |
| `server.port` | `8080` | |
| `server.read_timeout` | `10s` | |
| `server.write_timeout` | `30s` | |
| `server.shutdown_timeout` | `5s` | |
| `slack.base_url` | `https://slack.com/api` | override for a stub |
| `slack.timeout` | `10s` | |
| `slack.bot_name` | `ZEST` | display name on alerts and replies |
| `slack.report_bot_name` | `ZEST - Report` | display name on digests |
| `slack.icon_url` | `""` | bot avatar |
| `zendesk.base_url` | derived from subdomain | override for sandbox/stub |
| `zendesk.timeout` | `10s` | |
| `zendesk.webhook_secret` | `""` | **empty disables signature verification** |
| `escalation.tag` | `escalated` | written by flow 1, searched by flows 4/5 |
| `report.stale_after` | `168h` | how long before a ticket counts as stale |
| `report.timezone` | `Local` | e.g. `Asia/Manila`; validated at boot |
| `report.weekly_enabled` | `true` | |
| `report.weekly_cron` | `30 1 * * 6` | Saturday 01:30 |
| `report.monthly_enabled` | `false` | built but off, matching Zap 5 |
| `report.monthly_cron` | `30 1 1 * *` | |

---

## Running and testing

### No credentials needed

```bash
make e2e
```

Boots stub Slack and Zendesk upstreams, runs the service against them, fires all
four flows, and prints the exact payloads that would have gone upstream —
including the rendered Slack message and the ticket write-back. `make stub` runs
the fakes alone on `:8777` if you want to poke by hand.

### With real credentials

```bash
cp config/secret.local.sample.yaml config/secret.local.yaml   # then fill it in
make run
```

Test in three stages so a failure tells you which side is wrong:

1. **Slack alone** — `curl` Slack's `chat.postMessage` directly with your token
   and channel id. Rules out scopes and channel membership.
2. **Real Slack, stub Zendesk** — set the real `slack.*` keys but keep
   `zendesk.base_url: http://127.0.0.1:8777/zendesk` and `make stub`. Messages
   land in a real channel; no ticket is touched.
3. **Both real** — use a throwaway ticket. Escalating a real ticket overwrites
   its pointer and detaches it from any existing thread.

Keep `report.weekly_enabled: false` while testing (the local config does) and use
`POST /api/v1/reports/stale` to run the digest on demand.

### Other make targets

| Target | Does |
| --- | --- |
| `build` / `clean` / `start` | binary at `./cmd/zest.o` |
| `run` | `go run ./cmd` with `config/config.local.yaml` + `config/secret.local.yaml` |
| `fmt` / `vet` / `lint` / `test` / `tidy` | individually |
| `stub` / `e2e` | local upstream fakes and the flow drive |
| `validate-push` | tidy → fmt → vet → lint → test → build → clean |

---

## Zendesk and Slack setup

### Slack

1. Create an app at [api.slack.com/apps](https://api.slack.com/apps).
2. Add bot scopes **`chat:write`** and **`chat:write.customize`**, then install
   to the workspace and copy the `xoxb-…` token.
   *Without the second scope every post fails* — the service always sends a
   `username`, and Slack rejects that with `invalid_scope` rather than ignoring
   it.
3. `/invite @ZEST` into the channel. A bot that isn't a member gets
   `not_in_channel` on every call.
4. Right-click the channel → Copy link → the `C…` at the end is
   `slack.channel_id`.

### Zendesk

1. **Custom fields** — Admin Center → Objects and rules → Tickets → Fields →
   create `slack_thread_ts` and `slack_channel_id` as **Text**. Copy each
   field's numeric id into `escalation.thread_ts_field_id` and
   `escalation.channel_id_field_id`. Make them agent read-only: a hand-edited
   pointer silently detaches the ticket from its thread.
2. **Webhooks** — Admin Center → Apps and integrations → Webhooks → create three
   (POST, JSON, auth None) pointing at the three webhook endpoints below, and
   copy the signing secret into `zendesk.webhook_secret` (secret file).
3. **Triggers** — one per webhook, with a JSON body mapping onto the structs in
   `pkg/contracts/escalation.go`.

**Trigger conditions — two loop hazards worth reading twice:**

- **Never fire the escalation trigger on the `escalated` tag.** The service adds
  that tag itself as its last step, so a trigger listening for it would re-fire
  on its own write-back, forever. Fire on `needs-engineering` (or your macro) and
  add *Tags · contains none of · `escalated`* so a ticket can't get two threads.
- **Exclude the integration account on the comment trigger** (*Current user is
  not …*), or any comment that account writes echoes back into the thread.

---

## API reference

Base path `/api/v1`.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/ping` | Liveness (also at `/api/v1/ping`) |
| POST | `/api/v1/webhooks/zendesk/escalation` | Flow 1 — create the thread |
| POST | `/api/v1/webhooks/zendesk/comment` | Flow 2 — mirror a comment |
| POST | `/api/v1/webhooks/zendesk/status` | Flow 3 — mirror a status change |
| POST | `/api/v1/reports/stale` | Run the digest on demand |
| POST | `/api/v1/slack/messages` | Post an ad-hoc Slack message |
| PUT | `/api/v1/zendesk/tickets/:ticket_id/tags` | Replace a ticket's tags |

The three `/webhooks/zendesk/*` endpoints sit behind HMAC-SHA256 signature
verification (`X-Zendesk-Webhook-Signature` over timestamp + body).

Payload fields are defined in `pkg/contracts/escalation.go` — that file is the
source of truth for what a trigger body must contain.

```bash
# Flow 1
curl -X POST localhost:8080/api/v1/webhooks/zendesk/escalation \
  -H 'Content-Type: application/json' \
  -d '{"ticket_id":"4242","subject":"Camera offline","priority":"urgent"}'

# Flow 2
curl -X POST localhost:8080/api/v1/webhooks/zendesk/comment \
  -H 'Content-Type: application/json' \
  -d '{"ticket_id":"4242","comment":"Still failing","author":"Jamie"}'
```

Errors are uniform: `{"error":"..."}` with the status the upstream or the
validator produced.

---

## Extending it

Adding an endpoint:

1. Add the DTOs to `pkg/contracts`.
2. Add the business call to a service in `pkg/application/<domain>`.
3. Add the handler to `pkg/api/handlers`, and hang it off the `Handlers` struct.
4. Register the route in `cmd/server/<domain>_routes.go`.

Not built, and deliberately so:

- **Slack → Zendesk reverse sync** (react with 🎫 to write an internal note back
  to the ticket). Documented in the original design, never built in Zapier. It
  needs a Slack Events subscription and a ticket lookup by `thread_ts`.
- **A separate reports channel.** Alerts and digests share one channel because
  the Zapier build did; only the bot display name distinguishes them.
