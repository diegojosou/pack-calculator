# Pack Calculator

A small Go service that, given an order quantity and a configurable set of
pack sizes, returns the optimal pack breakdown:

1. **Only whole packs** are shipped — packs are never broken open.
2. Within rule 1, the **fewest items** are shipped (rule 2 of the brief).
3. Within rules 1 and 2, the **fewest packs** are used (rule 3 of the brief).

It exposes a JSON HTTP API and ships with a small static UI served from the
same binary.

> Live demo: <https://pack-calculator-yugh.onrender.com>
>
> Hosted on Render's free tier, which sleeps the service after about 15
> minutes of inactivity. The first request after a cold start can take
> around 30 seconds to wake the container; subsequent requests are immediate.

## Quick start

### Run locally with Go

```bash
git clone <repo-url> pack-calculator
cd pack-calculator
make run         # starts on http://localhost:8080
```

Then open <http://localhost:8080> in a browser.

### Run with Docker

```bash
make docker-build      # build the image (~15 MB, distroless base)
make docker-run        # run on port 8080 with a named volume for persistence
```

Or directly without `make`:

```bash
docker build -t pack-calculator:latest .
docker run --rm -p 8080:8080 -v pack-calculator-data:/data pack-calculator:latest
```

Then **visit <http://localhost:8080>** or `curl http://localhost:8080/healthz`.

> The binary binds to `0.0.0.0:8080` inside the container so the published
> port reaches the listener. `scripts/docker-smoke.sh` is a regression test
> that builds, runs, and curls the container end-to-end from the host.

### Run with Docker Compose

```bash
docker compose up --build
```

### Verify the container end-to-end

```bash
make docker-smoke
```

That target builds the image, starts the container, polls `/healthz`,
configures the edge-case pack sizes (23, 31, 53), submits an order of
500,000 items, asserts the result, and tears the container down. Exit code 0
means the container is reachable and the algorithm is correct.

## API reference

Base URL: `http://localhost:8080`

### `GET /api/pack-sizes`

Returns the currently configured pack sizes (ascending).

```bash
curl http://localhost:8080/api/pack-sizes
# {"pack_sizes":[250,500,1000,2000,5000]}
```

### `PUT /api/pack-sizes`

Replaces the pack-size set. Body is `{"pack_sizes": [int, ...]}`. Sizes are
deduplicated and validated; any non-positive value rejects the whole request
and leaves stored state unchanged.

```bash
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"pack_sizes":[23,31,53]}' \
  http://localhost:8080/api/pack-sizes
# {"pack_sizes":[23,31,53]}
```

### `POST /api/calculate`

Computes the optimal pack breakdown for an order. Body is `{"order": int}`.

```bash
curl -X POST -H 'Content-Type: application/json' \
  -d '{"order":500000}' \
  http://localhost:8080/api/calculate
```

```json
{
  "order": 500000,
  "shipped_items": 500000,
  "total_packs": 9438,
  "packs": [
    {"size": 53, "quantity": 9429},
    {"size": 31, "quantity": 7},
    {"size": 23, "quantity": 2}
  ],
  "used_pack_sizes": [53, 31, 23]
}
```

### `GET /healthz`

Cheap liveness probe. Returns `{"status":"ok"}` on 200 when the store is
reachable; `{"status":"down"}` on 503 otherwise.

## Configuration

Environment variables (all optional):

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Listening port. |
| `DB_PATH` | `/data/pack-calculator.db` | SQLite file location. The parent dir is created on startup. |
| `SEED` | `250,500,1000,2000,5000` | Comma-separated pack sizes used **only on first boot** (when the store is empty). Subsequent restarts keep operator-configured values. |

## Architecture

```
cmd/server/main.go            entry point — config, store, HTTP server lifecycle
internal/calculator/          pack-fitting algorithm (DP) + tests
internal/store/               SQLite + in-memory persistence implementations
internal/server/              HTTP handlers, JSON shapes, middleware
internal/web/                 //go:embed of the static UI
internal/web/assets/          index.html, app.js, styles.css
```

### Algorithm

A bounded unbounded-knapsack DP, in two phases:

1. Compute, for every reachable item total `t` in `[0, order + max(packSize)]`,
   the minimum number of packs needed to make exactly `t` items.
2. The smallest reachable `t` with `t >= order` is the optimal item count
   (rule 2). The DP value at that `t` is the minimum pack count for it (rule 3).
   The breakdown is reconstructed by backtracking through the recorded
   "last pack added" array.

A naive greedy algorithm fails on inputs like `{23, 31, 53}` with an order
of 500,000:

- `floor(500000 / 53) = 9433` 53-packs ⇒ 499,949 items, remainder 51.
- `51` is not expressible as a non-negative combination of `{23, 31}`, so
  greedy either stalls or overshoots to a non-optimal item count.

The DP avoids this by considering all reachable totals in the search window.

Memory: `O(order + max(packSize))` ints. The order is capped at 50,000,000
(see `maxOrder` in `internal/calculator/calculator.go`) to keep allocations
bounded.

### Persistence

Pack sizes are persisted in SQLite (single-file DB). The driver is
[`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) — pure Go, no CGO —
which keeps the final container image to a static binary on a distroless
base.

A `MemoryStore` is provided behind the same interface for tests and
ephemeral runs.

## Testing

```bash
make test            # all unit + integration tests with -race
make cover           # writes coverage.html
```

Highlights:

- `internal/calculator/calculator_test.go` — covers every worked example from
  the brief plus the large-order edge case
  (`{23, 31, 53}` + 500,000 ⇒ `{23: 2, 31: 7, 53: 9429}`).
- `internal/store/store_test.go` — same contract test runs against both
  `MemoryStore` and `SQLiteStore`; persistence is verified across a
  Close/reopen cycle.
- `internal/server/server_test.go` — handler tests via `httptest`, including
  the edge case end-to-end through the HTTP layer.
- `scripts/docker-smoke.sh` — runs the same edge case against a real Docker
  container to prove the listener is reachable from the host.

## Deployment

The repo is set up to deploy to any Docker-aware platform. Two suggested paths:

### Fly.io (free tier)

```bash
fly launch              # use the existing Dockerfile, accept defaults
fly volumes create pack_data --region <region> --size 1
fly deploy
```

In `fly.toml`, mount the volume at `/data` and set `PORT = "8080"`.

### Render

A `render.yaml` blueprint is committed at the repo root. To deploy:

1. From the Render dashboard, choose **New +** → **Blueprint** and pick
   this repo.
2. Render reads `render.yaml`, provisions a Docker web service on the free
   plan with `PORT`, `DB_PATH`, and `SEED` already wired, and uses
   `/healthz` as the health check.
3. Click **Apply**. First build takes 3 to 5 minutes.

The free plan does not support persistent disks, so the SQLite file is
ephemeral and operator-configured pack sizes reset to `SEED` on each
deploy or cold start. To keep state across restarts, switch `plan: free`
to `plan: starter` in `render.yaml` and uncomment the `disk:` block
(mounts at `/data`).

## Project conventions

- Go 1.22+ (uses method-prefixed `http.ServeMux` patterns).
- `gofmt` / `go vet` clean.
- Small public surface — only the calculator and store APIs are exported.
- All errors that can be triggered by user input have sentinel `error` values
  so the HTTP layer maps them to 400 responses cleanly.

## License

MIT.
