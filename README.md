# rulecast

A feature flag and targeting service in Go. Rules are written in a small expression language,
type-checked against a declared attribute schema when they are saved, and compiled into closures so
that evaluating one costs tens of nanoseconds and zero allocations.

Built as a portfolio project. The interesting part is not the CRUD around flags — it is the
language, the compiler, and the bucketing.

One external dependency: the Postgres driver. Everything else — HTTP, SSE, JSON, hashing, the
parser, benchmarks, fuzzing, metrics — is the standard library.

---

## Getting started

Two ways to run it. Neither needs the other.

### Without a database — Go only

```bash
make tidy
make check
make run
```

`tidy` resolves dependencies and writes `go.sum`, `check` runs gofmt, vet and the tests with the
race detector, `run` serves the UI on http://localhost:8090/index.html.

No Docker, no Postgres. `RULECAST_DATABASE_URL` is unset, so the service uses the in-memory store
and seeds itself with five flags on first start. The log says so plainly:

```
running without a database; published versions will not survive a restart
```

Publishing, rollback and the whole UI work; they just live for as long as the process does. This is
the fastest way to look at the thing.

The tests need no containers either. `store.Versions` is a five-method port with an in-memory
implementation, and that implementation doubles as the test double — so even the HTTP tests run a
real server through `httptest` against memory. `go test -race ./...` finishes in seconds.

### With Postgres — Docker

```bash
make up
make logs
make down
```

`up` builds the image and starts Postgres and the service, `logs` follows the service log, `down`
stops the containers and keeps the volume.

`make up` is the only target that touches Docker. It builds a distroless image (~12 MB, non-root)
and starts two containers. Compose waits for Postgres to pass its health check before starting the
service; the service then applies its embedded migrations itself, so there is no separate migration
step.

`make tidy` must have run at least once: the image build does `COPY go.mod go.sum` and
`go mod download`, and without `go.sum` it fails immediately.

What Postgres adds over the in-memory run:

- published versions survive a restart, and `/api/v1/admin/versions` shows the real history;
- exposures are written to the `exposure` table in batches with `COPY`, so the experiment side is
  actually recording something.

### Signing in

Both `make run` and `make up` set API keys, so the two fields at the top right of the UI are not
optional:

| Field | Value |
|---|---|
| admin key | `admin-local-key` |
| sdk key | `sdk-local-key` |

Override with `make run ADMIN_KEY=… SDK_KEY=…`, or edit the environment block in
`docker-compose.yml`. Leaving a key empty in the configuration disables the check for that surface
entirely — convenient when you press ▶ in an IDE with no environment set, and the reason the UI
works there without typing anything.

### Poking at it

```bash
make demo
curl -s localhost:8090/readyz
curl -s localhost:8090/metrics
```

`make demo` evaluates `checkout-v2` for a German VIP and should answer with
`"reason":"RULE_MATCH","ruleId":"vip-dach"`.

### Filling the experiment side

Flags need no SQL seed — the service publishes `flag.Seed` itself when the environment is empty, and
that path goes through validation and compilation, so a broken seed fails the start instead of
poisoning the database.

Exposures are different: the table stays empty until traffic arrives. Rather than inserting rows
directly, drive the real endpoint:

```bash
make traffic
make report
```

`make traffic` runs `cmd/rulecastload`, which evaluates one flag for thousands of synthetic users
and prints the split it observed:

```
flag pricing-banner-copy: 5000 evaluations in 1.2s (4166/s)
  ROLLOUT → control              423   34.1%
  ROLLOUT → urgency              412   33.2%
  ROLLOUT → value                405   32.7%
  DEFAULT → control             3760      —
```

Writing those rows by hand would be worse than useless: the variation in the table would not be the
one bucketing actually assigns, so every number derived from it would be fiction. Going through
`/api/v1/sdk/evaluate` means the recorded assignment *is* the assignment.

`make report` then reads them back through
`GET /api/v1/admin/experiments/{flagKey}`, which needs Postgres — the in-memory store answers too,
but only for the traffic of the current process.

Override the target: `make traffic FLAG=checkout-v2 USERS=20000`.

In the UI, the loop worth walking through:

1. pick `checkout-v2` on the left;
2. in the `vip-dach` rule, break an attribute — type `user.contry` — and watch the caret appear
   under the typo as you type;
3. fix it, press **Publish new version**; the version becomes 2;
4. press **Roll back** on v1 in the history; v3 appears as a copy of v1, and v2 stays in the log;
5. press **Evaluate** on the right to see the variation together with its reason and rule id.

### Looking inside the database

```bash
docker compose exec postgres psql -U rulecast -d rulecast \
  -c "select version, comment, created_by, created_at from flag_set_version order by version desc"

docker compose exec postgres psql -U rulecast -d rulecast \
  -c "select flag_key, variation, count(*) from exposure group by 1, 2 order by 3 desc"
```

### Ports

| | |
|---|---|
| UI and API | http://localhost:8090 |
| Postgres | `localhost:5434` (`rulecast` / `rulecast`) |

Postgres is published on **5434** on purpose: 5432 is usually taken by a local install and 5433 by
the paystream stack next door.

### Troubleshooting

| Symptom | Cause |
|---|---|
| `make up` fails on `go mod download` | `go.sum` is missing — run `make tidy` first |
| UI loads but every list is empty | keys not entered; see *Signing in* |
| `/` returns 404 while the API works | `RULECAST_WEB_ROOT` is relative, so the working directory must be the project root; set it in the IDE run configuration or pass an absolute path |
| `Connection refused` on 5434 | the container is not running — `docker compose ps` |
| `bind: address already in use` on 8090 | an earlier run is still alive — `lsof -tiTCP:8090 -sTCP:LISTEN \| xargs kill` |
| `make: No rule to make target '#'` | zsh does not strip `#` comments in interactive shells, so a pasted trailing comment reaches make as a target; paste the command alone |
| Image build fails with `go.mod requires go >= …` | the builder image in `Dockerfile` is older than the `go` line in `go.mod`; bump `FROM golang:<version>-alpine` |
| Schema changes have no effect | migrations are append-only; add a new file under `internal/store/migrations/` rather than editing `0001_init.sql`, or `make down && docker volume rm rulecast_postgres-data` |

---

## The rule language

```
user.country in ["DE", "AT", "CH"] and user.tier == "vip" and user.spend > 250
user.email endsWith "@example.com" and not user.beta
device.os == "ios" and app.version startsWith "3."
user.email matches "@(gmail|proton)\\.com$"
```

Operators: `and or not == != > >= < <= in contains startsWith endsWith matches`, parentheses,
string/number/bool literals, string lists, `#` comments. Both quote styles, with the usual escapes.

`in` is exact membership, so `user.email in ["@gmail"]` matches only an address that *is* that
string. Substring work belongs to `contains`, `startsWith` and `endsWith`, which are case
sensitive; `matches` with `(?i)` covers the rest.

The pipeline is four stages, each in its own file under `internal/expr`:

| Stage | File | What it does |
|---|---|---|
| Lex | `lexer.go` | hand-written scanner, tracks line and column |
| Parse | `parser.go` | Pratt parser, four precedence levels, produces an AST |
| Type-check | `typecheck.go` | resolves attributes against the schema, rejects bad operand types |
| Compile | `compile.go` | folds the AST into a tree of closures |

**Attributes are declared, not discovered.** Each environment ships a schema:

```json
{ "user.country": "string", "user.spend": "number", "user.beta": "bool" }
```

Without it, `user.contry == "DE"` would be a rule that silently never matches — the worst kind of
production bug, because nothing is broken, the audience is just empty. With it, saving that rule
fails and says where:

```
unknown attribute "user.contry" at line 1, column 26
  user.country == "DE" and user.contry == "PL"
                           ^
```

Regular expressions in `matches` are compiled during the type-check, so a malformed pattern is
rejected by the author rather than discovered by a client at runtime.

`in` picks its representation by size: a linear scan for the handful of values a targeting rule
usually lists, a hash set past eight. Both paths are covered by tests.

## Bucketing

A rollout has to answer two questions correctly, and the second one is where hand-rolled
implementations usually fail:

1. **Is this user in the rollout?** Stable across processes and restarts.
2. **What happens when the percentage changes?** Widening 10% → 20% must *only add* users.

Both fall out of one decision: a user's bucket is a fixed point in `[0, 10000)`, and a rollout of
P% serves everyone below `P × 100`. Growing the window cannot evict anyone already inside it. The
property is asserted directly, not assumed:

```go
func TestWideningARolloutOnlyAdds(t *testing.T)   // 20k users across 1, 5, 10, 25, 50, 75, 100%
```

FNV-1a is hand-rolled over the string bytes rather than taken from `hash/fnv`, because the
standard `Write([]byte)` forces an allocation per evaluation. `hash/maphash` is unusable here for a
subtler reason: it reseeds per process, so the same user would land in a different bucket after a
restart — silently reshuffling every experiment on deploy.

The flag key and a per-flag salt go into the hash, so two flags at 10% do not target the same tenth
of the audience. That is also asserted (`TestOfDecorrelatesFlags`).

## Evaluation and the hot path

`internal/eval` compiles a whole environment into an immutable `Snapshot`: every regexp compiled,
every rule a closure, every variation lookup a map hit. Publishing a new version builds a new
snapshot and swaps a pointer:

```go
type Registry struct {
    current atomic.Pointer[Snapshot]
    ...
}
```

Readers do a single atomic load — no mutex on the request path. An in-flight evaluation keeps the
snapshot it started with, so it can never observe a half-applied version.

Every result carries **why** it came out that way:

```json
{ "flagKey": "checkout-v2", "variation": "on", "value": true,
  "reason": "RULE_MATCH", "ruleId": "vip-dach", "version": 7 }
```

`RULE_MATCH · ROLLOUT · DEFAULT · FLAG_OFF · FLAG_NOT_FOUND · MISSING_USER_KEY`. Without the
reason, "why did this user get the old checkout" is unanswerable; with it, it is one API call.

## Versioning and rollback

A flag is never edited in place. Changing one copies the current document, applies the change and
publishes it as the next version, so:

- **audit is free** — every version records who, when and why;
- **rollback is a publish**, not a delete: rolling back to v3 writes v9 as a copy of v3, and the
  fact that a rollback happened stays in the history;
- **a rejected change leaves nothing behind** — validation and compilation both run before the
  insert, and the live snapshot is only swapped after the row is committed.

Concurrent publishes are settled by the primary key on `(environment, version)`. Two editors racing
from the same base compute the same next number and the loser gets a `409`, rather than one of them
silently overwriting the other.

## Distribution to SDKs

| Endpoint | Purpose |
|---|---|
| `POST /api/v1/sdk/evaluate` | evaluate one flag |
| `POST /api/v1/sdk/evaluate-all` | evaluate everything for one user |
| `GET /api/v1/sdk/snapshot` | bootstrap the whole document; the version is the ETag |
| `GET /api/v1/sdk/stream` | SSE, pushes each new version |

The stream sends the current version on connect, then every publish. A subscriber that stops
reading is dropped rather than allowed to block the publish — it will pick up the newest version
when it reconnects, and a stale version was worthless anyway.

`snapshot` uses the version number as its ETag, so a reconnecting client that already has v7 gets
a `304` and no payload.

## Admin API

```
GET    /api/v1/admin/flags
GET    /api/v1/admin/flags/{key}
PUT    /api/v1/admin/flags/{key}          publishes a new version
DELETE /api/v1/admin/flags/{key}
POST   /api/v1/admin/validate             type-check one expression, no save
GET    /api/v1/admin/versions
GET    /api/v1/admin/versions/{version}
POST   /api/v1/admin/versions/{version}/rollback
GET    /api/v1/admin/schema
```

`validate` answers `200` even for a broken rule — the editor calls it on every keystroke, and a
failing rule is an answer there, not a failed request. Validation reports *every* problem it finds
with a JSON path, because an author fixing a ruleset wants the whole list rather than one round
trip per typo:

```json
{ "code": "INVALID_DOCUMENT", "problems": [
    { "path": "flags[1].rules[0].expression", "message": "unknown attribute \"user.contry\" …" },
    { "path": "flags[1].rules[2].serve.variation", "message": "unknown variation \"onn\"" } ] }
```

## Experiments

Rules can serve a weighted split instead of a fixed variation. Each assignment produced by a rule
or a rollout emits an exposure — which user saw which variation, at which version.

Exposure volume equals request volume, so they are buffered and loaded with `COPY` rather than
inserted one at a time. Under sustained overload the buffer drops rather than blocks: an experiment
losing a sample is tolerable, a blocked evaluation is not. Dropped exposures are counted and
exported, so the loss is visible instead of silent.

Evaluations that were not assignments — a disabled flag, an unknown key — are deliberately *not*
recorded. They tell an experiment nothing and would only dilute the data.

`GET /api/v1/admin/experiments/{flagKey}?from=&to=` reads them back: exposures and distinct users
per variation, with each variation's share. `cmd/rulecastload` generates the traffic to look at.

## The UI

`web/` — one page, vanilla JS, no build step. Flag list, rule editor with live type-checking as you
type (caret and message straight from the compiler), a weighted-rollout editor, an evaluation
tester that shows the reason, and version history with a rollback button.

## Layout

```
cmd/rulecast/        entry point, wiring, graceful shutdown, container probe
cmd/rulecastload/    synthetic traffic generator, so exposures have something to report
internal/expr/       the language: lexer, parser, AST, type-checker, compiler
internal/bucket/     deterministic hashing and weighted assignment
internal/flag/       domain types, validation, seed document
internal/eval/       compiled snapshot, atomic registry
internal/store/      Versions and Exposures ports, Postgres and in-memory adapters, migrations
internal/api/        HTTP: SDK, admin, SSE, middleware, metrics
internal/config/     environment configuration
web/                 admin UI
```

`internal/expr` and `internal/bucket` import nothing but the standard library. The domain does not
know that Postgres or HTTP exist.

## Testing

```bash
make test    # unit, race detector
make bench   # ns/op and allocs/op
make fuzz    # 30s of fuzzing against the parser
```

What is actually asserted, beyond the happy paths:

- **the rollout property** — widening a percentage never evicts a user, over 20k users and seven
  percentages;
- **flag decorrelation** — two flags at the same percentage do not select the same users;
- **distribution** — buckets are even to within 5% across ten slots at 100k users; weighted splits
  land within 1.5 points of their weights;
- **the parser never panics** — fuzzed, because rules are text typed by humans into an admin API;
- **concurrency** — eight readers evaluating while 50 versions are published, under `-race`;
- **a rejected publish does not move the live version**;
- **rollback is append-only** — the version number goes up, not back;
- **SSE delivers to an already-connected client**.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `RULECAST_ADDR` | `:8090` | listen address |
| `RULECAST_ENVIRONMENT` | `production` | which environment this process serves |
| `RULECAST_DATABASE_URL` | *(empty)* | empty runs in memory; versions do not survive a restart |
| `RULECAST_SDK_KEY` | *(empty)* | key for the SDK endpoints; empty disables the check |
| `RULECAST_ADMIN_KEY` | *(empty)* | key for the admin endpoints |
| `RULECAST_WEB_ROOT` | `web` | static UI directory |
| `RULECAST_LOG_LEVEL` | `info` | debug, info, warn, error |
| `RULECAST_HEARTBEAT_SECONDS` | `25` | SSE keep-alive interval |

Keys are compared with `subtle.ConstantTimeCompare`; a plain `==` leaks a shared secret one byte at
a time to anyone who can measure the response.

## Operations

`/healthz` is liveness, `/readyz` turns green only once a version is compiled and active, and
`/metrics` is Prometheus text format — hand-written, because the exposition format for counters and
gauges is a few lines and it keeps the dependency count at one.

Exported: evaluations total and by reason, publishes, connected stream clients, active version and
flag count, dropped exposures.

## Stack

Go 1.23 · PostgreSQL 16 · `pgx/v5` · Docker (distroless, non-root) · GitHub Actions
