GO ?= go

.PHONY: fmt test build clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

run-tui:
	rm -f tui && $(GO) build -o tui ./cmd/tui && ./tui

run-webui:
	rm -f webui && $(GO) build -o webui ./cmd/webui && ./webui

clean:
	rm -rf tui webui
