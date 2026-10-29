package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "loopback address for the node probe API")
	tokenFile := flag.String("token-file", "", "path to a mode-0600 bootstrap token file")
	simulationJournal := flag.String("simulation-journal", "", "absolute path to test-only journal; no code execution")
	simulationRevision := flag.String("simulation-revision", "", "expected policy revision for test-only simulation")
	developmentConfig := flag.String("development-config", "", "explicit private development Node configuration; loopback only")
	flag.Parse()
	if *developmentConfig != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serveDevelopment(ctx, *addr, *developmentConfig); err != nil {
			slog.Error("development Node stopped", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := serveNode(*addr, *tokenFile, *simulationJournal, *simulationRevision); err != nil {
		slog.Error("sandbox node startup failed", "error", err)
		os.Exit(1)
	}
}

func serve(addr, tokenFile string) error {
	return serveNode(addr, tokenFile, "", "")
}

func serveNode(addr, tokenFile, simulationJournal, simulationRevision string) error {
	if err := validateLoopbackAddress(addr); err != nil {
		return err
	}
	if strings.TrimSpace(tokenFile) == "" {
		return errors.New("sandbox node token file is required")
	}
	info, err := os.Lstat(tokenFile)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("sandbox node token file must be regular and accessible only to its owner")
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return err
	}
	handler, err := sandboxnode.NewProbeHandler([]byte(strings.TrimSpace(string(token))))
	if err != nil {
		return err
	}
	if simulationJournal != "" || simulationRevision != "" {
		if simulationJournal == "" || simulationRevision == "" {
			return errors.New("both simulation journal and revision are required")
		}
		journal, journalErr := sandboxnode.OpenFileJournal(simulationJournal, 32, 128, nil)
		if journalErr != nil {
			return journalErr
		}
		if _, reapErr := journal.Reap(10 * time.Minute); reapErr != nil {
			return reapErr
		}
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				_, _ = journal.Reap(10 * time.Minute)
			}
		}()
		sim, simErr := sandboxnode.NewSimulationHandler([]byte(strings.TrimSpace(string(token))), journal, simulationRevision)
		if simErr != nil {
			return simErr
		}
		probes := handler
		mux := http.NewServeMux()
		mux.Handle("/v1/sim/", sim)
		mux.Handle("/", probes)
		handler = mux
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	return server.Serve(listener)
}

func validateLoopbackAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("sandbox node must bind to a numeric loopback address")
	}
	return nil
}
