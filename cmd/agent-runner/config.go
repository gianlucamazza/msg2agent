package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// WorkerConfig binds one network identity to one CLI tool.
type WorkerConfig struct {
	Name string `json:"name"` // agent name -> did:wba:<domain>:agent:<name>
	Tool string `json:"tool"` // claude | codex | grok | opencode
	// Model overrides the tool's default model (e.g. a route reachable without extra secrets).
	Model string `json:"model,omitempty"`
	// ROBinds are extra $HOME-relative paths exposed read-only in the jail (e.g. the token
	// file a provider config references). Everything not listed stays masked.
	ROBinds []string `json:"ro_binds,omitempty"`
	// Overlays mask extra $HOME-relative files inside the jail with this content, e.g. a
	// credential the user's tool config references but the worker must not see.
	Overlays map[string]string `json:"overlays,omitempty"`
}

// Config is the runner configuration (JSON, see docs/decisions/ADR-001-agent-runner.md).
type Config struct {
	Relay         string         `json:"relay"`
	Domain        string         `json:"domain"`
	StateDir      string         `json:"state_dir"`
	IdentityDir   string         `json:"identity_dir"`
	Roots         []string       `json:"roots"`    // task dirs must live under one of these
	Senders       []string       `json:"senders"`  // DIDs allowed to submit tasks
	RWAllow       []string       `json:"rw_allow"` // DIDs allowed to request the rw profile
	Timeout       Duration       `json:"timeout"`
	MaxConcurrent int            `json:"max_concurrent"`
	Slice         string         `json:"slice"`
	Sandbox       *bool          `json:"sandbox"` // default true; false only for tests/debugging
	Workers       []WorkerConfig `json:"workers"`
}

// Duration is a time.Duration that unmarshals from a Go duration string ("15m").
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.normalize(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) normalize() error {
	if c.Relay == "" {
		c.Relay = "ws://127.0.0.1:8080"
	}
	if c.Domain == "" {
		c.Domain = "localhost"
	}
	if c.Timeout.Duration == 0 {
		c.Timeout.Duration = 15 * time.Minute
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 2
	}
	if c.Slice == "" {
		c.Slice = "agents.slice"
	}
	if c.Sandbox == nil {
		on := true
		c.Sandbox = &on
	}
	c.StateDir = expandHome(c.StateDir)
	c.IdentityDir = expandHome(c.IdentityDir)
	for i, r := range c.Roots {
		c.Roots[i] = expandHome(r)
	}
	if c.StateDir == "" || c.IdentityDir == "" {
		return errors.New("state_dir and identity_dir are required")
	}
	if len(c.Roots) == 0 {
		return errors.New("roots must list at least one directory")
	}
	if len(c.Workers) == 0 {
		return errors.New("no workers configured")
	}
	seen := map[string]bool{}
	for _, w := range c.Workers {
		if _, ok := tools[w.Tool]; !ok {
			return fmt.Errorf("worker %q: unknown tool %q", w.Name, w.Tool)
		}
		rels := append([]string{}, w.ROBinds...)
		for rel := range w.Overlays {
			rels = append(rels, rel)
		}
		for _, rel := range rels {
			if filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
				return fmt.Errorf("worker %q: path %q must be relative to $HOME", w.Name, rel)
			}
		}
		if w.Name == "" || seen[w.Name] {
			return fmt.Errorf("worker name %q empty or duplicated", w.Name)
		}
		seen[w.Name] = true
	}
	return nil
}

func (c *Config) senderAllowed(did string) bool { return slices.Contains(c.Senders, did) }
func (c *Config) rwAllowed(did string) bool     { return slices.Contains(c.RWAllow, did) }

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}
