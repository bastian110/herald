package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPiBridgeTextOnlyTaskDoesNotPassEmptyImageArgument(t *testing.T) {
	tmp := t.TempDir()
	repoRoot := filepath.Join("..", "..")
	bridgeSource := filepath.Join(repoRoot, "bin", "herald-pi-bridge")
	bridgeDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bridgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bridgePath := filepath.Join(bridgeDir, "herald-pi-bridge")
	bridgeBytes, err := os.ReadFile(bridgeSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bridgePath, bridgeBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	sendLog := filepath.Join(tmp, "send.log")
	piLog := filepath.Join(tmp, "pi.log")
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "recv" ]]; then
  printf '%s\n' '{"kind":"text","text":"hello from telegram","chat_id":123}'
  for _ in $(seq 1 50); do
    [[ -s "$HERALD_TEST_SEND_LOG" ]] && exit 0
    sleep 0.1
  done
  exit 1
fi
if [[ "${1:-}" == "send" ]]; then
  printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "pi"), `#!/usr/bin/env bash
set -euo pipefail
{
  printf 'args:'
  for arg in "$@"; do printf ' <%s>' "$arg"; done
  printf '\nstdin:%s\n' "$(cat)"
} >> "$HERALD_TEST_PI_LOG"
for arg in "$@"; do
  if [[ "$arg" == "@" ]]; then
    echo 'unexpected empty @file argument' >&2
    exit 88
  fi
done
printf 'PI OK\n'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridgePath)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"PATH="+tmp+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+filepath.Join(tmp, "home"),
		"HERALD_TEST_SEND_LOG="+sendLog,
		"HERALD_TEST_PI_LOG="+piLog,
		"HERALD_PI_SESSION_DIR="+filepath.Join(tmp, "sessions"),
		"HERALD_PI_WORKDIR="+tmp,
		"HERALD_PI_LOCK_FILE="+filepath.Join(tmp, "bridge.lock"),
		"HERALD_PI_QUEUE_DIR="+filepath.Join(tmp, "queue"),
		"HERALD_PI_RUN_PID_FILE="+filepath.Join(tmp, "pi.pid"),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := &strings.Builder{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFileContent(ctx, t, sendLog)
	killProcessGroup(t, cmd.Process.Pid)
	if err := cmd.Wait(); err != nil && ctx.Err() != nil {
		t.Fatalf("bridge timed out: %v\n%s", err, output.String())
	}

	piOutput := readString(t, piLog)
	if strings.Contains(piOutput, " <@>") {
		t.Fatalf("pi received empty @file argument; pi log:\n%s", piOutput)
	}
	if !strings.Contains(piOutput, "stdin:hello from telegram") {
		t.Fatalf("pi did not receive prompt on stdin; pi log:\n%s", piOutput)
	}

	sent := readString(t, sendLog)
	if strings.Contains(sent, "pi exited") {
		t.Fatalf("bridge reported pi failure:\n%s", sent)
	}
	if !strings.Contains(sent, "PI OK") {
		t.Fatalf("bridge did not send pi output; send log:\n%s", sent)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func waitForFileContent(ctx context.Context, t *testing.T, path string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", path)
		case <-ticker.C:
			content, err := os.ReadFile(path)
			if err == nil && len(content) > 0 {
				return
			}
		}
	}
}

func killProcessGroup(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		t.Logf("failed to terminate process group %d: %v", pid, err)
	}
}
