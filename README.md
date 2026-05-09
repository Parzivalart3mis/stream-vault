# StreamVault

Go REST API processing 6K+ monetization events/sec via goroutine worker pools and `sync.Map`-backed leaderboard caching, with PostgreSQL persistence and structured slog observability. Built to mirror the backend concerns of a live-streaming platform's commerce layer: subscriptions, gifting, and tipping.

---

## Architecture

```
POST /creators/{id}/subscribe|gift|tip
            │
            ▼
    HTTP handler (chi)
            │  non-blocking send
            ▼
 ┌─────────────────────────┐
 │  buffered chan Event     │  cap=1000   ← 503 + Retry-After if full
 └─────────────────────────┘
            │
    ┌───────┴───────┐
    │  worker pool  │  N=8 goroutines
    └───────────────┘
            │  batch up to 100 rows OR 50ms (whichever first)
            ▼
    pgx CopyFrom → events table
            │
            ▼
    leaderboard.Cache.Add()   ← sync.Map[creatorID]*atomic.Int64

GET /leaderboard
    ← served from cache (no DB hit)
    ← background goroutine reconciles from DB every 60s
```

**Why 202 on writes:** the handler enqueues the event and returns immediately. The worker pool batches and persists asynchronously. If the channel is full, the handler returns 503 with `Retry-After: 1` — that's observable backpressure, not a silent drop.

---

## Quick Start

```bash
git clone https://github.com/yashk/streamvault && cd streamvault
cp .env.example .env
make up
curl http://localhost:8080/health
```

---

## API Reference

All responses use the envelope `{"data": ..., "error": null, "meta": {"request_id": "..."}}`.

| Method | Route | Notes |
|--------|-------|-------|
| GET | `/health` | Returns `{status, db, uptime}` |
| POST | `/creators` | Body: `{display_name}` → 201 |
| POST | `/creators/{id}/subscribe` | Body: `{tier:1\|2\|3}`, header `X-Viewer-Id` → 202 |
| POST | `/creators/{id}/gift` | Body: `{quantity, tier?}`, header `X-Viewer-Id` → 202 |
| POST | `/creators/{id}/tip` | Body: `{amount_cents}`, header `X-Viewer-Id` → 202 |
| GET | `/creators/{id}/revenue` | Query: `?since=ISO8601&until=ISO8601&group_by=type` |
| GET | `/creators/{id}/events` | Query: `?type=&cursor=&limit=` (max 200, cursor-paginated) |
| GET | `/leaderboard` | Query: `?window=7d&limit=10`, served from cache |

```bash
# Create a creator
curl -X POST http://localhost:8080/creators \
  -H 'Content-Type: application/json' \
  -d '{"display_name":"ninja"}'

# Subscribe (tier 2 = $9.99)
curl -X POST http://localhost:8080/creators/{id}/subscribe \
  -H 'Content-Type: application/json' \
  -H 'X-Viewer-Id: viewer-123' \
  -d '{"tier":2}'

# Gift 10 subs
curl -X POST http://localhost:8080/creators/{id}/gift \
  -H 'Content-Type: application/json' \
  -H 'X-Viewer-Id: viewer-123' \
  -d '{"quantity":10}'

# Tip $5.00
curl -X POST http://localhost:8080/creators/{id}/tip \
  -H 'Content-Type: application/json' \
  -H 'X-Viewer-Id: viewer-123' \
  -d '{"amount_cents":500}'

# Revenue by type
curl "http://localhost:8080/creators/{id}/revenue?group_by=type"

# Paginated events (cursor-based)
curl "http://localhost:8080/creators/{id}/events?limit=50"

# Leaderboard (from cache)
curl http://localhost:8080/leaderboard
```

---

## Concurrency Model

### Event Ingestion Pipeline (`internal/processor/pipeline.go`)

The three write endpoints (`/subscribe`, `/gift`, `/tip`) return **202 Accepted** immediately — the handler does a non-blocking send into a buffered channel and returns. A pool of 8 worker goroutines drain the channel, accumulate events into batches of up to 100, and flush via `pgx.CopyFrom` every 50ms or when the batch is full, whichever comes first.

`CopyFrom` is PostgreSQL's binary bulk-load protocol — substantially faster than individual `INSERT` statements at load.

**Backpressure:** when the channel's 1,000-event buffer is full, the handler returns `503 Service Unavailable` with `Retry-After: 1`. This is intentional — it exposes the system's capacity boundary rather than silently dropping or blocking the caller.

**Graceful shutdown:** `SIGTERM` triggers `srv.Shutdown` (drains in-flight HTTP requests), then `pipeline.Shutdown` (closes the channel, waits for workers to flush their current batches with a 30s deadline), then `pool.Close`.

### Leaderboard Cache (`internal/leaderboard/cache.go`)

The leaderboard is a `sync.Map` keyed by creator UUID, storing `*atomic.Int64` values for last-7-day revenue totals. After each successful batch write, workers call `cache.Add(creatorID, cents)` — a lock-free atomic increment. Reads call `Top(n)`, which iterates the map, sorts on read (N is small), and returns without acquiring any lock.

A background goroutine reloads the full leaderboard from the DB every 60 seconds, handling restarts and drift. This is the reconciliation loop — the cache is the hot path, the DB is the source of truth.

---

## Benchmark Results

Load test fires 10,000 events across 50 concurrent goroutines (`go run scripts/loadtest.go`):

**Native binary vs Postgres:16 in Docker:**
```
total events   : 10000
elapsed        : 1.63s
throughput     : 6133 events/sec
accepted (202) : 10000
backpressure   : 0
failures       : 0
p50 latency    : 7.6ms
p99 latency    : 17.0ms
```

**App + Postgres both in Docker Desktop (VM overhead):**
```
throughput     : 3388 events/sec
p50 latency    : 13.3ms
p99 latency    : 36.0ms
```

To reproduce:
```bash
make up          # start app + postgres in Docker
make load        # fires 10K events, prints throughput + latency
```

---

## Tech Notes

**pgx/v5 (no ORM):** pgx talks the PostgreSQL wire protocol directly. `CopyFrom` for batch inserts, `pgxpool` for connection pooling. An ORM would add an abstraction layer that hides the performance knobs and removes the opportunity to show you know how to use them.

**slog (stdlib):** Go 1.21 added `log/slog` as the standard structured logging package. JSON handler in production, text handler in tests. One `*slog.Logger` injected per component — no global state.

**chi v5:** Stdlib-flavored router. Uses `net/http` handlers and middleware directly, no framework magic. The entire routing layer is ~20 lines in `internal/api/router.go`.

---

## Local Dev

```bash
make up           # docker compose up --build -d
make down         # stop containers
make down-volumes # stop + wipe postgres volume (clean slate)
make test         # go test -race ./...
make load         # go run scripts/loadtest.go
make seed         # seed 10 creators (requires local .env)
make psql         # psql into the running postgres container
make logs         # tail app logs
```

Migrations run automatically via a `migrate` init container on `make up`. To re-run on a clean volume: `make down-volumes && make up`.

Race detector: `go test -race ./...` — the pipeline (`sync.WaitGroup` + channel) and the leaderboard cache (`sync.Map` + `atomic.Int64`) are both race-clean by design.
