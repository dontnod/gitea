// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// TODO: This packages still uses a singleton for the Manager.
// Once there's a decent web framework and dependencies are passed around like they should,
// then we delete the singleton.

var (
	// ErrExecTimeout represent a timeout error
	ErrExecTimeout = errors.New("Process execution timeout")
	manager        *Manager
)

// Process represents a working process inherit from Gogs.
type Process struct {
	PID         int64 // Process ID, not system one.
	Description string
	Start       time.Time
	Cmd         *exec.Cmd
}

// Manager knows about all processes and counts PIDs.
type Manager struct {
	mutex sync.Mutex

	counter   int64
	Processes map[int64]*Process
}

// GetManager returns a Manager and initializes one as singleton if there's none yet
func GetManager() *Manager {
	if manager == nil {
		manager = &Manager{
			Processes: make(map[int64]*Process),
		}
	}
	return manager
}

// Add a process to the ProcessManager and returns its PID.
func (pm *Manager) Add(description string, cmd *exec.Cmd) int64 {
	pm.mutex.Lock()
	pid := pm.counter + 1
	pm.Processes[pid] = &Process{
		PID:         pid,
		Description: description,
		Start:       time.Now(),
		Cmd:         cmd,
	}
	pm.counter = pid
	pm.mutex.Unlock()

	return pid
}

// Remove a process from the ProcessManager.
func (pm *Manager) Remove(pid int64) {
	pm.mutex.Lock()
	delete(pm.Processes, pid)
	pm.mutex.Unlock()
}

// Exec a command and use the default timeout.
func (pm *Manager) Exec(desc, cmdName string, args ...string) (string, string, error) {
	return pm.ExecDir(-1, "", desc, cmdName, args...)
}

// ExecTimeout a command and use a specific timeout duration.
func (pm *Manager) ExecTimeout(timeout time.Duration, desc, cmdName string, args ...string) (string, string, error) {
	return pm.ExecDir(timeout, "", desc, cmdName, args...)
}

// ExecDir a command and use the default timeout.
func (pm *Manager) ExecDir(timeout time.Duration, dir, desc, cmdName string, args ...string) (string, string, error) {
	return pm.ExecDirEnv(timeout, dir, desc, nil, cmdName, args...)
}

// ExecDirEnv runs a command in given path and environment variables, and waits for its completion
// up to the given timeout (or DefaultTimeout if -1 is given).
// Returns its complete stdout and stderr
// outputs and an error, if any (including timeout)
func (pm *Manager) ExecDirEnv(timeout time.Duration, dir, desc string, env []string, cmdName string, args ...string) (string, string, error) {
	return pm.ExecDirEnvStdIn(timeout, dir, desc, env, nil, cmdName, args...)
}

// ExecDirEnvStdIn runs a command in given path and environment variables with provided stdIN, and waits for its completion
// up to the given timeout (or DefaultTimeout if -1 is given).
// Returns its complete stdout and stderr
// outputs and an error, if any (including timeout)
func (pm *Manager) ExecDirEnvStdIn(timeout time.Duration, dir, desc string, env []string, stdIn io.Reader, cmdName string, args ...string) (string, string, error) {
	if timeout == -1 {
		timeout = 60 * time.Second
	}

	stdOut := new(bytes.Buffer)
	stdErr := new(bytes.Buffer)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = stdOut
	cmd.Stderr = stdErr
	if stdIn != nil {
		cmd.Stdin = stdIn
	}

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	pid := pm.Add(desc, cmd)
	err := cmd.Wait()
	pm.Remove(pid)

	if err != nil {
		err = fmt.Errorf("exec(%d:%s) failed: %v(%v) stdout: %v stderr: %v", pid, desc, err, ctx.Err(), stdOut, stdErr)
	}

	return stdOut.String(), stdErr.String(), err
}

// Kill and remove a process from list.
func (pm *Manager) Kill(pid int64) error {
	if proc, exists := pm.Processes[pid]; exists {
		pm.mutex.Lock()
		if proc.Cmd != nil &&
			proc.Cmd.Process != nil &&
			proc.Cmd.ProcessState != nil &&
			!proc.Cmd.ProcessState.Exited() {
			if err := proc.Cmd.Process.Kill(); err != nil {
				return fmt.Errorf("failed to kill process(%d/%s): %v", pid, proc.Description, err)
			}
		}
		delete(pm.Processes, pid)
		pm.mutex.Unlock()
	}

	return nil
}
