GO ?= go
SQLC_PATHS := sqlc.yaml internal/backend/agentruntime/sqlc internal/backend/agentruntime/dbsqlc
LIVE_NOISE_CONFIG_PATHS := scripts/live_transcript_noise.json scripts/generate-live-transcript-noise-config.mjs internal/backend/agentruntime/live_noise_config_gen.go apps/web/src/features/workspace/liveTranscriptNoiseConfig.ts

.PHONY: fmt test build sqlc-generate sqlc-check live-noise-generate live-noise-check clean

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

sqlc-generate:
	$(GO) generate ./internal/backend/agentruntime/dbsqlc

sqlc-check: sqlc-generate
	git diff --exit-code -- $(SQLC_PATHS)

live-noise-generate:
	node scripts/generate-live-transcript-noise-config.mjs

live-noise-check: live-noise-generate
	git diff --exit-code -- $(LIVE_NOISE_CONFIG_PATHS)

run-tui:
	rm -f tui && $(GO) build -o tui ./cmd/tui && ./tui

run-agentapi:
	rm -f agentapi && $(GO) build -o agentapi ./cmd/agentapi && ./agentapi

clean:
	rm -rf tui agentapi
