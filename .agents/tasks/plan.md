# Implementation Plan — DocuSign Master Service

> This plan is also decomposed into FEAT artifacts under `.agents/tasks/task-docusign-master-service/`.
> The workflow tail has been restructured to execute FEATs sequentially.

---

## Dependency Order Summary

Foundation → Data models → DocuSign client → Resilience → Jobs engine → HTTP API → Storage/Output → Tests → Docker/Docs

---

## Implementation Plan

- [ ] 1. **Project scaffold: go.mod, .gitignore, .env.example**
      Initialize the Go module `github.com/your-org/docusign-master-service` at Go 1.26.
      Add all required direct dependencies with pinned versions (chi v5.2.1, pgx v5.7.2, minio-go v7.0.82, parquet-go v0.24.0, golang-jwt v5.2.2, uuid v0.6.0).
      Files: `go.mod`, `.gitignore`, `.env.example`
      Verify: `go mod tidy` completes without errors; `go mod verify` passes.

- [ ] 2. **Configuration: internal/config/config.go + config_test.go**
      Define a `Config` struct covering all env vars listed in the spec. Use `os.Getenv` with explicit validation; return a descriptive error listing every missing required var. Add `LoadConfig()` constructor. No third-party config library — keep it stdlib-only.
      Files: `internal/config/config.go`, `internal/config/config_test.go`
      Verify: `go test ./internal/config/...` — tests for missing required vars, defaults, and valid full config all pass.

- [ ] 3. **Database connection pool: internal/database/db.go**
      Wrap `pgxpool.New` with `DB_MAX_CONNS` / `DB_MIN_CONNS` applied via `pgxpool.Config`. Export `Pool *pgxpool.Pool`. Add `Ping` helper. Decision: use `pgxpool` (built into pgx/v5) rather than `database/sql` so callers get typed scan helpers and batch support without extra adapters.
      Files: `internal/database/db.go`
      Verify: `go build ./internal/database/...`

- [ ] 4. **Database migrations: migrations/*.sql**
      Write the exact SQL from the spec verbatim. Add a lightweight `RunMigrations(pool)` function in `internal/database/migrate.go` that executes files in lexicographic order using `pgx` directly (no external migration tool needed at this scale). Each file is idempotent via `IF NOT EXISTS`.
      Files: `migrations/001_create_jobs.sql`, `migrations/002_create_audit_logs.sql`, `migrations/003_create_failed_calls.sql`, `internal/database/migrate.go`
      Verify: `go build ./internal/database/...`

- [ ] 5. **Security — HMAC middleware and nonce: internal/security/hmac.go, nonce.go, encryption.go + tests**
      `hmac.go`: canonical string `METHOD\nPATH\nTIMESTAMP\nNONCE\nSHA256(BODY)`. Compute `HMAC-SHA256(secret, canonical)` and compare with `hmac.Equal` for constant-time comparison. Reject if `|now − timestamp| > tolerance`. Two client keys: `coordinator` (full) and `engineer` (GET only).
      `nonce.go`: `sync.Map` storing `nonce → expiry`; background goroutine purges expired entries every minute.
      `encryption.go`: stub `Encrypt`/`Decrypt` using AES-GCM for any future at-rest encryption needs.
      Files: `internal/security/hmac.go`, `internal/security/nonce.go`, `internal/security/encryption.go`, `internal/security/hmac_test.go`
      Verify: `go test ./internal/security/...` — all six test cases (valid, invalid sig, expired timestamp, nonce replay, unknown client, read-only violation) pass.

- [ ] 6. **Resilience — retry and rate limiter: internal/resilience/retry.go, rate_limiter.go + tests**
      `retry.go`: `DoWithRetry(ctx, cfg, fn)` — exponential backoff with full jitter, max attempts cap, respects context cancellation. Retries on net errors and HTTP 500/502/503/504. Does NOT retry on 401/403/400.
      `rate_limiter.go`: wraps `DoWithRetry`; on HTTP 429 reads `Retry-After` (seconds or HTTP-date), sleeps that duration (respecting context), then retries the call — 429 does NOT consume an attempt from the retry budget.
      Files: `internal/resilience/retry.go`, `internal/resilience/rate_limiter.go`, `internal/resilience/retry_test.go`, `internal/resilience/rate_limiter_test.go`
      Verify: `go test ./internal/resilience/...` — all mock-server tests pass.

- [ ] 7. **Audit: internal/audit/models.go, repository.go, service.go**
      `models.go`: `AuditLog` struct matching `audit_logs` table columns; `AuditEvent` constants for categories and types.
      `repository.go`: `Insert(ctx, log AuditLog)` using pgx named args. Fire-and-forget pattern — log write failures to stderr but never fail the calling request.
      `service.go`: `AuditService` wrapping the repo; convenience methods `LogRequest`, `LogJobEvent`, `LogAuthEvent`.
      Files: `internal/audit/models.go`, `internal/audit/repository.go`, `internal/audit/service.go`
      Verify: `go build ./internal/audit/...`

- [ ] 8. **DocuSign authentication: internal/docusign/auth.go**
      `JWTAuthClient`: builds a JWT (RS256, `iss=integration_key`, `sub=user_id`, `aud=auth_server`, 1-hour exp) signed with RSA private key loaded from `DOCUSIGN_PRIVATE_KEY_PATH`. POST to `{auth_server}/oauth/token` with `grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer`. Call `/oauth/userinfo` to resolve `base_uri` and `account_id`. Cache token until 5 min before expiry using a mutex-protected field. Never log the token, key, or secret.
      Uses `github.com/golang-jwt/jwt/v5` for signing, stdlib `crypto/rsa` for key parsing.
      Files: `internal/docusign/auth.go`
      Verify: `go build ./internal/docusign/...`

- [ ] 9. **DocuSign API client: internal/docusign/client.go, envelopes.go, recipients.go, documents.go, templates.go**
      `client.go`: `DSClient` struct holding `*AuthClient`, base URI, HTTP client with configured timeout and max-response-size reader. All methods call through the resilience layer.
      `envelopes.go`: `ListEnvelopes(ctx, accountID, from, to, startPos, count)` → parses response fields `envelopes[]`, `resultSetSize`, `totalSetSize`, `nextUri` / `endPosition`. Returns a page result with next cursor.
      `recipients.go`: `GetRecipients(ctx, accountID, envelopeID)` → parses `signers`, `carbonCopies`, `certifiedDeliveries`, etc.
      `documents.go`: `GetDocuments(ctx, accountID, envelopeID)` → parses `envelopeDocuments[]`.
      `templates.go`: `ListTemplates(ctx, accountID, startPos, count)` → parses `envelopeTemplates[]`, handles pagination.
      Decision: parse only the fields defined in the normalized structs — no `interface{}` catch-alls. Unknown fields silently ignored via `json:",omitempty"`.
      Files: `internal/docusign/client.go`, `internal/docusign/envelopes.go`, `internal/docusign/recipients.go`, `internal/docusign/documents.go`, `internal/docusign/templates.go`
      Verify: `go build ./internal/docusign/...`

- [ ] 10. **Normalization: internal/normalization/envelopes.go, recipients.go, documents.go, templates.go + tests**
       Each file defines the normalized struct (matching the spec exactly) and a `Normalize*(raw)` function that maps DocuSign API response structs to normalized structs. All timestamps parsed from DocuSign ISO-8601 strings into `time.Time` with a shared helper. Null-safe string helpers used throughout.
       Files: `internal/normalization/envelopes.go`, `internal/normalization/recipients.go`, `internal/normalization/documents.go`, `internal/normalization/templates.go`, `internal/normalization/*_test.go`
       Verify: `go test ./internal/normalization/...` — all normalization unit tests pass.

- [ ] 11. **Storage: internal/storage/minio.go, local.go**
       `minio.go`: `MinIOStorage` wrapping `minio.Client`. Methods: `EnsureBucket`, `UploadParquet(ctx, objectKey, data)`, `UploadJSON(ctx, objectKey, data)`. Object path template: `docusign/{type}/organization_id={org_id}/processing_date={YYYY-MM-DD}/{type}.parquet`. Decision: use `minio.PutObject` with a `bytes.Reader` — avoids temp files and keeps memory usage bounded by batch size.
       `local.go`: `LocalStorage` writing files to `LOCAL_OUTPUT_DIR/{job_id}/{type}.{ext}`. Creates directories as needed.
       Both implement a common `Storage` interface with `WriteParquet` and `WriteJSON` methods so the jobs layer is storage-agnostic.
       Files: `internal/storage/minio.go`, `internal/storage/local.go`
       Verify: `go build ./internal/storage/...`

- [ ] 12. **Parquet writing helper: internal/storage/parquet_writer.go**
       Uses `github.com/parquet-go/parquet-go`'s `parquet.Write[T]` generic function to serialize a slice of normalized structs to a `bytes.Buffer`. Parquet schema is inferred from struct tags (`parquet:"name,..."` tags added to all normalized structs in step 10). Decision: use reflection-based schema inference (struct tags) rather than hand-coding schema — simpler to maintain and the library supports it well.
       Files: `internal/storage/parquet_writer.go`
       Verify: `go build ./internal/storage/...`

- [ ] 13. **Jobs data layer: internal/jobs/models.go, repository.go, checkpoint.go**
       `models.go`: `Job` struct matching `jobs` table; `JobStatus` and `JobStage` string constants; `JobCheckpoint` struct.
       `repository.go`: CRUD methods — `Create`, `GetByID`, `UpdateStatus`, `UpdateHeartbeat`, `UpdateCounts`, `List`, `ListByStatus`, `MarkCrashed` (updates jobs with `last_heartbeat < now - crash_timeout` and status IN running stages to CRASHED), `Delete`. All use pgx named parameter queries.
       `checkpoint.go`: `UpsertCheckpoint`, `GetCheckpoint` using the `UNIQUE(job_id, stage)` constraint and `ON CONFLICT DO UPDATE`.
       Files: `internal/jobs/models.go`, `internal/jobs/repository.go`, `internal/jobs/checkpoint.go`
       Verify: `go build ./internal/jobs/...`

- [ ] 14. **Jobs service and worker: internal/jobs/service.go, worker.go**
       `service.go`: `JobService` orchestrating one full extraction job: fetch envelopes (paginated, checkpointed), fetch recipients+documents per envelope, fetch templates, normalize, write parquet+json to local, optionally upload to MinIO. Updates job status and counts throughout. Records failed calls to `failed_external_calls` table.
       `worker.go`: `WorkerPool` with a buffered channel of size `MAX_CONCURRENT_JOBS`. On `Start()`: queries PENDING jobs and enqueues them, marks stale in-progress jobs as CRASHED. Each worker goroutine runs a heartbeat ticker. `Shutdown(ctx)` drains the channel and waits for running jobs with a deadline.
       Files: `internal/jobs/service.go`, `internal/jobs/worker.go`
       Verify: `go build ./internal/jobs/...`

- [ ] 15. **HTTP middleware: internal/httpapi/middleware/hmac.go, logging.go, recovery.go**
       `hmac.go`: Chi middleware. Skips `/api/health` and `/api/stats`. Reads `X-DS-Signature`, `X-DS-Timestamp`, `X-DS-Client-ID`, `X-DS-Nonce` headers. Calls `security.VerifyRequest`. Stores client ID in request context.
       `logging.go`: Structured request/response logging via `slog`. Logs method, path, status, latency, client ID from context.
       `recovery.go`: Catches panics, logs them, returns 500.
       Files: `internal/httpapi/middleware/hmac.go`, `internal/httpapi/middleware/logging.go`, `internal/httpapi/middleware/recovery.go`
       Verify: `go build ./internal/httpapi/...`

- [ ] 16. **HTTP handlers: internal/httpapi/handlers/*.go**
       `health.go`: GET `/api/health` — returns `{"status":"ok","version":"..."}`. GET `/api/stats` — queries job counts by status.
       `credentials.go`: POST `/api/validate-credentials` — calls `DSClient.ValidateCredentials()` (a no-side-effect token fetch + userinfo call), returns account info.
       `scan.go`: POST `/api/scan/start`, GET `/api/scan/{scan_id}/status`, POST `/api/scan/{scan_id}/cancel`, POST `/api/scan/{scan_id}/resume`, GET `/api/scan/list`, GET `/api/scan/statistics`, DELETE `/api/scan/{scan_id}/remove`.
       `normalization.go`: POST `/api/normalization/{scan_id}/normalize`, GET `/api/normalization/{scan_id}/tables`, GET `/api/normalization/supported-objects`.
       `maintenance.go`: POST `/api/maintenance/cleanup`, POST `/api/maintenance/detect-crashed`.
       `audit.go`: GET `/api/audit/logs`, GET `/api/audit/stats` — accepts query params for filtering.
       All handlers follow the pattern: decode request → validate → delegate to service → encode JSON response. No business logic in handlers.
       Files: `internal/httpapi/handlers/health.go`, `internal/httpapi/handlers/credentials.go`, `internal/httpapi/handlers/scan.go`, `internal/httpapi/handlers/normalization.go`, `internal/httpapi/handlers/maintenance.go`, `internal/httpapi/handlers/audit.go`
       Verify: `go build ./internal/httpapi/...`

- [ ] 17. **Router: internal/httpapi/routes.go**
       Wires Chi router with middleware stack (recovery → logging → hmac). Registers all routes from step 16. Returns `http.Handler`.
       Files: `internal/httpapi/routes.go`
       Verify: `go build ./internal/httpapi/...`

- [ ] 18. **Entry point: cmd/server/main.go**
       Calls `config.Load()`, connects DB, runs migrations, initializes all services, creates worker pool, wires router, starts `http.Server` with graceful shutdown on SIGINT/SIGTERM (drain worker pool, then close HTTP server).
       Files: `cmd/server/main.go`
       Verify: `go build ./cmd/server/` — binary compiles cleanly.

- [ ] 19. **Test suite**
       `internal/config/config_test.go` — already covered in step 2.
       `internal/security/hmac_test.go` — already covered in step 5.
       `internal/normalization/*_test.go` — already covered in step 10.
       `internal/resilience/retry_test.go`, `rate_limiter_test.go` — already covered in step 6.
       `internal/jobs/service_test.go` — uses a mock DocuSign HTTP server and an in-memory job repository; tests happy path, pagination resume, and cancellation.
       `tests/integration_test.go` — spins up the full router (no real DB/DocuSign), exercises all 14 HMAC-protected endpoints with a valid signed request, verifies 200/202/400/401 responses.
       Files: `internal/jobs/service_test.go`, `tests/integration_test.go`
       Verify: `go test ./...` — all tests pass.

- [ ] 20. **Scripts: scripts/sign_request_example.go**
       A standalone `main` package demonstrating how to sign a request with HMAC-SHA256 for the Coordinator. Reads `HMAC_SECRET_KEY_CORE` from env, prints the four required headers.
       Files: `scripts/sign_request_example.go`
       Verify: `go run scripts/sign_request_example.go` — prints header output without error.

- [ ] 21. **Docker and Compose: Dockerfile, docker-compose.yml**
       `Dockerfile`: multi-stage build — `golang:1.26-alpine` builder stage compiles binary, `alpine:3.21` runtime stage copies binary + CA certs. Non-root user. `EXPOSE 8080`.
       `docker-compose.yml`: services: `app` (this image, env_file .env), `postgres:16-alpine` (with healthcheck), `minio/minio:latest` (with healthcheck and `mc mb` init container).
       Files: `Dockerfile`, `docker-compose.yml`
       Verify: `docker build -t docusign-ms .` — image builds successfully.

- [ ] 22. **Documentation: README.md, DESIGN.md**
       `README.md`: prerequisites, DocuSign sandbox setup (create integration key, enable JWT, upload RSA public key, grant user consent), env var reference table, `docker-compose up` quickstart, endpoint reference.
       `DESIGN.md`: architecture overview, auth flow diagram (ASCII), job lifecycle state machine, HMAC signing protocol, storage path conventions, extension points.
       Files: `README.md`, `DESIGN.md`
       Verify: files exist and are non-empty.

---

## Dependency Graph

```
1 (go.mod)
  └─ 2 (config)
       └─ 3 (database/db)
            └─ 4 (migrations)
  └─ 5 (security)
  └─ 6 (resilience)
  └─ 7 (audit)          ← needs 3
  └─ 8 (docusign/auth)  ← needs 6
       └─ 9 (docusign/client) ← needs 6, 8
  └─ 10 (normalization)
  └─ 11+12 (storage)    ← needs 10
  └─ 13 (jobs/models+repo) ← needs 3
       └─ 14 (jobs/service+worker) ← needs 9, 10, 11, 13
  └─ 15 (middleware)    ← needs 5
       └─ 16 (handlers) ← needs 14, 7
            └─ 17 (routes)
                 └─ 18 (main) ← needs all
  19 (tests)            ← needs all
  20 (scripts)          ← standalone
  21 (docker)           ← needs 18
  22 (docs)
```

---

## Tricky Compatibility Notes

1. **`parquet-go/parquet-go` is pre-v1** — the `parquet.Write[T]` generic API is stable but the schema tag syntax uses `parquet:"name"` on struct fields. Use `*string` for nullable string fields and `*time.Time` for nullable timestamps (the library maps these to optional Parquet columns).

2. **`pgx/v5` named parameters** — pgx v5 uses `@param_name` syntax in queries (not `$1`) when using `pgx/v5/pgconn` named args. Alternatively use positional `$N` — choose positional consistently throughout to avoid confusion.

3. **Go 1.26 generics** — `parquet.Write[T]` requires the concrete type at compile time. All four normalized types must be written as separate calls. A generic helper `WriteParquet[T any](slice []T) ([]byte, error)` works cleanly in Go 1.26.

4. **DocuSign JWT audience** — The `aud` claim for JWT Grant must be the hostname only (e.g. `account-d.docusign.com`), not a full URL. The grant type string is `urn:ietf:params:oauth:grant-type:jwt-bearer` (exact).

5. **MinIO SDK v7 bucket creation** — `MakeBucket` returns an error if the bucket already exists; call `BucketExists` first or use `EnsureBucket` pattern that ignores `BucketAlreadyOwnedByYou`.

6. **Chi v5 URL params** — Use `chi.URLParam(r, "scan_id")` not `r.URL.Query().Get("scan_id")` for path parameters.

7. **`sync.Map` nonce TTL** — The background cleanup goroutine must be started with `go` in the `NewNonceStore()` constructor and stopped via a context to avoid goroutine leaks in tests. Pass a `context.Context` to the constructor.
