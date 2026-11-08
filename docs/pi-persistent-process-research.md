# Persistent Pi process for Telegram human input

Date: 2026-11-08. Research independently confirmed by Oracle against installed Pi 1.4.0 documentation and runtime source.

## Implementation status

Subsequently authorized by the user and implemented in `bin/herald-pi-bridge` and `bin/herald-pi-rpc.py`. The bridge retains a persistent RPC child, dispatches sequential prompts, routes extension dialogs, and keeps cleanup on runtime shutdown. Nine Python worker tests and the full Go test suite pass. Tests cover persistence, reset, crash, delayed cleanup with stdout backpressure, correlated dialog replies and dialog expiry. Oracle's reviews blocked double-owner shutdown, uncorrelated/expired dialog routing, and Bash deferring SIGTERM while awaiting a foreground listener pipeline. These were fixed with worker-owned EOF shutdown, explicit dialog tokens/deadlines, and tracked background receiver/listener processes with an interruptible parent wait. A main-PID-only SIGTERM integration test checks delayed cleanup and no running child processes; it passed three repeated runs. Existing unrelated changes were preserved. Oracle's final verdict is SHIP after independently rerunning the tests. The installed systemd unit was updated to `KillMode=mixed` with a 45-second stop timeout, and `systemctl --user daemon-reload` succeeded without changing the service MainPID. The running service has not been restarted by this change; use `/reload` to activate the updated bridge. No live Amazon/2FA end-to-end test has been performed.

## Recommendation

Keep one `pi --mode rpc` subprocess alive for the active conversation. Keep stdin open and consume stdout continuously. Send Telegram messages as JSONL `prompt` commands to the same runtime. Collect completed assistant messages from `message_end` and publish the final response at `agent_settled`, not subprocess exit. Preserve the existing session directory, conversation selection, sequential queue, and chat routing.

The model run can finish while the process remains idle awaiting the next user message. No model polling is required. Browser Use's closure-held browser ID, page and credential-redaction state remain available across ordinary prompts.

## Confirmed cause

`bin/herald-pi-bridge:328–370` launches a new `pi -p` per message and waits for exit before replying. Print mode disposes its runtime. `~/.pi/agent/extensions/browser-use/index.ts:205–213` stops the Cloud browser on `session_shutdown`.

Cloud API evidence for the affected browser: started 11:38:16 UTC, stopped 11:39:58 UTC, timeout scheduled 12:08:16 UTC. The assistant completed its response at 11:39:57 UTC. Actual browser lifetime was approximately 1 minute 42 seconds, not 30 minutes.

## Important design constraints

- Merely replacing `-p` in `printf ... | pi` is insufficient: EOF on stdin shuts RPC down. Own long-lived pipes.
- Do not copy `--no-session` from the documentation examples: retain conversation history.
- `prompt` acceptance is not run completion. A `handled` disposition does not necessarily start a run or produce `agent_settled`.
- Preserve the current queue policy; do not silently turn ordinary messages into steering instructions.
- Extension dialogs are a separate protocol: forward `extension_ui_request` immediately and return `extension_ui_response` with the same ID. Waiting for run completion before displaying a blocking dialog would deadlock.
- The Amazon flow needs no custom dialog: send the browser link, finish the turn, then accept “c’est fait” as a new prompt in the same runtime.
- Keep cleanup on actual shutdown. New/switch session, reload, crashes and service restarts remain browser-state boundaries; persisted conversation history does not preserve a running extension.
- Cloud timeout still runs independently, and browser waiting remains billable.
- Preserve attachments: RPC rejects CLI `@file` arguments; images use `prompt.images`.
- Handle startup failures, stderr, framing, backpressure, cancellation and orderly shutdown explicitly.
- SDK with a durable Node.js/Bun host is a valid alternative but is not necessary for the existing Bash/Go bridge. In this version, `AgentSessionRuntime.dispose()` emits `session_shutdown`; `session.dispose()` alone is not equivalent to full CLI runtime cleanup.
- Existing unrelated changes were present in the Herald worktree. Future implementation must preserve them.

## Validation

Parent ran an isolated RPC lifecycle smoke test with no model prompts or browsers:

`pi --mode rpc --no-session --no-extensions --no-skills --no-prompt-templates --no-context-files --offline --no-approve`

Two successive `get_state` requests succeeded in the same process, with `isStreaming: false`. Closing stdin caused clean exit code 0. This validates the idle process baseline, not an end-to-end Telegram/Browser Use integration.

Oracle independently confirmed the recommendation and its lifecycle constraints. The research phase made no code changes; subsequent user-authorized implementation is recorded above.

## Primary sources

Official upstream documentation was fetched and compared with installed documentation:

- [RPC](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md): long-lived subprocess, framing, lifecycle, EOF shutdown.
- [CLI Integration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/cli-integration.md): print/JSON one-shot versus RPC long-lived.
- [RPC Commands](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc-commands.md): prompts, dispositions, queueing and session management.
- [RPC Extension UI](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc-extension-ui.md): blocking dialogs and correlated responses.
- [SDK](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sdk.md): in-process alternative.

Installed runtime paths below are relative to `/home/bastian/.local/lib/node_modules/@earendil-works/pi-coding-agent/`:

- `dist/modes/rpc/rpc-mode.js:265–269,579–600,612–653`: settled event handling, shutdown, UI responses, stdin EOF and persistent lifetime.
- `dist/core/agent-session-runtime.js:102–112,296–302`: teardown and shutdown event emission.
- `dist/core/agent-session.js:1011–1029,2998–3002`: session disposal and reload boundary.
- `dist/modes/print-mode.js:23–29`: print-mode runtime disposal.
- `examples/rpc-client.ts`: official typed subprocess client example; its final stop is example-owned, not required after each prompt.
