package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/telegram"
)

func TestPiBridgeLocation(t *testing.T) {
	var message telegram.Message
	if err := json.Unmarshal([]byte(`{"chat":{"id":123},"location":{"latitude":48.8566,"longitude":2.3522}}`), &message); err != nil {
		t.Fatal(err)
	}
	env, ok := envelopeFromMessage(&message)
	if !ok {
		t.Fatal("Telegram position ignored")
	}
	raw, err := protocol.Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	bridge := copyPiBridge(t, tmp)
	sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' "$TEST_LOCATION"
 sleep 10
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
	writePiRPC(t, tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge)
	cmd.Dir = tmp
	cmd.Env = append(bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions")), "TEST_LOCATION="+strings.TrimSpace(string(raw)), "TELEGRAM_BOT_TOKEN=")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFileContains(ctx, t, sendLog, "PI OK")
	killProcessGroup(t, cmd.Process.Pid)
	_ = cmd.Wait()
	if prompt := readString(t, piLog); !strings.Contains(prompt, "stdin:"+env.Text+"\n") {
		t.Fatalf("position not passed to pi: %q", prompt)
	}
}
