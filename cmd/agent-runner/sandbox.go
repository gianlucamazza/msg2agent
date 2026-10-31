package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Port of vibemonitor orchestration/sandbox.py (ADR-006): a bubblewrap jail that is the
// authoritative boundary for every worker run. $HOME is a tmpfs with only the tool's own
// config/cache re-bound rw; /run is fresh (no reach to the relay or the user-session sockets);
// the network is shared because the CLI must reach its model API.

var (
	sysRO = []string{"/usr", "/etc", "/opt"}
	// usrmergeRoots are the compatibility paths Debian/Ubuntu/Fedora point into /usr.
	// Targets are read from the host: a hardcoded /lib64 -> usr/lib (true on some
	// older trees, never on Ubuntu 24/26) hides /lib64/ld-linux-x86-64.so.2 and
	// every exec inside the jail fails with ENOENT. Override with MSG2AGENT_SANDBOX_USR_MERGE
	// (comma-separated paths) on layouts that need extra roots.
	usrmergeRoots = []string{"/bin", "/lib", "/lib64", "/sbin"}
	// Tool runtimes and config the CLI reads (binaries, node modules, fnm, git identity).
	// Narrower than vibemonitor's whole ~/.local: keyrings and this runner's own state
	// (other tasks' results) stay masked.
	toolRuntimeRO = []string{".local/bin", ".local/lib", ".local/share/claude", ".local/share/fnm",
		".npm", ".config/fnm", ".gitconfig", ".config/git"}
)

// sandboxSpec describes one jailed run.
type sandboxSpec struct {
	Tool     string
	Cwd      string // the task directory or worktree
	Writable bool   // bind Cwd rw (rw profile) instead of ro
	RepoGit  string // main repo .git for a linked worktree (rw, hooks/config re-denied ro)
	Home     string
	Overlays map[string]string // host path to mask -> read-only source file
	ExtraRO  []string          // extra $HOME-relative paths bound read-only
}

// bwrapPath is the bubblewrap binary. MSG2AGENT_BWRAP overrides PATH lookup
// (nix, custom prefixes). Empty means the binary is not usable.
func bwrapPath() string {
	if p := strings.TrimSpace(os.Getenv("MSG2AGENT_BWRAP")); p != "" {
		if _, err := os.Stat(p); err == nil { // #nosec G703 -- operator-set MSG2AGENT_BWRAP, not request input
			return p
		}
		return ""
	}
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return ""
	}
	return path
}

func configuredUsrMergeRoots() []string {
	if v := strings.TrimSpace(os.Getenv("MSG2AGENT_SANDBOX_USR_MERGE")); v != "" {
		var roots []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roots = append(roots, p)
			}
		}
		if len(roots) > 0 {
			return roots
		}
	}
	return usrmergeRoots
}

// appendCompatRoot recreates one host root inside the jail: a usrmerge symlink
// keeps the host target; a real directory is bound read-only (Alpine, older RHEL).
func appendCompatRoot(a []string, p string) []string {
	if isSymlink(p) {
		target, err := os.Readlink(p)
		if err != nil || target == "" {
			return a
		}
		return append(a, "--symlink", target, p)
	}
	if exists(p) {
		return append(a, "--ro-bind", p, p)
	}
	return a
}

// sandboxUnavailableReason is empty when the jail we actually construct can exec
// a binary. A --ro-bind / / probe is not enough: that preserves host usrmerge
// symlinks and can succeed on hosts where wrapSandbox's root cannot run anything.
// Tests skip with this string; the runner fail-closes on a non-empty reason.
func sandboxUnavailableReason() string {
	if bwrapPath() == "" {
		return "bwrap not on PATH (set MSG2AGENT_BWRAP to a bubblewrap binary)"
	}
	dir, err := os.MkdirTemp("", "jail-probe-")
	if err != nil {
		return "cannot create jail probe directory: " + err.Error()
	}
	defer func() { _ = os.RemoveAll(dir) }()
	truePath := "/usr/bin/true"
	if !exists(truePath) {
		truePath = "true"
	}
	argv := wrapSandbox([]string{truePath}, sandboxSpec{Tool: "codex", Cwd: dir, Home: dir})
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- resolved bwrap path, constructed jail args
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "bwrap jail cannot exec (userns denied or root layout unusable): " + strings.TrimSpace(string(out)+" "+err.Error())
	}
	return ""
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// wrapSandbox returns argv prefixed with the bwrap jail described by s.
func wrapSandbox(argv []string, s sandboxSpec) []string {
	bin := bwrapPath()
	if bin == "" {
		bin = "bwrap"
	}
	a := []string{bin, "--unshare-all", "--share-net", "--die-with-parent", "--new-session"}
	for _, d := range sysRO {
		if exists(d) {
			a = append(a, "--ro-bind", d, d)
		}
	}
	for _, p := range configuredUsrMergeRoots() {
		a = appendCompatRoot(a, p)
	}
	a = append(a, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	a = append(a, "--tmpfs", s.Home, "--tmpfs", "/run")
	if exists("/run/systemd/resolve") { // DNS: resolv.conf usually points here
		a = append(a, "--dir", "/run/systemd", "--ro-bind", "/run/systemd/resolve", "/run/systemd/resolve")
	}
	// Read-only allow-list first, so a writable subpath bound later wins.
	for _, p := range toolRuntimeRO {
		hp := filepath.Join(s.Home, p)
		a = append(a, "--ro-bind-try", hp, hp)
	}
	for _, p := range s.ExtraRO {
		hp := filepath.Join(s.Home, p)
		a = append(a, "--ro-bind-try", hp, hp)
	}
	for _, p := range tools[s.Tool].HomeRW {
		hp := filepath.Join(s.Home, p)
		a = append(a, "--bind-try", hp, hp)
	}
	for target, src := range s.Overlays { // after the rw binds: the overlay wins
		a = append(a, "--ro-bind", src, target)
	}
	if s.Writable {
		a = append(a, "--bind", s.Cwd, s.Cwd)
	} else {
		a = append(a, "--ro-bind", s.Cwd, s.Cwd)
	}
	if s.RepoGit != "" && exists(s.RepoGit) {
		a = append(a, "--bind", s.RepoGit, s.RepoGit)
		for _, sub := range []string{"hooks", "config"} {
			if c := filepath.Join(s.RepoGit, sub); exists(c) {
				a = append(a, "--ro-bind", c, c)
			}
		}
	}
	a = append(a, "--chdir", s.Cwd, "--")
	return append(a, argv...)
}
