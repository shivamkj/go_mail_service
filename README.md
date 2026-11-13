# mailsvc

A minimal, in-memory, multi-account email service in Go. No framework, no
external services beyond the mail providers themselves.

## Setup

```bash
go get github.com/emersion/go-imap@latest
go get github.com/emersion/go-message@latest
go mod tidy
go run ./cmd/server
```

## Architecture

- `message/` — the `Message`/`Attachment` structs shared by every package.
- `store/` — thread-safe in-memory index: messages by account+mailbox+UID,
  by Message-ID, and grouped into threads via In-Reply-To/References.
- `imapclient/` — one `Syncer` per account. Connects, does an initial sync,
  then sits in `IDLE`. On any server notification it stops idling, fetches
  everything newer than the last-seen UID, and goes back into `IDLE`. On any
  error it reconnects with exponential backoff. IDLE is also refreshed every
  ~29 minutes since most servers time it out around 30.
- `smtpclient/` — a small connection pool per account (`Pool`). Sending
  checks out a connection, verifies it with `NOOP`, reuses it for the
  transaction, and returns it to the pool, so a burst of sends doesn't pay a
  fresh TCP+TLS+AUTH handshake each time. A dead connection is silently
  redialed once and the send retried.
- `service.go` — glues one `Store` to N accounts. `SendNew` and `Reply`
  build a MIME message (with attachments) via `go-message/mail` and hand the
  raw bytes to the account's SMTP pool. `Reply` sets `In-Reply-To`/
  `References` so Gmail/Outlook/etc. thread it correctly.

## Scaling this up

This is deliberately minimal. For real production use, you'd want to change:

- **Persistence.** Everything lives in memory and is lost on restart. Swap
  `store.Store` for Postgres/etc., and persist each account's last-seen UID
  so a restart doesn't have to guess how far back to resync.
- **Attachments in memory.** Large attachments currently sit in the
  `Message` struct as `[]byte`. At scale, stream them straight to object
  storage (S3/GCS) and store a reference instead.
- **Horizontal scaling.** Each account's IMAP IDLE connection is one
  long-lived goroutine — fine up to thousands per process. Beyond that,
  shard accounts across worker processes/instances (e.g. hash account ID mod
  N workers) rather than running every account in one process.
- **SMTP pool sizing/backpressure.** `PoolSize` is per-account; tune it to
  the provider's concurrent-connection limits, and add a queue in front of
  `Send` if you need to survive provider outages instead of failing inline.
- **Security.** Passwords are plain config fields here — load them from a
  secrets manager, and prefer OAuth2 (XOAUTH2) over passwords for providers
  that support/require it (Gmail and Microsoft 365 are deprecating basic
  auth).

## Notes on the libraries

Built on `github.com/emersion/go-imap` (v1, stable, IDLE built into the
client) and `github.com/emersion/go-message/mail` for MIME parsing/building.
Both evolve; if `go build` complains about a method name, run
`go doc github.com/emersion/go-message/mail` (or the imap client package) to
check the exact signature on the version `go mod tidy` resolved.
