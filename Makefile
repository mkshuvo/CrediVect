.PHONY: up down build test lint proto

up:
	docker compose up -d

down:
	docker compose down

build:
	docker compose build

test:
	@echo "Running Python Scoring Tests..."
	cd services/scoring && python -m unittest discover tests
	@echo "Running Go Core Tests..."
	cd services/core && go test ./...
