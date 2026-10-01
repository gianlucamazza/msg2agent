package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// systemdExecutor runs each task as a transient unit in the agents slice, so CPU/IO/memory
// limits and oomd apply, and a cancel can stop the whole process tree by unit name.
type systemdExecutor struct {
	Slice      string
	RuntimeMax time.Duration
}

func (e systemdExecutor) args(unit, cwd string, argv, env []string) []string {
	a := []string{"--user", "--quiet", "--wait", "--pipe", "--collect",
		"--unit=" + unit, "--slice=" + e.Slice, "--working-directory=" + cwd,
		"-p", fmt.Sprintf("RuntimeMaxSec=%d", int(e.RuntimeMax.Seconds()))}
	for _, kv := range env {
		a = append(a, "-E", kv)
	}
	return append(append(a, "--"), argv...)
}

func (e systemdExecutor) Run(ctx context.Context, unit, cwd string, argv, env []string) ([]byte, int, error) {
	cmd := exec.Command("systemd-run", e.args(unit, cwd, argv, env)...) //nolint:gosec // G204: argv list, never a shell; paths validated against roots
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, -1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		// Stopping the unit kills the jailed tree; systemd-run --wait then returns.
		_ = exec.Command("systemctl", "--user", "stop", unit+".service").Run() //nolint:gosec // G204: argv list, never a shell; paths validated against roots
		err = <-done
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.Bytes(), 0, nil
	case errors.As(err, &exitErr):
		if stdout.Len() == 0 && stderr.Len() > 0 {
			return stderr.Bytes(), exitErr.ExitCode(), nil
		}
		return stdout.Bytes(), exitErr.ExitCode(), nil
	default:
		return stdout.Bytes(), -1, err
	}
}
