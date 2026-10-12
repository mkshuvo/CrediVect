.PHONY: up down build rebuild ps logs health test test-go test-scoring seed train proto migrate

# Same overrides docker compose reads; gitignored, so fall back to the defaults.
-include .env
CORE_PORT ?= $(if $(CORE_HOST_PORT),$(CORE_HOST_PORT),8000)

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
