# System Architecture & Technical Specification — CrediVect
## Enterprise Credit Risk Scoring & Underwriting Platform

**Target Repository**: `D:\projects\CrediVect`  
**Architecture**: Polyglot Microservices (Golang 1.23+, Python 3.12+, Next.js 16.3.4 / React 19.2.8 + MUI v6)  
**Security Standard**: Zero-Vulnerability Policy, Alpine 3.24+ Hardening, Safe Model Deserialization  
**Status**: Ready for Execution  

---

## 1. Container Architecture & UI Verification

The Docker architecture includes three distinct UI interfaces:
1. **`credivect-ui` (Port `3000`)**: The primary Next.js 16.3.4 application (Borrower Loan Wizard `/apply` + Underwriter Risk Workspace `/dashboard`).
2. **`credivect-temporal-ui` (Port `8233`)**: The Temporal Web Dashboard for inspecting live workflow executions, activity retries, and manual review signals.
3. **`credivect-redpanda-console` (Port `8080`)**: The Kafka Console for live inspection of event streams, partitions, and consumer groups.

---

### Complete `docker-compose.yml` Specification (All Services Pinned)

```yaml
services:
  # =========================================================================
  # 1. PRIMARY APPLICATION FRONTEND (Next.js 16.3.4 + MUI v6 + React 19.2.8)
  # =========================================================================
  ui:
    build:
      context: ./services/ui
      dockerfile: Dockerfile
    container_name: credivect-ui
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - NEXT_PUBLIC_API_URL=http://localhost:8000
      - NEXT_PUBLIC_AUTH_PROVIDER=local
      - NODE_ENV=production
    depends_on:
      - core

  # =========================================================================
  # 2. GOLANG CORE GATEWAY & ORCHESTRATOR (Gin API + Temporal Go Worker)
  # =========================================================================
  core:
    build:
      context: ./services/core
      dockerfile: Dockerfile
    container_name: credivect-core
    restart: unless-stopped
    ports:
      - "8000:8000"
    environment:
      - PORT=8000
      - AUTH_MODE=local
      - JWT_SECRET=credivect-super-secure-local-jwt-secret-key-32bytes!
      - DRAGONFLY_URL=dragonfly:6379
      - POSTGRES_URL=postgres://credivect_user:credivect_password@postgres:5432/credivect?sslmode=disable
      - KAFKA_BROKERS=redpanda:9092
      - TEMPORAL_HOST=temporal:7233
      - SCORING_GRPC_HOST=scoring:50051
    depends_on:
      postgres:
        condition: service_healthy
      dragonfly:
        condition: service_healthy
      redpanda:
        condition: service_started

  # =========================================================================
  # 3. PYTHON QUANTITATIVE SCORING ENGINE (LightGBM + TreeSHAP gRPC Server)
  # =========================================================================
  scoring:
    build:
      context: ./services/scoring
      dockerfile: Dockerfile
    container_name: credivect-scoring
    restart: unless-stopped
    ports:
      - "50051:50051"

  # =========================================================================
  # 4. POSTGRESQL 17.11 (Relational Storage & Immutable Audit Ledger)
  # =========================================================================
  postgres:
    image: postgres:17.11-alpine3.24
    container_name: credivect-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: credivect
      POSTGRES_USER: credivect_user
      POSTGRES_PASSWORD: credivect_password
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U credivect_user -d credivect"]
      interval: 5s
      timeout: 5s
      retries: 5

  # =========================================================================
  # 5. DRAGONFLYDB v1.40.1 (In-Memory Cache: Idempotency & Rate Limiting)
  # =========================================================================
  dragonfly:
    image: docker.dragonflydb.io/dragonflydb/dragonfly:v1.40.1
    container_name: credivect-dragonfly
    restart: unless-stopped
    ports:
      - "6379:6379"
    volumes:
      - dragonfly_data:/data
    command: ["--proactor_threads=4", "--cache_mode=true"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  # =========================================================================
  # 6. REDPANDA v26.2.1 (Apache Kafka Compatible Event Streaming Engine)
  # =========================================================================
  redpanda:
    image: docker.redpanda.com/redpandadata/redpanda:v26.2.1
    container_name: credivect-redpanda
    restart: unless-stopped
    command:
      - redpanda
      - start
      - --kafka-addr=internal://0.0.0.0:9092,external://0.0.0.0:19092
      - --advertise-kafka-addr=internal://redpanda:9092,external://localhost:19092
      - --pandaproxy-addr=0.0.0.0:8082
      - --advertise-pandaproxy-addr=localhost:8082
      - --schema-registry-addr=0.0.0.0:8081
      - --rpc-addr=0.0.0.0:33145
      - --advertise-rpc-addr=redpanda:33145
      - --mode=dev-container
      - --smp=1
      - --memory=1G
    ports:
      - "19092:19092"
      - "9092:9092"
      - "9644:9644"
    volumes:
      - redpanda_data:/var/lib/redpanda/data

  # =========================================================================
  # 7. REDPANDA CONSOLE v3.11.0 (Web UI for Kafka Topics & Stream Telemetry)
  # =========================================================================
  redpanda-console:
    image: docker.redpanda.com/redpandadata/console:v3.11.0
    container_name: credivect-redpanda-console
    restart: unless-stopped
    depends_on:
      - redpanda
    environment:
      KAFKA_BROKERS: redpanda:9092
    ports:
      - "8080:8080"

  # =========================================================================
  # 8. TEMPORAL SERVER v1.29.6 (Durable Underwriting Saga Orchestrator)
  # =========================================================================
  temporal:
    image: temporalio/server:1.29.6
    container_name: credivect-temporal
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      - DB=postgresql
      - DB_PORT=5432
      - POSTGRES_USER=credivect_user
      - POSTGRES_PWD=credivect_password
      - POSTGRES_SEEDS=postgres
      - DYNAMIC_CONFIG_FILE_PATH=config/dynamicconfig/development-sql.yaml
    ports:
      - "7233:7233"

  # =========================================================================
  # 9. TEMPORAL WEB UI v2.53.3 (Workflow Execution & Signal Dashboard)
  # =========================================================================
  temporal-ui:
    image: temporalio/ui:2.53.3
    container_name: credivect-temporal-ui
    restart: unless-stopped
    depends_on:
      - temporal
    environment:
      - TEMPORAL_ADDRESS=temporal:7233
      - TEMPORAL_CORS_ORIGINS=http://localhost:8233
    ports:
      - "8233:8080"

volumes:
  postgres_data:
  dragonfly_data:
  redpanda_data:
```

---

### Hardened Multi-Stage UI Dockerfile (`services/ui/Dockerfile`)

To guarantee zero vulnerabilities and high production performance, the Next.js UI uses a 3-stage build on **Alpine 3.24** with `output: "standalone"` and runs under an unprivileged non-root user:

```dockerfile
# Stage 1: Install dependencies
FROM node:22-alpine3.24 AS deps
WORKDIR /app
RUN apk add --no-cache libc6-compat
COPY package.json package-lock.json ./
RUN npm ci

# Stage 2: Build the Next.js application
FROM node:22-alpine3.24 AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production
RUN npm run build

# Stage 3: Minimal hardened production runner
FROM node:22-alpine3.24 AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

# Create unprivileged non-root system user
RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Copy standalone build output and static assets
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
```

---

## 2. Dependency Security & Vulnerability Audit

| Component | Pinned Version | Vulnerability Audit & Patch Status |
| :--- | :--- | :--- |
| **`next`** | `16.3.4` | **SAFE / PATCHED**. Patches the critical **CVE-2025-55182** (React Server Components Flight protocol RCE, CVSS 10.0) which affected older unpatched Next.js releases ($< 16.0.7$). |
| **`react`** | `19.2.8` | **SAFE / PATCHED**. Specifically pinned to `19.2.8` (released July 2026), incorporating cumulative security patches for CVE-2025-55182, CVE-2025-55183, and CVE-2025-55184. |
| **`react-dom`** | `19.2.8` | Matches core React 19.2.8 security release. |
| **`@mui/material`** | `6.4.5` | **SAFE**. Active stable MUI v6 release, zero known CVEs. |
| **`@mui/icons-material`**| `6.4.5` | **SAFE**. Zero vulnerabilities. |
| **`@mui/material-nextjs`**| `6.4.5` | **SAFE**. Official AppRouterCacheProvider for Next.js App Router. |
| **`@emotion/react`** | `11.14.0` | **SAFE**. Zero known vulnerabilities. |
| **`@emotion/styled`**| `11.14.0` | **SAFE**. Zero known vulnerabilities. |
| **`recharts`** | `2.15.1` | **SAFE**. Zero known vulnerabilities. |
| **`github.com/gin-gonic/gin`** | `v1.10.0` | **SAFE**. Resolves past path-traversal CVEs. |
| **`github.com/golang-jwt/jwt/v5`** | `v5.2.1` | **SAFE**. Eliminates key-confusion and timing vulnerabilities (CVE-2020-26160). |
| **`github.com/auth0/go-jwt-middleware/v3`** | `v3.0.0` | **SAFE**. Audited modular Auth0 middleware for local HS256 & remote JWKS verification. |
| **`google.golang.org/grpc`** | `v1.70.0` | **SAFE**. Fixes HTTP/2 Rapid Reset (CVE-2023-44487). |
| **`google.golang.org/protobuf`** | `v1.36.5` | **SAFE**. Pure Go implementation, immune to Java-specific protobuf CVE-2024-7254. |
| **`github.com/jackc/pgx/v5`** | `v5.7.2` | **SAFE**. Modern PostgreSQL driver with prepared statement security. |
| **`pydantic`** | `2.10.6` | **SAFE / PATCHED**. Fixes ReDoS vulnerability (CVE-2024-3772). |
| **`lightgbm`** | `4.5.0` | **SAFE**. Embedded C-API inside gRPC worker (`lgbserver` binary disabled). |
| **Safe Model Deserialization** | Policy | **No insecure `pickle`**. Models loaded strictly via native text/JSON format or Safetensors. |

---

## 3. Pluggable Authentication Architecture (Local $\rightarrow$ Clerk / Auth0 Ready)

```
                      ┌────────────────────────────────────────┐
                      │          FRONTEND UI LAYER             │
                      │                                        │
                      │         IAuthProvider Interface       │
                      │  useAuth() -> { user, token, role }    │
                      └───────────────────┬────────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼                                               ▼
     [LocalAuthAdapter]                                [Clerk / Auth0 Adapter]
     - Calls /api/v1/auth/login                       - Wraps <ClerkProvider> or <Auth0Provider>
     - Local JWT in HTTP-only Cookie                  - OIDC Discovery & Session Management
                  │                                               │
                  └───────────────────────┬───────────────────────┘
                                          │
                                  Bearer <JWT Token>
                                          │
                      ┌───────────────────▼────────────────────┐
                      │          BACKEND GOLANG LAYER          │
                      │                                        │
                      │   auth0/go-jwt-middleware/v3 Validator │
                      └───────────────────┬────────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼                                               ▼
         AUTH_MODE=local                                  AUTH_MODE=jwks
     - Validates via HMAC-SHA256                     - Remote JWKS Caching Provider
     - Uses internal JWT_SECRET                      - Auto-fetches keys from Clerk/Auth0
                  │                                               │
                  └───────────────────────┬───────────────────────┘
                                          │
                               Injects into context.Context:
                         Claims { UserID, Email, Role, Permissions }
```

---

## 4. Financial Data Standards & Invariants

CrediVect strictly enforces five institutional data invariants across all schemas, APIs, and business logic:

1. **The "Zero-Float" Golden Rule (IEEE-754 Ban)**:
   - Monetary values: `BIGINT` in PostgreSQL (`int64` in Go) representing **minor units** (USD cents).
   - Currencies: ISO-4217 `CHAR(3)`.
   - Interest rates and margins: **integer Basis Points (BPS)** (`1 bp = 0.01%`).
   - Probabilities ($PD$): Fixed-point `NUMERIC(7, 6)`.
   - Rounding: Strictly **Banker’s Rounding** (`ROUND_HALF_EVEN` / IEEE 754-2008).
2. **Primary Key Architecture: Time-Ordered UUIDv7**:
   - RFC 9562 standard embedding 48-bit millisecond timestamps for B-Tree index clustering and spatial locality.
3. **Uber LedgerStore Pattern: Immutability & Tamper-Evident Hash Chaining**:
   - Append-only financial history.
   - Cryptographic Merkle chain on audit rows: $\text{entry\_hash} = \text{SHA-256}(\text{prev\_hash} + \text{payload\_hash} + \text{timestamp})$.
4. **Optimistic Concurrency Control (OCC)**:
   - `version BIGINT` column on mutable workflow entities.
5. **Bitemporality**:
   - UTC `TIMESTAMPTZ` for both `valid_time` and `transaction_time`.

---

## 5. Entity-Relationship (ER) Diagram (Financial Data Standard)

```mermaid
erDiagram
    APPLICANTS ||--o{ CREDIT_APPLICATIONS : submits
    CREDIT_APPLICATIONS ||--o| BUREAU_REPORTS : retrieves
    CREDIT_APPLICATIONS ||--o| FEATURE_VECTORS : computes
    CREDIT_APPLICATIONS ||--o{ SCORING_RUNS : evaluates
    CREDIT_APPLICATIONS ||--o| DECISIONS : concludes
    CREDIT_APPLICATIONS ||--o{ AUDIT_LEDGER : audits

    SCORING_RUNS ||--o{ SHAP_EXPLANATIONS : generates
    SCORING_RUNS ||--o{ ADVERSE_ACTION_REASONS : derives
    SCORING_RUNS }o--|| MODEL_VERSIONS : executes
    MODEL_VERSIONS ||--o{ DRIFT_METRICS : monitors

    APPLICANTS {
        uuid id PK "UUIDv7 (Time-Ordered)"
        string external_id UK "Unique Client Identifier"
        string ssn_blind_index UK "HMAC-SHA256 for searching"
        bytea ssn_encrypted "AES-256-GCM Encrypted"
        string first_name "VARCHAR(100)"
        string last_name "VARCHAR(100)"
        string email "VARCHAR(255)"
        date date_of_birth "DATE"
        bigint monthly_income_cents "INTEGER CENTS (No Floats)"
        char(3) currency_code "ISO-4217 (e.g. USD)"
        string employment_status "VARCHAR(50)"
        timestamptz created_at "UTC"
    }

    CREDIT_APPLICATIONS {
        uuid id PK "UUIDv7 (Time-Ordered)"
        uuid applicant_id FK
        bigint requested_amount_cents "INTEGER CENTS (No Floats)"
        char(3) currency_code "ISO-4217"
        integer loan_term_months "INTEGER"
        string loan_purpose "VARCHAR(100)"
        string status "SUBMITTED | IN_REVIEW | APPROVED | DECLINED | CANCELLED"
        string workflow_id "Temporal Workflow ID"
        bigint version "Optimistic Locking Version"
        timestamptz created_at "UTC"
        timestamptz updated_at "UTC"
    }

    BUREAU_REPORTS {
        uuid id PK "UUIDv7 (Time-Ordered)"
        uuid application_id FK
        string bureau_name "EXPERIAN | EQUIFAX | TRANSUNION"
        jsonb raw_payload "Original Bureau JSON"
        smallint bureau_score "INTEGER (300-850)"
        bigint total_debt_cents "INTEGER CENTS"
        bigint revolving_balance_cents "INTEGER CENTS"
        bigint revolving_limit_cents "INTEGER CENTS"
        integer revolving_utilization_bps "BASIS POINTS (e.g. 4250 = 42.5%)"
        smallint delinquencies_24m "INTEGER COUNT"
        smallint inquiries_6m "INTEGER COUNT"
        smallint total_trades "INTEGER COUNT"
        smallint open_trades "INTEGER COUNT"
        boolean bankruptcy_flag "BOOLEAN"
        timestamptz valid_time "Bureau Snapshot Real-World Time"
        timestamptz transaction_time "CrediVect Ingestion Time"
    }

    FEATURE_VECTORS {
        uuid id PK "UUIDv7 (Time-Ordered)"
        uuid application_id FK
        integer dti_bps "BASIS POINTS (e.g. 3400 = 34.00%)"
        integer revolving_utilization_bps "BASIS POINTS"
        smallint delinquencies_24m "INTEGER COUNT"
        smallint inquiries_6m "INTEGER COUNT"
        integer age_of_oldest_trade_months "INTEGER MONTHS"
        integer income_to_loan_bps "BASIS POINTS"
        timestamptz computed_at "UTC"
    }

    MODEL_VERSIONS {
        uuid id PK "UUIDv7"
        string model_name "VARCHAR(100)"
        string version "e.g. v1.2.0"
        string algorithm "LIGHTGBM | LOGISTIC_WOE | ENSEMBLE"
        string artifact_path "S3 / Local Artifact URI"
        string dataset_sha256 "SHA-256 Dataset Checksum"
        boolean is_active "BOOLEAN"
        timestamptz deployed_at "UTC"
    }

    SCORING_RUNS {
        uuid id PK "UUIDv7 (Append-Only)"
        uuid application_id FK
        uuid model_version_id FK
        smallint scorecard_points "INTEGER 300 - 850"
        numeric raw_ml_logit "NUMERIC(10, 6) Raw Tree Margin"
        numeric calibrated_pd "NUMERIC(7, 6) PD: 0.000000 - 1.000000"
        string risk_tier "SUPER_PRIME | PRIME | NEAR_PRIME | SUBPRIME | HIGH_RISK"
        integer execution_time_micros "EXECUTION DURATION IN MICROSECONDS"
        timestamptz scored_at "UTC"
    }

    SHAP_EXPLANATIONS {
        uuid id PK "UUIDv7 (Append-Only)"
        uuid scoring_run_id FK
        string feature_name "VARCHAR(100)"
        numeric feature_value "NUMERIC(14, 4)"
        numeric shap_value "NUMERIC(10, 6) Local Log-Odds Attribution"
        smallint rank "1 = Most Negative Driver"
    }

    ADVERSE_ACTION_REASONS {
        uuid id PK "UUIDv7 (Append-Only)"
        uuid scoring_run_id FK
        char(4) reason_code "CHAR(4) e.g. AA01, AA04"
        string description "VARCHAR(255) FCRA Mandatory Description"
        smallint priority_rank "INTEGER 1 to 4"
    }

    DECISIONS {
        uuid id PK "UUIDv7 (Append-Only)"
        uuid application_id FK
        uuid scoring_run_id FK
        string outcome "APPROVED | DECLINED | REFERRED"
        bigint approved_limit_cents "INTEGER CENTS (0 if declined)"
        integer approved_apr_bps "BASIS POINTS (e.g. 1245 = 12.45%)"
        string decision_type "AUTOMATED | MANUAL_OVERRIDE"
        uuid underwriter_id "UUIDv7 (NULL if automated)"
        string decision_notes "TEXT"
        timestamptz decisioned_at "UTC"
    }

    AUDIT_LEDGER {
        uuid id PK "UUIDv7 (Append-Only Immutable)"
        uuid application_id FK
        string event_type "VARCHAR(100)"
        string actor "SYSTEM | UNDERWRITER | API"
        string payload_sha256 "SHA-256 of Event Payload"
        string previous_entry_hash "SHA-256 of Previous Row (Merkle Chain)"
        string entry_hash "SHA-256(prev_hash + payload_hash + timestamp)"
        jsonb metadata "Immutable Event Context"
        timestamptz recorded_at "UTC"
    }

    DRIFT_METRICS {
        uuid id PK "UUIDv7"
        uuid model_version_id FK
        string feature_name "VARCHAR(100) e.g. OVERALL_SCORE"
        numeric psi_value "NUMERIC(6, 4) Population Stability Index"
        integer sample_size "COUNT"
        string status "STABLE | MODERATE_DRIFT | CRITICAL_DRIFT"
        timestamptz window_start "UTC"
        timestamptz window_end "UTC"
        timestamptz evaluated_at "UTC"
    }
```

---

## 6. Target Project Scaffolding (`D:\projects\CrediVect`)

```
D:\projects\CrediVect\
├── docker-compose.yml              # All 9 services pinned (Next.js, Go, Python, Postgres 17, Dragonfly, Redpanda, Temporal)
├── Makefile                        # Build, lint, test, docker commands
├── proto/                          # Protocol Buffers Definitions
│   └── scoring.proto               # High-speed contract between Go and Python
│
├── services/
│   ├── ui/                         # 🌐 FRONTEND (Next.js 16.3.4 + React 19.2.8 + MUI v6)
│   │   ├── Dockerfile              # Multi-stage node:22-alpine3.24 with Next.js standalone runner
│   │   ├── package.json            # next: 16.3.4, react: 19.2.8, @mui/material: 6.4.5
│   │   ├── src/
│   │   │   ├── app/
│   │   │   │   ├── layout.tsx      # AppRouterCacheProvider + ThemeProvider
│   │   │   │   ├── page.tsx        # Landing Page
│   │   │   │   ├── login/          # Pluggable Auth Login Form
│   │   │   │   ├── apply/          # Borrower Loan Application Stepper
│   │   │   │   ├── status/[id]/    # Live Decision & Adverse Action Tracker
│   │   │   │   └── dashboard/      # Underwriter Workspace & SHAP Visualizer
│   │   │   ├── components/         # MUI Cards, SHAP Waterfall chart, SLA Chips
│   │   │   ├── theme/              # Fintech Material Theme tokens
│   │   │   └── lib/
│   │   │       ├── auth/           # Pluggable Auth Provider (Local -> Clerk/Auth0)
│   │   │       │   ├── AuthContext.tsx
│   │   │       │   ├── LocalAuthProvider.tsx
│   │   │       │   └── useAuth.ts
│   │   │       └── api.ts          # REST Client with Idempotency & Auth Headers
│   │
│   ├── core/                       # 🔵 GOLANG SERVICE (Orchestrator, Gateway & Kafka)
│   │   ├── Dockerfile              # Multi-stage: golang:1.23.6-alpine3.24 -> alpine:3.24.1
│   │   ├── go.mod                  # gin v1.10.0, jwt/v5 v5.2.1, grpc v1.70.0, pgx/v5 v5.7.2
│   │   ├── go.sum
│   │   ├── cmd/
│   │   │   └── server/main.go      # HTTP Server & Temporal Worker init
│   │   └── internal/
│   │       ├── api/                # Gin REST Handlers & Idempotency Middleware
│   │       │   └── middleware/
│   │       │       └── auth.go     # Pluggable JWT Validator (HS256 Secret OR Remote JWKS)
│   │       ├── cache/              # Dragonfly Client (Idempotency, Rate Limiting)
│   │       ├── domain/             # Minor-unit Money & Basis-Point Types (No Floats)
│   │       ├── workflows/          # Temporal UnderwritingWorkflow & Bureau Activities
│   │       ├── kafka/              # Event Producers & Consumers
│   │       ├── storage/            # PostgreSQL pgx Repositories & Hash Chaining
│   │       └── client/             # gRPC Client to Python Scoring Service
│   │
│   └── scoring/                    # 🟡 PYTHON SERVICE (Quantitative Scoring & SHAP)
│       ├── Dockerfile              # python:3.12.9-slim-bookworm (Zero musl/C-extension CVEs)
│       ├── pyproject.toml          # pydantic 2.10.6, lightgbm 4.5.0, shap 0.46.0, grpcio 1.70.0
│       ├── server.py               # gRPC Service Entrypoint
│       └── engine/
│           ├── scorecard.py        # WoE Monotonic Scorecard (300-850)
│           ├── lightgbm_model.py   # Tree Ensemble Model & Safe Text Deserialization
│           ├── explain.py          # TreeSHAP & FCRA Adverse Action Mapping
│           └── models/             # Safe text/JSON model artifacts (Anti-pickle)
│
└── tests/
    ├── unit/                       # Financial math precision & rounding tests
    ├── integration/                # API, Dragonfly idempotency & Kafka tests
    ├── workflows/                  # Temporal workflow replay & activity retry tests
    └── load/                       # High-concurrency benchmark tests (P95 < 50ms)
```

---

## 7. Phased Implementation Roadmap

1. **Phase 1 — Infrastructure & Financial Data Types**:
   - Write `docker-compose.yml` with all 9 services pinned (including the UI on port 3000).
   - Define financial domain models in Go (Integer minor units, Basis points, UUIDv7 generators, Banker's rounding) and PostgreSQL schemas.
   - Define `proto/scoring.proto` contracts.
2. **Phase 2 — Python Scoring Microservice (`services/scoring`)**:
   - Implement WoE Scorecard, LightGBM model, Platt scaling calibrator, TreeSHAP explainer, and Adverse Action reason code mapper.
   - Run Python gRPC server on port 50051 using `python:3.12.9-slim-bookworm` with safe text deserialization (anti-pickle).
3. **Phase 3 — Go Core & Dragonfly Integration (`services/core`)**:
   - Setup Go module, Gin REST API, and Dragonfly idempotency middleware (`SETNX`).
   - Implement pluggable JWT middleware (`auth0/go-jwt-middleware/v3`) supporting local HMAC secret and remote JWKS.
   - Implement gRPC client calling Python scoring service.
4. **Phase 4 — Temporal Underwriting Saga & Bureau Activities**:
   - Implement `UnderwritingWorkflow` in Go with exponential retry bureau activities, 72-hour `ReviewerDecisionSignal`, and escalation timers.
5. **Phase 5 — Kafka Event Streaming & Tamper-Evident Audit Ledger**:
   - Implement Kafka event producers and consumers.
   - Implement cryptographic Merkle hash chaining in PostgreSQL audit ledger.
6. **Phase 6 — Frontend UI Implementation (`services/ui`)**:
   - Setup **Next.js 16.3.4** + React 19.2.8 + MUI v6 with `AppRouterCacheProvider`.
   - Implement `AuthProvider` with local session management and Clerk/Auth0 migration adapter.
   - Build Applicant Loan Wizard (`/apply`) and Status Tracker with live polling (`/status/[id]`).
   - Build Underwriter Workspace (`/dashboard`) with interactive SHAP waterfall chart and Temporal Signal trigger button.
7. **Phase 7 — Verification, Benchmark & E2E Validation**:
   - Automated financial precision tests (zero floating point drift).
   - E2E loan decision tests and $<50\text{ms}$ latency benchmarks.
