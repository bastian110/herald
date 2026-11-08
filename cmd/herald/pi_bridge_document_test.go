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

func TestPiBridgeDocument(t *testing.T) {
	for _, tc := range []struct {
		name, message, failure, want string
		success                      bool
	}{
		{"pdf", `{"kind":"document","chat_id":123,"text":"rembourse ce justificatif","file_id":"pdf-id","file_name":"../../Claire $(touch HACKED).pdf","mime_type":"application/pdf","file_size":30}`, "", "PI OK", true},
		{"no caption or filename", `{"kind":"document","chat_id":123,"file_id":"pdf-id","mime_type":"application/pdf"}`, "", "PI OK", true},
		{"too large", `{"kind":"document","chat_id":123,"file_id":"pdf-id","file_size":20971521}`, "", "20 Mo", false},
		{"getFile failed", `{"kind":"document","chat_id":123,"file_id":"pdf-id"}`, "metadata", "Échec téléchargement", false},
		{"download failed", `{"kind":"document","chat_id":123,"file_id":"pdf-id"}`, "download", "Échec téléchargement", false},
		{"token missing", `{"kind":"document","chat_id":123,"file_id":"pdf-id"}`, "token", "TELEGRAM_BOT_TOKEN manque", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			bridge := copyPiBridge(t, tmp)
			sendLog, piLog := filepath.Join(tmp, "send.log"), filepath.Join(tmp, "pi.log")
			writeExecutable(t, filepath.Join(tmp, "herald"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == recv ]]; then
 printf '%s\n' "$TEST_DOCUMENT"
 sleep 10
elif [[ "$1" == send ]]; then
 printf '%s\n' "$*" >> "$HERALD_TEST_SEND_LOG"
fi
`)
			writeExecutable(t, filepath.Join(tmp, "curl"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$TEST_CURL_LOG"
if [[ "$*" == *getFile* ]]; then
 if [[ "$TEST_FAILURE" == metadata ]]; then
  printf '{"ok":false,"description":"bad file"}'
 else
  printf '{"ok":true,"result":{"file_path":"documents/file.pdf"}}'
 fi
else
 output=""
 while (($#)); do
  if [[ "$1" == -o ]]; then output="$2"; shift; fi
  shift
 done
 printf '%%PDF-1.4\nfixture' > "$output"
 [[ "$TEST_FAILURE" != download ]]
fi
`)
			writePiRPC(t, tmp)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bridge)
			cmd.Dir = tmp
			cmd.Env = append(bridgeTestEnv(tmp, sendLog, piLog, filepath.Join(tmp, "sessions")),
				"TEST_DOCUMENT="+tc.message, "TEST_FAILURE="+tc.failure,
				"TEST_CURL_LOG="+filepath.Join(tmp, "curl.log"), "TELEGRAM_BOT_TOKEN=test-token")
			if tc.failure == "token" {
				cmd.Env = append(cmd.Env, "TELEGRAM_BOT_TOKEN=")
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer killProcessGroup(t, cmd.Process.Pid)
			waitForFileContains(ctx, t, sendLog, tc.want)
			killProcessGroup(t, cmd.Process.Pid)
			_ = cmd.Wait()
			files, err := filepath.Glob(filepath.Join(tmp, "sessions", "attachments", "*", "*"))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.success {
				if len(files) != 0 {
					t.Fatalf("failed download left files: %v", files)
				}
				if _, err := os.Stat(piLog); !os.IsNotExist(err) {
					t.Fatal("failed document should not invoke pi")
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("want one persistent attachment, got %v", files)
			}
			if got := readString(t, files[0]); got != "%PDF-1.4\nfixture" {
				t.Fatalf("PDF not preserved: %q", got)
			}
			info, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("private PDF permissions: %v", info.Mode())
			}
			prompt := readString(t, piLog)
			if !strings.Contains(prompt, files[0]) || !strings.Contains(prompt, "pdftotext") {
				t.Fatalf("missing PDF path/instructions: %s", prompt)
			}
			if tc.name == "pdf" && !strings.Contains(prompt, "rembourse ce justificatif") {
				t.Fatal("caption lost")
			}
			if _, err := os.Stat(filepath.Join(tmp, "HACKED")); !os.IsNotExist(err) {
				t.Fatal("filename executed as shell code")
			}
			curlLog := readString(t, filepath.Join(tmp, "curl.log"))
			if !strings.Contains(curlLog, "--max-filesize 20971520") {
				t.Fatal("download must enforce 20 MB even without metadata")
			}
		})
	}
}
