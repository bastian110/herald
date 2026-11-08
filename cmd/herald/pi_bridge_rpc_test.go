package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPiBridgeKeepsRPCProcessBetweenMessages(t *testing.T) {
	tmp := t.TempDir()
	bridge := copyPiBridge(t, tmp)
	sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
	writePiRPC(t, tmp)
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' '{"kind":"text","text":"first message","chat_id":123}'
 until grep -q 'PI OK' "$HERALD_TEST_SEND_LOG" 2>/dev/null; do sleep 0.05; done
 # The second message arrives after the first run has settled, not while queued.
 sleep 0.3
 printf '%s\n' '{"kind":"text","text":"second message","chat_id":123}'
 sleep 10
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge)
	cmd.Dir = tmp
	cmd.Env = bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFile(ctx, t, sendLog, func(s string) bool { return strings.Count(s, "PI OK") == 2 })
	log := readString(t, piLog)
	var pids []string
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "pid:") {
			pids = append(pids, line)
		}
	}
	if len(pids) != 2 || pids[0] != pids[1] {
		t.Fatalf("messages did not reuse same runtime: %s", log)
	}
	if !strings.Contains(log, "stdin:first message") || !strings.Contains(log, "stdin:second message") {
		t.Fatalf("lost prompt: %s", log)
	}
	if strings.Contains(readString(t, sendLog), "ajouté à la file") {
		t.Fatal("idle persistent runtime was incorrectly marked busy")
	}
	pidPath := filepath.Join(tmp, "pi.pid")
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("runtime not retained after response: %v", err)
	}
	killProcessGroup(t, cmd.Process.Pid)
	_ = cmd.Wait()
}

func TestPiBridgeNewReplacesIdleRPCRuntime(t *testing.T) {
	tmp := t.TempDir()
	bridge := copyPiBridge(t, tmp)
	sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
	writePiRPC(t, tmp)
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' '{"kind":"text","text":"before reset","chat_id":123}'
 until grep -q 'PI OK' "$HERALD_TEST_SEND_LOG" 2>/dev/null; do sleep 0.05; done
 printf '%s\n' '{"kind":"text","text":"/new","chat_id":123}'
 until grep -q 'Nouvelle conversation' "$HERALD_TEST_SEND_LOG" 2>/dev/null; do sleep 0.05; done
 printf '%s\n' '{"kind":"text","text":"after reset","chat_id":123}'
 sleep 10
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge)
	cmd.Dir = tmp
	cmd.Env = append(bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions")), "HERALD_TEST_CLEANUP=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFile(ctx, t, sendLog, func(s string) bool { return strings.Count(s, "PI OK") == 2 })
	var pids []string
	for _, line := range strings.Split(readString(t, piLog), "\n") {
		if strings.HasPrefix(line, "pid:") {
			pids = append(pids, line)
		}
	}
	if len(pids) != 2 || pids[0] == pids[1] {
		t.Fatalf("/new did not replace runtime: %v", pids)
	}
	if cleanup := readString(t, piLog+".cleanup"); cleanup != pids[0]+"\n" {
		t.Fatalf("/new interrupted delayed runtime cleanup: %q", cleanup)
	}
	killProcessGroup(t, cmd.Process.Pid)
	_ = cmd.Wait()
}

func TestPiBridgeMainPIDSignalCleansRuntimeAndChildren(t *testing.T) {
	tmp := t.TempDir()
	bridge := copyPiBridge(t, tmp)
	sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
	writePiRPC(t, tmp)
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' '{"kind":"text","text":"before service shutdown","chat_id":123}'
 sleep 60
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge)
	cmd.Dir = tmp
	cmd.Env = append(bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions")), "HERALD_TEST_CLEANUP=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFileContains(ctx, t, sendLog, "PI OK")
	var descendants []int
	var collect func(int)
	collect = func(pid int) {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
		if err != nil {
			return
		}
		for _, field := range strings.Fields(string(data)) {
			child, err := strconv.Atoi(field)
			if err != nil {
				t.Fatal(err)
			}
			descendants = append(descendants, child)
			collect(child)
		}
	}
	collect(cmd.Process.Pid)
	if len(descendants) < 3 {
		t.Fatalf("missing listener, worker or runtime children: %v", descendants)
	}
	// Match KillMode=mixed: signal only the bridge, not its process group.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge did not exit cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("main-PID-only SIGTERM did not interrupt listener wait")
	}
	if cleanup := readString(t, piLog+".cleanup"); !strings.Contains(cleanup, "pid:") {
		t.Fatalf("runtime cleanup was interrupted: %q", cleanup)
	}
	for _, pid := range descendants {
		if state, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil && !strings.Contains(string(state), ") Z ") {
			t.Errorf("child still running after shutdown: %d", pid)
		}
	}
}

func TestPiBridgeRPCRecoversAfterCrashWithoutReplayingPrompt(t *testing.T) {
	tmp := t.TempDir()
	bridge := copyPiBridge(t, tmp)
	sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
	writeExecutable(t, filepath.Join(tmp, "pi"), `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
 r=json.loads(line)
 if r['type']=='get_state':
  print(json.dumps({'type':'response','id':r['id'],'success':True,'data':{'sessionId':'test-session'}}),flush=True)
 elif r['type']=='prompt':
  with open(os.environ['HERALD_TEST_PI_LOG'],'a') as f: f.write(r['message']+'\n')
  if r['message']=='crash':sys.exit(7)
  print(json.dumps({'type':'message_end','message':{'role':'assistant','content':[{'type':'text','text':'RECOVERED'}]}}),flush=True)
  print(json.dumps({'type':'agent_settled'}),flush=True)
`)
	writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' '{"kind":"text","text":"crash","chat_id":123}'
 until grep -q 'processus Pi' "$HERALD_TEST_SEND_LOG" 2>/dev/null; do sleep 0.05; done
 printf '%s\n' '{"kind":"text","text":"retry explicitly","chat_id":123}'
 sleep 10
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bridge)
	cmd.Dir = tmp
	cmd.Env = bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcessGroup(t, cmd.Process.Pid)
	waitForFileContains(ctx, t, sendLog, "RECOVERED")
	if got := readString(t, piLog); got != "crash\nretry explicitly\n" {
		t.Fatalf("unexpected replay or lost prompt: %q", got)
	}
	killProcessGroup(t, cmd.Process.Pid)
	_ = cmd.Wait()
}
