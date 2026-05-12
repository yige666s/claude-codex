GO ?= go

.PHONY: fmt test build clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

run-tui:
	rm -f tui && $(GO) build -o tui ./cmd/tui && ./tui

run-agentapi:
	rm -f agentapi && $(GO) build -o agentapi ./cmd/agentapi && ./agentapi

clean:
	rm -rf tui agentapi
