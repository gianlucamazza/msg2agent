package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	maxPromptBytes = 64 << 10 // argv-safe and far beyond any sane task
	maxReplyBytes  = 48 << 10 // full output always kept on disk
	maxHops        = 1
)

// Executor runs argv to completion and returns stdout and the exit code.
// The production implementation is systemdExecutor; tests inject a fake.
type Executor interface {
	Run(ctx context.Context, unit, cwd string, argv, env []string) (stdout []byte, exitCode int, err error)
}

// ReplyFunc delivers a terminal task result from worker back to the sender.
type ReplyFunc func(ctx context.Context, worker, to string, t *Task) error

// taskRequest is the decoded A2A message/send payload.
type taskRequest struct {
	Prompt  string  `json:"prompt"`
	Dir     string  `json:"dir"`
	Profile Profile `json:"profile"`
	Hops    int     `json:"hops"`
}

// Runner validates, schedules, and executes delegated tasks.
type Runner struct {
	cfg    *Config
	store  *Store
	exec   Executor
	reply  ReplyFunc
	logger *slog.Logger
	now    func() time.Time

	global  chan struct{}
	perTool map[string]chan struct{} // one run at a time per worker

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	auditMu sync.Mutex
	wg      sync.WaitGroup
}

func newRunner(cfg *Config, store *Store, ex Executor, reply ReplyFunc, logger *slog.Logger) *Runner {
	r := &Runner{
		cfg: cfg, store: store, exec: ex, reply: reply, logger: logger, now: time.Now,
		global:  make(chan struct{}, cfg.MaxConcurrent),
		perTool: map[string]chan struct{}{},
		cancels: map[string]context.CancelFunc{},
	}
	for _, w := range cfg.Workers {
		r.perTool[w.Name] = make(chan struct{}, 1)
	}
	return r
}

// Wait blocks until all in-flight runs have finished.
func (r *Runner) Wait() { r.wg.Wait() }

// CancelAll cancels every queued or running task (shutdown): their transient units are
// stopped and senders get a canceled reply instead of an orphan.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cancel := range r.cancels {
		cancel()
	}
}

// parseTaskRequest decodes the A2A params: message.parts[0].text is either a prompt string
// or an object {prompt, dir, profile, hops}.
func parseTaskRequest(params json.RawMessage) (*taskRequest, error) {
	var p struct {
		Message struct {
			Parts []struct {
				Text json.RawMessage `json:"text"`
			} `json:"parts"`
		} `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if len(p.Message.Parts) == 0 || len(p.Message.Parts[0].Text) == 0 {
		return nil, errors.New("message.parts[0].text is required")
	}
	raw := p.Message.Parts[0].Text
	req := &taskRequest{}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		req.Prompt = s
	} else if err := json.Unmarshal(raw, req); err != nil {
		return nil, fmt.Errorf("text must be a string or {prompt,dir,profile,hops}: %w", err)
	}
	if req.Profile == "" {
		req.Profile = ProfileRO
	}
	return req, nil
}

// validate enforces the ADR-001 policy and returns the resolved task directory ("" = scratch).
func (r *Runner) validate(from string, req *taskRequest) (string, error) {
	if !r.cfg.senderAllowed(from) {
		return "", fmt.Errorf("sender %s is not allowed", from)
	}
	if req.Hops > maxHops {
		return "", fmt.Errorf("hop limit exceeded (%d > %d)", req.Hops, maxHops)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return "", errors.New("empty prompt")
	}
	if len(req.Prompt) > maxPromptBytes {
		return "", fmt.Errorf("prompt exceeds %d bytes", maxPromptBytes)
	}
	switch req.Profile {
	case ProfileRO:
	case ProfileRW:
		if !r.cfg.rwAllowed(from) {
			return "", fmt.Errorf("sender %s may not request the rw profile", from)
		}
		if req.Dir == "" {
			return "", errors.New("rw profile requires dir (a git repository)")
		}
	default:
		return "", fmt.Errorf("unknown profile %q", req.Profile)
	}
	if req.Dir == "" {
		return "", nil
	}
	dir, err := r.resolveDir(req.Dir)
	if err != nil {
		return "", err
	}
	if req.Profile == ProfileRW {
		if out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output(); err != nil || //nolint:gosec // G204: argv list, never a shell; paths validated against roots
			strings.TrimSpace(string(out)) != "true" {
			return "", fmt.Errorf("%s is not a git work tree", dir)
		}
	}
	return dir, nil
}

// resolveDir requires an absolute, existing directory under one of the configured roots,
// after symlink resolution (so a link cannot escape the roots).
func (r *Runner) resolveDir(d string) (string, error) {
	d = expandHome(d)
	if !filepath.IsAbs(d) {
		return "", fmt.Errorf("dir must be absolute: %s", d)
	}
	real, err := filepath.EvalSymlinks(d)
	if err != nil {
		return "", fmt.Errorf("dir: %w", err)
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", d)
	}
	for _, root := range r.cfg.Roots {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(rr, real); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return real, nil
		}
	}
	return "", fmt.Errorf("dir %s is outside the allowed roots", d)
}

// Submit handles message/send for worker: it validates, persists, and starts the run.
func (r *Runner) Submit(worker, from string, params json.RawMessage) (any, error) {
	req, err := parseTaskRequest(params)
	if err != nil {
		return nil, err
	}
	dir, err := r.validate(from, req)
	if err != nil {
		r.logger.Warn("task rejected", "worker", worker, "from", from, "error", err)
		return nil, err
	}
	now := r.now()
	t := &Task{
		ID: uuid.NewString(), Worker: worker, Sender: from, Profile: req.Profile, Dir: dir,
		Prompt: req.Prompt, State: StateSubmitted, Created: now, Updated: now,
	}
	if err := r.store.Create(t); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[t.ID] = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go r.run(ctx, cancel, t)
	return map[string]string{"status": "accepted", "task_id": t.ID, "state": StateSubmitted}, nil
}

// Get handles tasks/get; only the original sender may read a task.
func (r *Runner) Get(from string, params json.RawMessage) (any, error) {
	t, err := r.ownedTask(from, params)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Cancel handles tasks/cancel.
func (r *Runner) Cancel(from string, params json.RawMessage) (any, error) {
	t, err := r.ownedTask(from, params)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	cancel, ok := r.cancels[t.ID]
	r.mu.Unlock()
	if !ok {
		return map[string]string{"task_id": t.ID, "state": t.State}, nil
	}
	cancel()
	return map[string]string{"task_id": t.ID, "state": "canceling"}, nil
}

func (r *Runner) ownedTask(from string, params json.RawMessage) (*Task, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ID == "" {
		return nil, errors.New("params.id is required")
	}
	t, err := r.store.Get(p.ID)
	if err != nil {
		return nil, err
	}
	if t.Sender != from {
		return nil, errNotFound // do not leak other senders' tasks
	}
	return t, nil
}

// run owns the task's cancel func: it always releases the context and drops the
// registration used by Cancel/CancelAll.
func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, t *Task) {
	defer r.wg.Done()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.cancels, t.ID)
		r.mu.Unlock()
	}()

	if slot := r.perTool[t.Worker]; acquire(ctx, slot) {
		if acquire(ctx, r.global) {
			r.execute(ctx, t)
			release(r.global)
		}
		release(slot)
	}
	if !t.terminal() {
		t.State, t.Error = StateCanceled, "canceled before start"
	}
	t.Updated = r.now()
	if err := r.store.Update(t); err != nil {
		r.logger.Error("persist task", "task", t.ID, "error", err)
	}
	r.audit(t)
	replyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.reply(replyCtx, t.Worker, t.Sender, t); err != nil {
		r.logger.Error("reply failed", "task", t.ID, "to", t.Sender, "error", err)
	}
}

func acquire(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func release(sem chan struct{}) { <-sem }

// execute performs the run and fills t's terminal fields.
func (r *Runner) execute(ctx context.Context, t *Task) {
	t.State, t.Updated = StateWorking, r.now()
	if err := r.store.Update(t); err != nil {
		r.logger.Error("persist task", "task", t.ID, "error", err)
	}
	tool := r.toolOf(t.Worker)
	cwd, repoGit, cleanup, err := r.prepareDir(t)
	if err != nil {
		t.State, t.Error = StateFailed, err.Error()
		return
	}
	defer cleanup()

	home, _ := os.UserHomeDir()
	argv := tools[tool].argv(t.Profile, buildPrompt(t.Worker, t.Sender, t.Profile, t.Prompt), cwd, r.modelOf(t.Worker))
	if *r.cfg.Sandbox {
		overlays, err := materializeOverlays(r.cfg.StateDir, t.Worker, home, r.overlaysOf(t.Worker))
		if err != nil {
			t.State, t.Error = StateFailed, err.Error()
			return
		}
		argv = wrapSandbox(argv, sandboxSpec{Tool: tool, Cwd: cwd, Writable: t.Profile == ProfileRW,
			RepoGit: repoGit, Home: home, Overlays: overlays, ExtraRO: r.worker(t.Worker).ROBinds})
	}
	env := append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}, tools[tool].env(t.Profile)...)

	runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout.Duration)
	defer cancel()
	out, code, err := r.exec.Run(runCtx, "a2a-"+t.ID, cwd, argv, env)
	t.ExitCode = code
	r.saveOutput(t.ID, out)
	t.Result = extractText(tool, out)
	switch {
	case ctx.Err() != nil:
		t.State, t.Error = StateCanceled, "canceled"
	case runCtx.Err() != nil:
		t.State, t.Error = StateFailed, fmt.Sprintf("timeout after %s", r.cfg.Timeout.Duration)
	case err != nil:
		t.State, t.Error = StateFailed, err.Error()
	case code != 0:
		t.State, t.Error = StateFailed, fmt.Sprintf("exit code %d", code)
	default:
		t.State = StateCompleted
	}
	if t.Profile == ProfileRW {
		diff, derr := worktreeDiff(cwd)
		if derr != nil {
			t.Error = strings.TrimSpace(t.Error + "; diff: " + derr.Error())
		}
		t.Diff = diff
		if diff != "" {
			p := filepath.Join(r.cfg.StateDir, "diffs", t.ID+".patch")
			if err := os.WriteFile(p, []byte(diff), 0o600); err != nil {
				r.logger.Warn("save diff", "error", err)
			}
		}
	}
}

// overlaysOf merges the tool's built-in overlays with the worker's configured ones.
func (r *Runner) overlaysOf(worker string) map[string]string {
	m := map[string]string{}
	for _, w := range r.cfg.Workers {
		if w.Name == worker {
			for k, v := range tools[w.Tool].Overlays {
				m[k] = v
			}
			for k, v := range w.Overlays {
				m[k] = v
			}
		}
	}
	return m
}

// materializeOverlays writes overlay files under stateDir/overlays/<worker> and maps each
// host target ($HOME-relative key) to its source file.
func materializeOverlays(stateDir, worker, home string, overlays map[string]string) (map[string]string, error) {
	m := map[string]string{}
	for rel, content := range overlays {
		src := filepath.Join(stateDir, "overlays", worker, strings.ReplaceAll(rel, "/", "__"))
		if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
			return nil, err
		}
		if strings.Contains(content, overlayRandom) {
			b := make([]byte, 24)
			if _, err := rand.Read(b); err != nil {
				return nil, err
			}
			content = strings.ReplaceAll(content, overlayRandom, hex.EncodeToString(b))
		}
		if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
			return nil, err
		}
		m[filepath.Join(home, rel)] = src
	}
	return m, nil
}

func (r *Runner) modelOf(worker string) string { return r.worker(worker).Model }

func (r *Runner) worker(name string) WorkerConfig {
	for _, w := range r.cfg.Workers {
		if w.Name == name {
			return w
		}
	}
	return WorkerConfig{}
}

func (r *Runner) toolOf(worker string) string {
	for _, w := range r.cfg.Workers {
		if w.Name == worker {
			return w.Tool
		}
	}
	return ""
}

// prepareDir returns the run's cwd: the task dir (ro), a fresh scratch dir (ro, no dir given),
// or an ephemeral detached worktree (rw). cleanup removes scratch dirs and worktrees.
func (r *Runner) prepareDir(t *Task) (cwd, repoGit string, cleanup func(), err error) {
	noop := func() {}
	if t.Profile == ProfileRO {
		if t.Dir != "" {
			return t.Dir, "", noop, nil
		}
		d := filepath.Join(r.cfg.StateDir, "scratch", t.ID)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", "", noop, err
		}
		return d, "", func() { _ = os.RemoveAll(d) }, nil
	}
	common, err := exec.Command("git", "-C", t.Dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output() //nolint:gosec // G204: argv list, never a shell; paths validated against roots
	if err != nil {
		return "", "", noop, fmt.Errorf("git common dir: %w", err)
	}
	wt := filepath.Join(r.cfg.StateDir, "worktrees", t.ID)
	if out, err := exec.Command("git", "-C", t.Dir, "worktree", "add", "--detach", wt, "HEAD").CombinedOutput(); err != nil { //nolint:gosec // G204: argv list, never a shell; paths validated against roots
		return "", "", noop, fmt.Errorf("worktree add: %v: %s", err, bytes.TrimSpace(out))
	}
	rm := func() {
		if out, err := exec.Command("git", "-C", t.Dir, "worktree", "remove", "--force", wt).CombinedOutput(); err != nil { //nolint:gosec // G204: argv list, never a shell; paths validated against roots
			r.logger.Warn("worktree remove", "path", wt, "error", err, "output", string(out))
		}
	}
	return wt, strings.TrimSpace(string(common)), rm, nil
}

// worktreeDiff returns every change in the worktree against HEAD, new files included.
func worktreeDiff(wt string) (string, error) {
	if out, err := exec.Command("git", "-C", wt, "add", "-A").CombinedOutput(); err != nil { //nolint:gosec // G204: argv list, never a shell; paths validated against roots
		return "", fmt.Errorf("git add: %v: %s", err, bytes.TrimSpace(out))
	}
	out, err := exec.Command("git", "-C", wt, "diff", "--cached", "--binary", "HEAD").Output() //nolint:gosec // G204: argv list, never a shell; paths validated against roots
	return string(out), err
}

func (r *Runner) saveOutput(id string, out []byte) {
	p := filepath.Join(r.cfg.StateDir, "results", id+".out")
	if err := os.WriteFile(p, out, 0o600); err != nil { //nolint:gosec // G703: id is a runner-generated UUID
		r.logger.Warn("save output", "error", err)
	}
}

// audit appends one JSONL line per terminal task. The prompt is recorded only as a hash.
func (r *Runner) audit(t *Task) {
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:8]) }
	line, _ := json.Marshal(map[string]any{
		"ts": r.now().UTC().Format(time.RFC3339), "task_id": t.ID, "sender": t.Sender,
		"worker": t.Worker, "profile": t.Profile, "dir": t.Dir, "state": t.State,
		"exit_code": t.ExitCode, "duration_s": t.Updated.Sub(t.Created).Seconds(),
		"prompt_sha": sum(t.Prompt), "result_sha": sum(t.Result), "diff_bytes": len(t.Diff),
		"error": t.Error, "sandbox": *r.cfg.Sandbox,
	})
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	f, err := os.OpenFile(filepath.Join(r.cfg.StateDir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		r.logger.Error("audit", "error", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// replyPayload trims a task for the wire; full output stays in state_dir/results.
func replyPayload(t *Task, stateDir string) map[string]any {
	p := map[string]any{"type": "a2a.task.result", "task_id": t.ID, "worker": t.Worker,
		"state": t.State, "profile": t.Profile, "exit_code": t.ExitCode}
	if t.Error != "" {
		p["error"] = t.Error
	}
	p["result"], p["result_truncated"] = truncate(t.Result, maxReplyBytes)
	if t.Diff != "" {
		p["diff"], p["diff_truncated"] = truncate(t.Diff, maxReplyBytes)
		p["diff_path"] = filepath.Join(stateDir, "diffs", t.ID+".patch")
	}
	p["output_path"] = filepath.Join(stateDir, "results", t.ID+".out")
	return p
}

func truncate(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n], true
}
