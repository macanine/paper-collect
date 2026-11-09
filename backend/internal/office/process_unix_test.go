//go:build !windows

package office

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRendererCleanupStopsChildProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := dir + "/child.pid"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 30 & echo $! > "$PID_FILE"`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	if err := runRenderer(cmd); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		if err != nil {
			t.Fatalf("probe child process: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("renderer child %d survived cleanup", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRendererCancellationStopsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := dir + "/child.pid"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 30 & echo $! > "$PID_FILE"; wait`)
	cmd.Env = append(os.Environ(), "PID_FILE="+pidFile)
	if err := runRenderer(cmd); err == nil {
		t.Fatal("renderer unexpectedly completed")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		if err != nil {
			t.Fatalf("probe child process: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("renderer child %d survived cancellation", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
