package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/backend/agentruntime"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type seedItem struct {
	Prompt  agentruntime.PromptTemplate `json:"prompt"`
	Version agentruntime.PromptVersion  `json:"version"`
}

func main() {
	apiURL := flag.String("api-url", envDefault("AGENT_API_URL", "http://127.0.0.1:8081"), "AgentAPI base URL")
	adminToken := flag.String("admin-token", os.Getenv("AGENT_API_ADMIN_TOKEN"), "admin token; defaults to AGENT_API_ADMIN_TOKEN")
	userID := flag.String("user-id", envDefault("AGENT_API_SEED_USER_ID", "system-prompt-seed"), "audit user id")
	sqlDSN := flag.String("sql-dsn", os.Getenv("AGENT_API_SQL_DSN"), "optional Postgres DSN; when set, seed through SQLPromptStore instead of HTTP")
	dryRun := flag.Bool("dry-run", false, "print actions without writing")
	printInventory := flag.Bool("print-inventory", false, "print baseline inventory JSON and exit")
	flag.Parse()

	items := seedItems()
	if *printInventory {
		printJSON(items)
		return
	}
	if *dryRun {
		for _, item := range items {
			fmt.Printf("would seed %s@%s (%s)\n", item.Prompt.ID, item.Version.Version, item.Prompt.Scope)
		}
		return
	}
	if strings.TrimSpace(*sqlDSN) != "" {
		summary, err := seedSQL(context.Background(), *sqlDSN, items)
		if err != nil {
			fatalf("seed sql: %v", err)
		}
		printJSON(summary)
		return
	}
	if strings.TrimSpace(*adminToken) == "" {
		fatalf("admin token is required; pass -admin-token or set AGENT_API_ADMIN_TOKEN")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	summary := seedSummary{}
	for _, item := range items {
		if err := upsertPrompt(client, *apiURL, *adminToken, *userID, item.Prompt); err != nil {
			fatalf("upsert prompt %s: %v", item.Prompt.ID, err)
		}
		status, err := createVersion(client, *apiURL, *adminToken, *userID, item.Prompt.ID, item.Version)
		if err != nil {
			fatalf("create version %s@%s: %v", item.Prompt.ID, item.Version.Version, err)
		}
		switch status {
		case "created":
			summary.Created++
		case "exists":
			summary.Exists++
		}
	}
	summary.Total = len(items)
	printJSON(summary)
}

func seedSQL(ctx context.Context, dsn string, items []seedItem) (seedSummary, error) {
	db, err := sql.Open("pgx", strings.TrimSpace(dsn))
	if err != nil {
		return seedSummary{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return seedSummary{}, err
	}
	store := agentruntime.NewSQLPromptStoreWithDialect(db, agentruntime.SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		return seedSummary{}, err
	}
	summary := seedSummary{Total: len(items)}
	for _, item := range items {
		if _, err := store.UpsertPrompt(ctx, item.Prompt); err != nil {
			return seedSummary{}, fmt.Errorf("upsert prompt %s: %w", item.Prompt.ID, err)
		}
		if _, err := store.CreatePromptVersion(ctx, item.Version); err != nil {
			if isConflictError(err) {
				summary.Exists++
				continue
			}
			return seedSummary{}, fmt.Errorf("create version %s@%s: %w", item.Prompt.ID, item.Version.Version, err)
		}
		summary.Created++
	}
	return summary, nil
}

type seedSummary struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Exists  int `json:"exists"`
}

func seedItems() []seedItem {
	var items []seedItem
	for _, baseline := range agentruntime.BuiltinSystemPromptBaselines() {
		items = append(items, seedItem{Prompt: baseline.Prompt, Version: baseline.Version})
		for _, alias := range baseline.Aliases {
			prompt := baseline.Prompt
			prompt.ID = strings.TrimSpace(alias)
			prompt.Name = baseline.Prompt.Name + " (legacy alias)"
			if prompt.Metadata == nil {
				prompt.Metadata = map[string]any{}
			}
			prompt.Metadata = cloneMap(prompt.Metadata)
			prompt.Metadata["canonical_prompt_id"] = baseline.Prompt.ID
			version := baseline.Version
			version.PromptID = prompt.ID
			items = append(items, seedItem{Prompt: prompt, Version: version})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Prompt.ID < items[j].Prompt.ID
	})
	return items
}

func upsertPrompt(client *http.Client, apiURL, token, userID string, prompt agentruntime.PromptTemplate) error {
	payload := map[string]any{"prompt": prompt}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, joinURL(apiURL, "/v1/admin/ops/prompts"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	addHeaders(req, token, userID)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("status %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}

func createVersion(client *http.Client, apiURL, token, userID, promptID string, version agentruntime.PromptVersion) (string, error) {
	body, _ := json.Marshal(version)
	req, err := http.NewRequest(http.MethodPost, joinURL(apiURL, "/v1/admin/ops/prompts/"+urlPathEscape(promptID)+"/versions"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	addHeaders(req, token, userID)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return "created", nil
	}
	if res.StatusCode == http.StatusConflict || strings.Contains(strings.ToLower(string(data)), "already") || strings.Contains(strings.ToLower(string(data)), "duplicate") || strings.Contains(strings.ToLower(string(data)), "unique") {
		return "exists", nil
	}
	return "", fmt.Errorf("status %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
}

func addHeaders(req *http.Request, token, userID string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", token)
	req.Header.Set("X-User-ID", userID)
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func urlPathEscape(value string) string {
	return url.PathEscape(value)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatalf("marshal json: %v", err)
	}
	fmt.Println(string(data))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
