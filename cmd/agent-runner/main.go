// Command agent-runner keeps headless CLI workers (claude, codex, grok) online on the local
// msg2agent relay and turns A2A tasks into jailed CLI runs. See
// docs/decisions/ADR-001-agent-runner.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/agent"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
)

const healthInterval = 30 * time.Second

func main() {
	cfgPath := flag.String("config", expandHome("~/.config/msg2agent/runner.json"), "Runner config (JSON)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(*cfgPath, logger); err != nil {
		logger.Error("agent-runner failed", "error", err)
		os.Exit(1)
	}
}

func run(cfgPath string, logger *slog.Logger) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	for _, sub := range []string{"", "scratch", "worktrees", "diffs", "results"} {
		if err := os.MkdirAll(filepath.Join(cfg.StateDir, sub), 0o700); err != nil {
			return err
		}
	}
	if *cfg.Sandbox && !sandboxAvailable() {
		return errors.New("bwrap sandbox unavailable: refusing to run workers unjailed (fail closed)")
	}
	if !*cfg.Sandbox {
		logger.Warn("SANDBOX DISABLED by config: workers run with tool flags only")
	}

	store, err := openStore(filepath.Join(cfg.StateDir, "tasks.db"))
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if n, err := store.FailOrphans(time.Now()); err != nil {
		return err
	} else if n > 0 {
		logger.Warn("marked orphaned tasks as failed", "count", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	agents := map[string]*agent.Agent{}
	defer func() {
		for _, a := range agents {
			_ = a.Stop()
		}
	}()
	reply := func(ctx context.Context, worker, to string, t *Task) error {
		_, err := agents[worker].SendAsync(ctx, to, "tasks/result", replyPayload(t, cfg.StateDir))
		return err
	}
	ex := systemdExecutor{Slice: cfg.Slice, RuntimeMax: cfg.Timeout.Duration + time.Minute}
	r := newRunner(cfg, store, ex, reply, logger)

	for _, w := range cfg.Workers {
		a, err := startWorker(ctx, cfg, w, r, logger)
		if err != nil {
			return fmt.Errorf("worker %s: %w", w.Name, err)
		}
		agents[w.Name] = a
		logger.Info("worker online", "did", a.DID(), "tool", w.Tool)
	}

	// The agent library does not reconnect: exit on relay loss and let systemd restart us.
	tick := time.NewTicker(healthInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down; canceling in-flight runs")
			r.CancelAll()
			r.Wait()
			return nil
		case <-tick.C:
			for name, a := range agents {
				hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				_, err := a.CallRelay(hctx, "relay.discover", nil)
				cancel()
				if err != nil && ctx.Err() == nil {
					return fmt.Errorf("relay health check failed for %s: %w", name, err)
				}
			}
		}
	}
}

func startWorker(ctx context.Context, cfg *Config, w WorkerConfig, r *Runner, logger *slog.Logger) (*agent.Agent, error) {
	ident, err := loadIdentity(filepath.Join(cfg.IdentityDir, w.Name+".key"), cfg.Domain, w.Name)
	if err != nil {
		return nil, err
	}
	a, err := agent.New(agent.Config{
		Domain: cfg.Domain, AgentID: w.Name, DisplayName: w.Name, RelayAddr: cfg.Relay,
		Logger: logger.With("worker", w.Name), Identity: ident,
	})
	if err != nil {
		return nil, err
	}
	name := w.Name
	a.RegisterMethod("message/send", func(ctx context.Context, params json.RawMessage) (any, error) {
		return r.Submit(name, agent.MessageFrom(ctx), params)
	})
	a.RegisterMethod("tasks/get", func(ctx context.Context, params json.RawMessage) (any, error) {
		return r.Get(agent.MessageFrom(ctx), params)
	})
	a.RegisterMethod("tasks/cancel", func(ctx context.Context, params json.RawMessage) (any, error) {
		return r.Cancel(agent.MessageFrom(ctx), params)
	})
	a.AddCapability("a2a-worker", fmt.Sprintf("Headless %s worker; task text or {prompt,dir,profile:ro|rw}", w.Tool),
		[]string{"message/send", "tasks/get", "tasks/cancel"})

	if err := a.Start(ctx); err != nil {
		return nil, err
	}
	if err := a.Connect(ctx, cfg.Relay); err != nil {
		return nil, err
	}
	tsSec := time.Now().Unix()
	rec := a.Record()
	_, err = a.CallRelay(ctx, "relay.register", map[string]any{
		"id": rec.ID, "did": rec.DID, "display_name": rec.DisplayName, "public_keys": rec.PublicKeys,
		"endpoints": rec.Endpoints, "capabilities": rec.Capabilities, "status": rec.Status,
		"proof": a.Sign(fmt.Appendf(nil, "%s:%d", a.DID(), tsSec)), "timestamp": tsSec,
	})
	if err != nil {
		_ = a.Stop()
		return nil, fmt.Errorf("relay.register: %w", err)
	}
	return a, nil
}

// loadIdentity loads a persistent identity or creates and saves one (same as cmd/mcp-server).
func loadIdentity(path, domain, name string) (*identity.Identity, error) {
	if id, err := identity.LoadFromFile(path, domain, name); err == nil {
		return id, nil
	}
	id, err := identity.NewIdentity(domain, name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return id, identity.SaveToFile(id, path)
}
