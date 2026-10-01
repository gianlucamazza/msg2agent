package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	alice = "did:wba:localhost:agent:claude-code"
	bob   = "did:wba:localhost:agent:codex"
	eve   = "did:wba:localhost:agent:eve"
)

type fakeExec struct {
	mu    sync.Mutex
	calls [][]string
	out   []byte
	code  int
	block chan struct{} // if set, Run waits on it or ctx
	write string        // if set, file written into cwd (simulates an rw edit)
}

func (f *fakeExec) Run(ctx context.Context, _, cwd string, argv, _ []string) ([]byte, int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, argv)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, -1, ctx.Err()
		}
	}
	if f.write != "" {
		if err := os.WriteFile(filepath.Join(cwd, f.write), []byte("hello\n"), 0o600); err != nil {
			return nil, 1, err
		}
	}
	return f.out, f.code, nil
}

func (f *fakeExec) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type replies struct {
	mu   sync.Mutex
	got  []*Task
	done chan struct{}
}

func (r *replies) fn(_ context.Context, _, to string, t *Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *t
	cp.Sender = to
	r.got = append(r.got, &cp)
	r.done <- struct{}{}
	return nil
}

func newTestRunner(t *testing.T, ex Executor) (*Runner, *replies, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	for _, sub := range []string{"scratch", "worktrees", "diffs", "results"} {
		if err := os.MkdirAll(filepath.Join(state, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	off := false
	cfg := &Config{
		StateDir: state, IdentityDir: t.TempDir(), Roots: []string{root},
		Senders: []string{alice, bob}, RWAllow: []string{alice}, Sandbox: &off,
		Workers: []WorkerConfig{{Name: "claude-worker", Tool: "claude"}, {Name: "codex-worker", Tool: "codex"}},
	}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	store, err := openStore(filepath.Join(state, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rep := &replies{done: make(chan struct{}, 8)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRunner(cfg, store, ex, rep.fn, logger), rep, root
}

func params(t *testing.T, text any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"message": map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func waitReply(t *testing.T, r *replies) *Task {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got[len(r.got)-1]
}

func TestParseTaskRequest(t *testing.T) {
	req, err := parseTaskRequest(params(t, "just a prompt"))
	if err != nil || req.Prompt != "just a prompt" || req.Profile != ProfileRO {
		t.Fatalf("string form: %+v %v", req, err)
	}
	req, err = parseTaskRequest(params(t, map[string]any{"prompt": "p", "dir": "/x", "profile": "rw", "hops": 1}))
	if err != nil || req.Dir != "/x" || req.Profile != ProfileRW || req.Hops != 1 {
		t.Fatalf("object form: %+v %v", req, err)
	}
	if _, err := parseTaskRequest(json.RawMessage(`{"message":{"parts":[]}}`)); err == nil {
		t.Fatal("expected error for empty parts")
	}
}

func TestValidatePolicy(t *testing.T) {
	r, _, root := newTestRunner(t, &fakeExec{})
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, from string
		req        taskRequest
		ok         bool
	}{
		{"unknown sender", eve, taskRequest{Prompt: "x", Profile: ProfileRO}, false},
		{"ro no dir", bob, taskRequest{Prompt: "x", Profile: ProfileRO}, true},
		{"ro in root", bob, taskRequest{Prompt: "x", Profile: ProfileRO, Dir: root}, true},
		{"outside roots", bob, taskRequest{Prompt: "x", Profile: ProfileRO, Dir: outside}, false},
		{"symlink escape", bob, taskRequest{Prompt: "x", Profile: ProfileRO, Dir: link}, false},
		{"relative dir", bob, taskRequest{Prompt: "x", Profile: ProfileRO, Dir: "rel"}, false},
		{"rw not allowed", bob, taskRequest{Prompt: "x", Profile: ProfileRW, Dir: root}, false},
		{"rw needs dir", alice, taskRequest{Prompt: "x", Profile: ProfileRW}, false},
		{"rw needs git", alice, taskRequest{Prompt: "x", Profile: ProfileRW, Dir: root}, false},
		{"hop limit", bob, taskRequest{Prompt: "x", Profile: ProfileRO, Hops: 2}, false},
		{"empty prompt", bob, taskRequest{Prompt: "  ", Profile: ProfileRO}, false},
		{"huge prompt", bob, taskRequest{Prompt: strings.Repeat("a", maxPromptBytes+1), Profile: ProfileRO}, false},
		{"bad profile", bob, taskRequest{Prompt: "x", Profile: "root"}, false},
	}
	for _, c := range cases {
		_, err := r.validate(c.from, &c.req)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestSubmitCompletesAndReplies(t *testing.T) {
	ex := &fakeExec{out: []byte(`{"type":"result","result":"done!"}`)}
	r, rep, _ := newTestRunner(t, ex)
	res, err := r.Submit("claude-worker", bob, params(t, "summarize"))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(map[string]string)["task_id"]
	got := waitReply(t, rep)
	r.Wait()
	if got.ID != id || got.State != StateCompleted || got.Result != "done!" || got.Sender != bob {
		t.Fatalf("unexpected reply %+v", got)
	}
	// The untrusted text is wrapped in the template, never passed bare.
	argv := ex.calls[0]
	if argv[0] != "claude" || !strings.Contains(argv[2], "<task>\nsummarize\n</task>") {
		t.Fatalf("argv %q", argv)
	}
	if i := slices.Index(argv, "--tools"); i < 0 || argv[i+1] != "Read,Grep,Glob" || !slices.Contains(argv, "--strict-mcp-config") {
		t.Fatalf("ro flags missing: %q", argv)
	}
	// Owner can read it, others cannot.
	idp, _ := json.Marshal(map[string]string{"id": id})
	if _, err := r.Get(bob, idp); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(alice, idp); err == nil {
		t.Fatal("non-owner read a task")
	}
	data, _ := os.ReadFile(filepath.Join(r.cfg.StateDir, "audit.jsonl"))
	if !strings.Contains(string(data), id) || strings.Contains(string(data), "summarize") {
		t.Fatalf("audit must log the task by hash only: %s", data)
	}
}

func TestSubmitFailureExitCode(t *testing.T) {
	r, rep, _ := newTestRunner(t, &fakeExec{out: []byte("boom"), code: 3})
	if _, err := r.Submit("codex-worker", bob, params(t, "x")); err != nil {
		t.Fatal(err)
	}
	got := waitReply(t, rep)
	if got.State != StateFailed || got.ExitCode != 3 || got.Result != "boom" {
		t.Fatalf("got %+v", got)
	}
}

func TestCancelRunningTask(t *testing.T) {
	ex := &fakeExec{block: make(chan struct{})}
	r, rep, _ := newTestRunner(t, ex)
	res, _ := r.Submit("codex-worker", bob, params(t, "long"))
	id := res.(map[string]string)["task_id"]
	idp, _ := json.Marshal(map[string]string{"id": id})
	if _, err := r.Cancel(alice, idp); err == nil {
		t.Fatal("non-owner canceled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for ex.callCount() == 0 && time.Now().Before(deadline) { // wait until running
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := r.Cancel(bob, idp); err != nil {
		t.Fatal(err)
	}
	if got := waitReply(t, rep); got.State != StateCanceled {
		t.Fatalf("state %s", got.State)
	}
}

func TestRWRunsInWorktreeAndReturnsDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ex := &fakeExec{out: []byte("edited"), write: "NEW.txt"}
	r, rep, root := newTestRunner(t, ex)
	repo := filepath.Join(root, "repo")
	for _, c := range [][]string{{"init", "-q", repo}, {"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t",
		"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	if _, err := r.Submit("codex-worker", alice, params(t, map[string]any{"prompt": "add file", "dir": repo, "profile": "rw"})); err != nil {
		t.Fatal(err)
	}
	got := waitReply(t, rep)
	r.Wait()
	if got.State != StateCompleted || !strings.Contains(got.Diff, "NEW.txt") {
		t.Fatalf("got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "NEW.txt")); !os.IsNotExist(err) {
		t.Fatal("rw run touched the user's checkout")
	}
	if entries, _ := os.ReadDir(filepath.Join(r.cfg.StateDir, "worktrees")); len(entries) != 0 {
		t.Fatalf("worktree not cleaned up: %v", entries)
	}
	if !slices.Contains(ex.calls[0], "workspace-write") {
		t.Fatalf("rw codex flags: %q", ex.calls[0])
	}
}

func TestFailOrphans(t *testing.T) {
	r, _, _ := newTestRunner(t, &fakeExec{})
	now := time.Now()
	for i, st := range []string{StateWorking, StateCompleted} {
		task := &Task{ID: string(rune('a' + i)), Worker: "w", State: st, Created: now, Updated: now}
		if err := r.store.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.store.FailOrphans(now)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got, _ := r.store.Get("a"); got.State != StateFailed {
		t.Fatalf("orphan state %s", got.State)
	}
}
