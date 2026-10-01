package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Port of vibemonitor orchestration/sandbox.py (ADR-006): a bubblewrap jail that is the
// authoritative boundary for every worker run. $HOME is a tmpfs with only the tool's own
// config/cache re-bound rw; /run is fresh (no reach to the relay or the user-session sockets);
// the network is shared because the CLI must reach its model API.

var (
	sysRO     = []string{"/usr", "/etc", "/opt"}
	mergedUsr = [][2]string{{"/bin", "usr/bin"}, {"/lib", "usr/lib"}, {"/lib64", "usr/lib"}, {"/sbin", "usr/bin"}}
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
}

// sandboxAvailable probes that bwrap exists and unprivileged user namespaces work.
func sandboxAvailable() bool {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return false
	}
	return exec.Command(path, "--ro-bind", "/", "/", "--unshare-all", "--", "true").Run() == nil //nolint:gosec // G204: resolved bwrap path, constant args
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// wrapSandbox returns argv prefixed with the bwrap jail described by s.
func wrapSandbox(argv []string, s sandboxSpec) []string {
	a := []string{"bwrap", "--unshare-all", "--share-net", "--die-with-parent", "--new-session"}
	for _, d := range sysRO {
		if exists(d) {
			a = append(a, "--ro-bind", d, d)
		}
	}
	for _, l := range mergedUsr {
		if isSymlink(l[0]) {
			a = append(a, "--symlink", l[1], l[0])
		}
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
