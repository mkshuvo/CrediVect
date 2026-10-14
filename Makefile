.PHONY: up down build rebuild ps logs health test test-go test-scoring bench e2e idp seed train proto migrate

# Same overrides docker compose reads; gitignored, so fall back to the defaults.
-include .env
CORE_PORT ?= $(if $(CORE_HOST_PORT),$(CORE_HOST_PORT),8000)

# cmd/e2e is a client of the running stack, so it must dial the port the stack
# actually published. Exported rather than written into the recipe so it works
# under either shell, and `?=` so an explicit CORE_URL still wins.
export CORE_URL ?= http://localhost:$(CORE_PORT)

# Long-running stack. Prefer `make build` plus targeted recreates over down/up.
up:
	docker compose up -d

down:
	docker compose down

build:
	docker compose build

rebuild:
	docker compose build core scoring ui
	docker compose up -d --force-recreate scoring core ui

ps:
	docker compose ps

logs:
	docker compose logs --tail=80 core scoring

health:
	curl -sS http://localhost:$(CORE_PORT)/api/v1/health

# Applies scripts/init-db.sql to the running postgres. The script is idempotent.
migrate:
	docker compose exec -T postgres psql -U credivect_user -d credivect -v ON_ERROR_STOP=1 < scripts/init-db.sql

# Seeds users, the active model version and a spread of applications across all
# risk tiers and statuses by running the real saga. Override the size with
# SEED_APPLICATIONS=40 make seed
seed:
	docker compose run --rm seed

# Retrains the PD model from the fixed seed and writes the artifacts back into
# services/scoring/engine/models, which core also reads as its PSI baseline.
# Needed once on a clean checkout, because the artifacts are not committed.
train:
	docker compose build scoring
	docker compose run --rm trainer

# Regenerates the gRPC stubs from proto/. Requires the scoring image.
proto:
	docker run --rm -v "$(CURDIR):/work" -w /work credivect-scoring:latest \
		python -m grpc_tools.protoc -I proto \
		--python_out=services/scoring/gen \
		--grpc_python_out=services/scoring/gen \
		proto/credivect/scoring/v1/scoring.proto

test: test-scoring test-go

test-scoring:
	docker compose run --rm --no-deps scoring python -m unittest discover -s tests -t . -v

test-go:
	cd services/core && go test ./...

# The in-process cost of the request path, with no I/O: hashing, blind indexes,
# money arithmetic, key generation and redaction. Run longer with
# BENCHTIME=1s make bench
BENCHTIME ?= 1000x
bench:
	cd services/core && go test -run '^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) ./internal/...

# Drives one complete loan through the running stack and asserts the promises
# that cross a service boundary: the saga's decision, the three read
# capacities, the FCRA notice, the ledger chain and the scoring SLA. Needs
# `make up` and `make seed` first.
e2e:
	cd services/core && go run ./cmd/e2e

# Serves a development identity provider on 127.0.0.1:9355, so delegated
# authentication can be exercised without an Auth0 tenant. Run it in a second
# terminal, recreate core with AUTH_MODE=oidc pointed at it, then run
# E2E_IDP_URL=http://host.docker.internal:9355 make e2e
idp:
	cd services/core && go run ./cmd/dev-idp
