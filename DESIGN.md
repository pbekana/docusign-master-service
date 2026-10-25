# DocuSign Master Service — Design Document

---

## Table of Contents

1. [Architecture](#architecture)
2. [Authentication Flow](#authentication-flow)
3. [Job Lifecycle](#job-lifecycle)
4. [HMAC Signing Protocol](#hmac-signing-protocol)
5. [Storage Conventions](#storage-conventions)
6. [Resilience Strategy](#resilience-strategy)
7. [Extension Points](#extension-points)

---

## Architecture

The service is split into a stateless HTTP API layer and a stateful background worker pool. Both share the same PostgreSQL database for coordination.

```
┌──────────────────────────────────────────────────────────────┐
│                        HTTP API Layer                        │
│  (Chi router, HMAC middleware, handlers, audit logging)      │
└────────────────────┬─────────────────────────────────────────┘
                     │ creates / queries jobs
                     ▼
┌──────────────────────────────────────────────────────────────┐
│                    Job Service + Worker Pool                  │
│  (job queue, concurrency limit, heartbeat, crash detection)  │
└────┬────────────────────────────┬────────────────────────────┘
     │ fetches data               │ persists state / checkpoints
     ▼                            ▼
┌───────────────┐        ┌────────────────────┐
│  DocuSign API │        │    PostgreSQL       │
│  (envelopes,  │        │  (jobs,            │
│   recipients, │        │   job_checkpoints, │
│   documents,  │        │   audit_logs,      │
│   templates)  │        │   failed_calls)    │
└───────────────┘        └────────────────────┘
                                  │ uploads output files
                                  ▼
                         ┌────────────────────┐
                         │      MinIO         │
                         │  (Parquet / JSON)  │
                         └────────────────────┘
```

### Package boundaries

| Package | Responsibility |
|---|---|
| `cmd/server` | Wires all dependencies together (dependency injection root) |
| `internal/config` | Reads and validates environment variables |
| `internal/httpapi` | Chi router, route registration, global middleware |
| `internal/httpapi/handlers` | HTTP handlers; delegate all logic to service layer |
| `internal/httpapi/middleware` | Recovery, logging, HMAC authentication |
| `internal/docusign` | DocuSign auth client, eSignature REST API client |
| `internal/jobs` | Job model, repository, service, worker pool, checkpoints |
| `internal/normalization` | Envelope/recipient/document/template normalisation |
| `internal/storage` | MinIO and local-filesystem storage backends |
| `internal/resilience` | Retry and rate-limit handling |
| `internal/security` | HMAC signing, nonce store, encryption helpers |
| `internal/audit` | Audit log model, repository, service |
| `internal/database` | pgx pool setup, migration runner |
| `migrations` | Versioned SQL migration files |

---

## Authentication Flow

The service authenticates with DocuSign using **OAuth 2.0 JWT Grant** for fully unattended, server-to-server operation.

```
Service                           DocuSign OAuth Server
  │                                       │
  │  1. Load RSA private key from file    │
  │  2. Build JWT assertion               │
  │     iss = integration key             │
  │     sub = user ID                     │
  │     aud = auth server hostname only   │
  │     scope = "signature impersonation" │
  │     exp = now + 1 hour                │
  │                                       │
  │──── POST /oauth/token ───────────────►│
  │     grant_type=urn:ietf:params:       │
  │       oauth:grant-type:jwt-bearer     │
  │     assertion=<signed JWT>            │
  │                                       │
  │◄─── { access_token, expires_in } ────│
  │                                       │
  │──── GET /oauth/userinfo ─────────────►│
  │     Authorization: Bearer <token>     │
  │                                       │
  │◄─── { sub, accounts:[...] } ─────────│
  │                                       │
  │  3. Select target account             │
  │     (configured or default)           │
  │  4. Extract account base_uri          │
  │  5. Cache token (expires_in - 60s)    │
  │                                       │
  │  6. Use cached token for all API      │
  │     calls; refresh before expiry      │
```

Key safety rules:
- The RSA private key is never logged.
- Access tokens are never logged.
- The JWT `aud` claim is the **hostname only** (e.g. `account-d.docusign.com`), not a full URL.
- Token refresh is triggered when the cached token has fewer than 60 seconds remaining.

---

## Job Lifecycle

Each extraction job moves through a strict state machine. State transitions are persisted to PostgreSQL in the `jobs` table.

### State machine

```
                    ┌──────────┐
          submit    │  PENDING │
         ─────────► │          │
                    └────┬─────┘
                         │ worker picks up job
                         ▼
                ┌──────────────────┐
                │ FETCHING_ENVELOPES│
                │ (paginated fetch) │
                └────────┬─────────┘
                         │ all pages done
                         ▼
               ┌───────────────────┐
               │  FETCHING_DETAILS │
               │  (recipients,     │
               │   documents,      │
               │   templates)      │
               └────────┬──────────┘
                        │ all details fetched
                        ▼
                ┌──────────────┐
                │  NORMALIZING │
                └──────┬───────┘
                       │ normalisation complete
                       ▼
            ┌──────────────────────┐
            │  UPLOADING_TO_MINIO  │
            └──────────┬───────────┘
                       │ upload complete
                       ▼
                ┌───────────┐
                │ COMPLETED │
                └───────────┘
```

Error and cancellation paths (apply from any active state):

```
[Any active state] ──── error ────────► FAILED
[Any active state] ──── cancel request ► CANCELLED
[Any active state] ──── heartbeat stale ► CRASHED
```

### Checkpointing and resumption

At the end of each successfully processed page, the worker writes a `job_checkpoints` row recording:
- The stage name.
- The current cursor position (page offset).
- The number of records processed so far.
- The ID of the last envelope processed.

On startup and before picking up a new job, the worker pool calls `RecoverOnStartup` to re-queue any jobs in `PENDING`, `FETCHING_ENVELOPES`, `FETCHING_DETAILS`, `NORMALIZING`, or `UPLOADING_TO_MINIO` state (i.e. jobs that were in-flight when the process last exited).

When a job is resumed, the service loads the latest checkpoint for each stage and restarts pagination from the saved cursor.

### Heartbeat and crash detection

Running workers update the `last_heartbeat_at` column every `HEARTBEAT_INTERVAL_SECONDS`. A scheduled maintenance call to `POST /api/maintenance/detect-crashed` marks any job whose heartbeat is older than `CRASH_TIMEOUT_MINUTES` as `CRASHED`.

---

## HMAC Signing Protocol

All mutable and sensitive endpoints require request signing with HMAC-SHA256 to authenticate callers without transmitting passwords.

### Canonical string construction

```
CANONICAL = METHOD + "\n"
          + PATH + "\n"
          + TIMESTAMP + "\n"
          + NONCE + "\n"
          + SHA256HEX(BODY)

SIGNATURE = HMAC-SHA256(SECRET, CANONICAL)
           encoded as lowercase hex
```

Where:
- `METHOD` — uppercase HTTP method (`GET`, `POST`, `DELETE`, …)
- `PATH` — URL path without query string (e.g. `/api/scan/start`)
- `TIMESTAMP` — Unix epoch seconds as a decimal string
- `NONCE` — a unique, random, single-use hex string
- `SHA256HEX(BODY)` — lowercase hex SHA-256 of the raw request body bytes (SHA-256 of empty bytes `""` for requests with no body)
- `SECRET` — the shared secret from `HMAC_SECRET_KEY_CORE`

### Required headers

| Header | Value |
|---|---|
| `X-Client-ID` | Client identifier registered in the service configuration |
| `X-Timestamp` | Unix timestamp (decimal string) |
| `X-Nonce` | Unique random hex string |
| `X-Signature` | Computed HMAC-SHA256 signature (hex) |

### Two client roles

| Client ID | Allowed methods | Intended use |
|---|---|---|
| `coordinator` | All HTTP methods | Orchestration systems, CI/CD pipelines |
| `engineer` | `GET` only | Human operators inspecting state |

Additional client IDs can be registered by extending the secrets map in `config.go`.

### Replay prevention

Every nonce is stored in an in-memory `NonceStore` until `now + 2 × HMAC_TIMESTAMP_TOLERANCE_SECONDS`. Any second request arriving with the same nonce within that window is rejected with a replay error. The background cleanup goroutine prunes expired entries periodically.

### Timestamp tolerance

The `HMAC_TIMESTAMP_TOLERANCE_SECONDS` window (default 300 seconds) limits how far in the past or future a signed request may be timestamped. Requests outside this window are rejected.

---

## Storage Conventions

### Local output paths

When `LOCAL_OUTPUT_DIR` is set, the service writes output files under:

```
$LOCAL_OUTPUT_DIR/{job_id}/{stage}.{format}
```

For example:
```
/tmp/docusign-output/3f2a1b.../envelopes.parquet
/tmp/docusign-output/3f2a1b.../recipients.parquet
/tmp/docusign-output/3f2a1b.../documents.json
```

### MinIO object key template

Objects are uploaded to the configured bucket using the path:

```
{job_id}/{stage}.{format}
```

For example:
```
3f2a1b4c-...-ef12/envelopes.parquet
3f2a1b4c-...-ef12/recipients.parquet
```

### Parquet schema notes

- Parquet files are written using `parquet-go/parquet-go`.
- Schema is derived from Go struct tags: `parquet:"field_name"`.
- Nullable fields use pointer types (`*string`, `*time.Time`).
- All timestamps are stored as UTC.
- String fields representing IDs or codes use the `parquet:"name"` tag directly (no dictionary encoding).

---

## Resilience Strategy

### Transient failure retry

The `internal/resilience` package provides a reusable `DoWithRetry` function that wraps any HTTP operation.

| Scenario | Behaviour |
|---|---|
| HTTP 500, 502, 503, 504 | Retry with bounded exponential backoff + jitter |
| Network timeout / connection reset | Retry with bounded exponential backoff + jitter |
| HTTP 429 with `Retry-After` header | Sleep exactly `Retry-After` seconds, then retry |
| HTTP 429 without `Retry-After` | Sleep `RATE_LIMIT_FALLBACK_DELAY_SECONDS`, then retry |
| HTTP 400, 401, 403, 404 | Do not retry (permanent client error) |
| Max attempts exceeded | Record in `failed_calls` table, return error |

### Backoff formula

```
delay = min(BASE_DELAY × 2^attempt, MAX_DELAY) + jitter
jitter = random value in [0, BASE_DELAY]
```

### Rate limit budget

The `RateLimiter` tracks cumulative wait time per job. If the projected wait would exceed `RATE_LIMIT_MAX_WAIT_SECONDS`, the job is paused and its state persisted to PostgreSQL. The job can be resumed once the rate-limit window has passed, avoiding indefinite blocking of the worker pool.

### Context cancellation

All retry loops check `ctx.Done()` between attempts. A cancelled context immediately terminates the retry loop and propagates the cancellation error up the call stack.

---

## Extension Points

### Adding a new DocuSign object type

1. Add a new fetching function in `internal/docusign/` (follow the pattern in `envelopes.go`).
2. Add a new normalisation function in `internal/normalization/`.
3. Add a Parquet-tagged struct in the relevant `_normalized.go` file.
4. Add a new stage constant in `internal/jobs/models.go`.
5. Wire the new stage into the worker pipeline in `internal/jobs/worker.go`.
6. Add a migration to capture any new database columns needed for progress tracking.

### Swapping the storage backend

The storage layer is injected as an interface. To add an AWS S3 backend, for example:

1. Create `internal/storage/s3.go` implementing the same interface as `MinIOStorage`.
2. Configure it in `cmd/server/main.go` based on an environment variable.
3. Inject it in place of (or alongside) `MinIOStorage`.

No changes to the worker or handler layers are required.
