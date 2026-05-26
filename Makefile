GO ?= go
SQLC_PATHS := sqlc.yaml internal/backend/agentruntime/sqlc internal/backend/agentruntime/dbsqlc

.PHONY: fmt test build sqlc-generate sqlc-check clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

sqlc-generate:
	$(GO) generate ./internal/backend/agentruntime/dbsqlc

sqlc-check: sqlc-generate
	git diff --exit-code -- $(SQLC_PATHS)

run-tui:
	rm -f tui && $(GO) build -o tui ./cmd/tui && ./tui

run-agentapi:
	rm -f agentapi && $(GO) build -o agentapi ./cmd/agentapi && ./agentapi

clean:
	rm -rf tui agentapi
