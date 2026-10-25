# DocuSign Master Service

A production-oriented Go service that extracts envelope, recipient, document, and template data from the DocuSign eSignature REST API, normalises it, persists it to PostgreSQL, and uploads output files (Parquet and JSON) to MinIO object storage.

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [DocuSign Sandbox Setup](#docusign-sandbox-setup)
4. [Configuration](#configuration)
5. [Quickstart](#quickstart)
6. [API Reference](#api-reference)
7. [HMAC Signing](#hmac-signing)
8. [Running Tests](#running-tests)

---

## Overview

**DocuSign Master Service** runs as a background-worker / REST API hybrid:

- Clients submit extraction jobs via the REST API.
- The worker pool picks up jobs and fetches data from DocuSign in pages.
- Fetched records are normalised and written to Parquet or JSON files.
- Completed output files are uploaded to a MinIO bucket.
- All job state, checkpoints, and audit events are persisted in PostgreSQL.

High-level components:

```
Client → [HTTP API] → [Job Queue]
                          ↓
                     [Worker Pool]
                      /         \
              [DocuSign API]  [PostgreSQL]
                              [MinIO]
```

---

## Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.26+ |
| Docker | 24+ |
| Docker Compose | v2+ |
| DocuSign Developer Account | [Sign up free](https://developers.docusign.com/) |

---

## DocuSign Sandbox Setup

The service uses **JWT Grant** for unattended server-to-server authentication. Follow these steps once in the DocuSign developer sandbox.

### Step 1 — Create an Integration Key

1. Log in to the [DocuSign Developer Console](https://admindemo.docusign.com/apps-and-keys).
2. Click **Add App and Integration Key**.
3. Give your app a descriptive name (e.g. `docusign-master-service`).
4. Copy the generated **Integration Key** (a UUID). This is your `DOCUSIGN_INTEGRATION_KEY`.

### Step 2 — Enable JWT Grant

1. On the app's detail page, scroll to **Authentication**.
2. Click **Add URI** under **Redirect URIs** — set it to `https://example.com` (only needed for the one-time consent step).
3. Under **Service Integration / JWT Grant**, click **Generate RSA Keypair** — or use the keypair you generate in Step 3.

### Step 3 — Generate an RSA Keypair

If you want to generate the keys locally instead of using the DocuSign UI:

```bash
# Generate 2048-bit RSA private key
openssl genrsa -out private.pem 2048

# Extract the matching public key
openssl rsa -in private.pem -pubout -out public.pem
```

Keep `private.pem` secret — it must **never** be committed to version control.

### Step 4 — Upload the Public Key

1. Back in the DocuSign developer console, on the app page under **RSA Keypairs**, click **Add RSA Keypair**.
2. Paste the contents of `public.pem` into the provided field.
3. Save. DocuSign now trusts JWTs signed by the corresponding private key.

### Step 5 — Grant User Consent (one-time)

JWT Grant requires that the DocuSign user has granted consent to the integration key. Open the following URL in a browser (substitute your values):

```
https://{DOCUSIGN_AUTH_SERVER}/oauth/auth?response_type=code&scope=signature%20impersonation&client_id={DOCUSIGN_INTEGRATION_KEY}&redirect_uri=https://example.com
```

- **Sandbox auth server**: `account-d.docusign.com`
- **Production auth server**: `account.docusign.com`

Example for sandbox:

```
https://account-d.docusign.com/oauth/auth?response_type=code&scope=signature%20impersonation&client_id=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx&redirect_uri=https://example.com
```

Log in with the DocuSign user whose ID you will put in `DOCUSIGN_USER_ID`, click **Allow Access**. The browser will redirect to `https://example.com` — ignore the error page; the consent has been recorded.

### Step 6 — Locate Your User ID

1. In the developer console, click your avatar / name (top right).
2. Select **My Profile**.
3. Copy the **API Username** (a UUID). This is your `DOCUSIGN_USER_ID`.

### Step 7 — Configure the Service

Copy `private.pem` to a secure path on the server (e.g. `/run/secrets/docusign_private.pem`) and set:

```env
DOCUSIGN_INTEGRATION_KEY=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
DOCUSIGN_USER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
DOCUSIGN_PRIVATE_KEY_PATH=/run/secrets/docusign_private.pem
DOCUSIGN_AUTH_SERVER=account-d.docusign.com
```

---

## Configuration

Copy `.env.example` to `.env` and fill in the values. All variables are read at startup; the service exits with a clear error if any required variable is missing.

| Variable | Required | Default | Description |
|---|---|---|---|
| `APP_ENV` | | `development` | Deployment environment: `development`, `staging`, `production` |
| `PORT` | | `8080` | HTTP listen port |
| `LOG_LEVEL` | | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `DATABASE_URL` | ✓ | — | PostgreSQL connection URL |
| `DB_MAX_CONNS` | | `10` | Max connection pool size |
| `DB_MIN_CONNS` | | `2` | Min idle connections |
| `DOCUSIGN_INTEGRATION_KEY` | ✓ | — | Integration key (client ID) from DocuSign console |
| `DOCUSIGN_USER_ID` | ✓ | — | DocuSign user UUID to impersonate |
| `DOCUSIGN_PRIVATE_KEY_PATH` | ✓ | — | Path to RSA private key file (.pem) |
| `DOCUSIGN_AUTH_SERVER` | ✓ | — | OAuth server hostname (e.g. `account-d.docusign.com`) |
| `DOCUSIGN_OAUTH_SCOPES` | | `signature impersonation` | Space-separated OAuth scopes |
| `DOCUSIGN_ACCOUNT_ID` | | _(auto-resolved)_ | DocuSign account UUID; resolved automatically if blank |
| `DOCUSIGN_API_BASE_URI` | | _(auto-resolved)_ | eSignature REST API base URI; resolved automatically if blank |
| `DOCUSIGN_REQUEST_TIMEOUT_SECONDS` | | `30` | Timeout per DocuSign API request |
| `DOCUSIGN_PAGE_SIZE` | | `100` | Envelopes per page fetched from DocuSign |
| `RATE_LIMIT_MAX_WAIT_SECONDS` | | `300` | Max total wait budget for rate-limited requests |
| `RATE_LIMIT_FALLBACK_DELAY_SECONDS` | | `60` | Fallback delay when `Retry-After` header is absent |
| `RETRY_MAX_ATTEMPTS` | | `5` | Max retry attempts for transient failures |
| `RETRY_BASE_DELAY_MS` | | `500` | Base exponential-backoff delay (ms) |
| `RETRY_MAX_DELAY_MS` | | `30000` | Cap on exponential-backoff delay (ms) |
| `MINIO_ENDPOINT` | ✓ | — | MinIO server endpoint (`host:port`) |
| `MINIO_ACCESS_KEY` | ✓ | — | MinIO access key |
| `MINIO_SECRET_KEY` | ✓ | — | MinIO secret key |
| `MINIO_BUCKET_NAME` | | `docusign-output` | Bucket where output files are stored |
| `MINIO_USE_SSL` | | `false` | Use TLS for MinIO connections |
| `HMAC_SECRET_KEY_CORE` | ✓ | — | Hex-encoded 32-byte HMAC secret for request signing |
| `HMAC_TIMESTAMP_TOLERANCE_SECONDS` | | `300` | Max clock skew allowed in signed requests |
| `MAX_CONCURRENT_JOBS` | | `3` | Max simultaneously running extraction jobs |
| `MAX_CONCURRENT_DOCUSIGN_REQUESTS` | | `5` | Max parallel DocuSign calls per job |
| `HEARTBEAT_INTERVAL_SECONDS` | | `30` | How often running jobs update their heartbeat |
| `CRASH_TIMEOUT_MINUTES` | | `10` | Minutes without a heartbeat before a job is marked CRASHED |
| `LOCAL_OUTPUT_DIR` | | `/tmp/docusign-output` | Local directory for Parquet/JSON output files |

---

## Quickstart

```bash
# 1. Copy and configure environment
cp .env.example .env
# Edit .env — fill in DocuSign credentials and adjust secrets.

# 2. Start all services
docker compose up -d

# 3. Verify the service is healthy
curl http://localhost:8080/api/health
```

The `minio-init` service creates the output bucket automatically. The service applies database migrations on startup.

---

## API Reference

All endpoints (except `/api/health` and `/api/stats`) require HMAC-signed requests. See [HMAC Signing](#hmac-signing) for details.

### Public endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/health` | None | Service liveness and dependency health |
| `GET` | `/api/stats` | None | Aggregate job statistics |

### Credentials

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/validate-credentials` | HMAC | Validate DocuSign credentials without starting a job |

### Scan / Job management

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/scan/start` | HMAC | Submit a new extraction job |
| `GET` | `/api/scan/list` | HMAC | List all jobs (filterable by status) |
| `GET` | `/api/scan/statistics` | HMAC | Detailed job statistics |
| `GET` | `/api/scan/{scan_id}/status` | HMAC | Get the status of a specific job |
| `POST` | `/api/scan/{scan_id}/cancel` | HMAC | Cancel a running job |
| `POST` | `/api/scan/{scan_id}/resume` | HMAC | Resume a paused or failed job |
| `DELETE` | `/api/scan/{scan_id}/remove` | HMAC | Remove a completed or failed job record |

### Normalization

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/normalization/supported-objects` | HMAC | List normalizable DocuSign object types |
| `POST` | `/api/normalization/{scan_id}/normalize` | HMAC | Trigger normalization for a completed scan |
| `GET` | `/api/normalization/{scan_id}/tables` | HMAC | List output tables produced by normalization |

### Maintenance

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/maintenance/cleanup` | HMAC | Remove old completed/failed job records |
| `POST` | `/api/maintenance/detect-crashed` | HMAC | Mark jobs with stale heartbeats as CRASHED |

### Audit

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/audit/logs` | HMAC | Paginated audit log entries |
| `GET` | `/api/audit/stats` | HMAC | Audit log aggregate statistics |

---

## HMAC Signing

All protected endpoints require four custom HTTP headers:

| Header | Description |
|---|---|
| `X-Client-ID` | Client identifier. Use `coordinator` for full access or `engineer` for read-only. |
| `X-Timestamp` | Unix epoch seconds as a decimal string (e.g. `1715000000`). |
| `X-Nonce` | A unique random hex string per request (prevents replay). |
| `X-Signature` | Hex-encoded HMAC-SHA256 of the canonical string (see below). |

### Canonical string

The signature covers a newline-joined string:

```
METHOD\nPATH\nTIMESTAMP\nNONCE\nSHA256HEX(BODY)
```

Where:
- `METHOD` is the uppercase HTTP method (`GET`, `POST`, etc.)
- `PATH` is the URL path without query string (e.g. `/api/scan/start`)
- `TIMESTAMP` is the Unix timestamp string from `X-Timestamp`
- `NONCE` is the value from `X-Nonce`
- `SHA256HEX(BODY)` is the lowercase hex SHA-256 of the raw request body (use the SHA-256 of an empty string for requests with no body)

The HMAC secret is the value of `HMAC_SECRET_KEY_CORE` from your `.env`.

### Client roles

| Client ID | Permitted methods |
|---|---|
| `coordinator` | All methods |
| `engineer` | `GET` only |

### Example

Generate and print all required headers using the included example script:

```bash
# GET request with no body
HMAC_SECRET_KEY_CORE=your-secret go run scripts/sign_request_example.go GET /api/scan/list

# POST request with JSON body
HMAC_SECRET_KEY_CORE=your-secret go run scripts/sign_request_example.go POST /api/scan/start '{"organization_id":"org-1","date_from":"2024-01-01T00:00:00Z","date_to":"2024-12-31T23:59:59Z"}'
```

The script prints the four headers and a ready-to-run `curl` command.

---

## Running Tests

All tests use mock HTTP servers — no live DocuSign credentials are required.

```bash
# Run all tests
go test ./...

# Run a specific package
go test ./internal/security/...

# Run with verbose output
go test -v ./...

# Run with race detector
go test -race ./...
```

Build verification:

```bash
go build ./...
docker build -t docusign-ms .
```
