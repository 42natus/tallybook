.PHONY: build test test-ci vet fmt fmt-check staticcheck lint check tidy up down up-all down-all backlog backlog-verify

GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

build:
	go build ./...

test:
	go test ./... -count=1

# CI must exercise the Postgres tests, rather than silently skip them.
test-ci:
	bash scripts/test-ci.sh

vet:
	go vet ./...

fmt:
	gofmt -l -w $(GOFILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFILES))" || (echo "gofmt: the following files need formatting:"; gofmt -l $(GOFILES); exit 1)

staticcheck:
	staticcheck ./...

# Runs everything CI runs, in the same order, so a local pass predicts a CI pass.
check: fmt-check vet staticcheck test

tidy:
	go mod tidy

# up/down: just Postgres — what the test suite needs (§8: integration
# tests against real Postgres, not the app binaries). docker-compose.yml
# also defines collector/settler/indexer services now; up-all/down-all
# bring up the full stack (needs .env filled in — see .env.example).
up:
	docker compose up -d postgres

down:
	docker compose down

up-all:
	docker compose up -d --build

down-all:
	docker compose down

# backlog: regenerate docs/WAVE2_BACKLOG.md from its JSON source of truth.
# The markdown is never hand-edited; this is the only way it changes.
backlog:
	python3 scripts/gen-wave-backlog.py

# backlog-verify: assert tier counts, point total, title uniqueness, no
# collision with any existing issue, field ranges, and that every file:line
# cited by every issue still resolves.
backlog-verify:
	bash scripts/verify-wave-backlog.sh
