// Package system wraps os/exec and basic filesystem helpers used across pev.
// All shell-outs in pev MUST go through this package so that command
// auditing, secret scrubbing, and consistent error wrapping happen in one place.
package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
)

// CommandResult captures everything we need to make a check decision and
// reproduce the run later.
type CommandResult struct {
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
}

// RunCaptured executes `cmd` via /bin/sh -c with the given timeout and
// captures stdout/stderr separately. It does not stream to the terminal.
//
// Returns a populated CommandResult and a non-nil error only when the
// command could not be started or the context was cancelled. Non-zero exit
// codes are NOT errors — checks decide whether non-zero is a failure.
func RunCaptured(ctx context.Context, cmd string, timeout time.Duration) (CommandResult, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	c := exec.CommandContext(cctx, "/bin/sh", "-c", cmd)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	err := c.Run()
	res := CommandResult{
		Command:  cmd,
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		Duration: time.Since(start),
	}

	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		log.WithFields(log.Fields{"cmd": cmd, "timeout": timeout}).Warn("command timed out")
		return res, nil
	}

	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("running %q: %w", cmd, err)
	}
	res.ExitCode = 0
	return res, nil
}

// CommandExists reports whether a command resolves on $PATH.
func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
