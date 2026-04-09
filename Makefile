GO ?= go

.PHONY: fmt test build clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

build-tui:
	$(GO) build -o tui ./cmd/tui

build-webui:
	$(GO) build -o webui ./cmd/webui

clean:
	rm -rf tui webui
