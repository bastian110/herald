#!/usr/bin/env python3
"""Consume Herald's sequential queue while keeping one Pi RPC runtime alive."""
import base64
import html
import json
import os
from pathlib import Path
import selectors
import shutil
import signal
import subprocess
import sys
import time
import uuid


class Worker:
    def __init__(self, queue, sessions, cwd, pid_file, marker, parse_mode, system_prompt):
        self.queue, self.sessions = Path(queue), Path(sessions)
        self.cwd, self.pid_file, self.marker = cwd, Path(pid_file), Path(marker)
        self.busy_file = Path(pid_file + ".busy")
        self.parse_mode, self.system_prompt = parse_mode, system_prompt
        self.process = None
        self.selector = selectors.DefaultSelector()
        self.buffer = b""
        self.active = None
        self.last_text = ""
        self.error = ""
        self.dialog = None
        self.request_id = None
        self.runtime_generation = None
        self.running = True
        self.acknowledged_generation = None

    def generation(self):
        return (self.queue / "generation").read_text().strip()

    def send(self, chat, text):
        args = ["herald", "send", "--chat-id", chat]
        if self.parse_mode:
            result = subprocess.run(args + ["--parse-mode", self.parse_mode, text], timeout=30)
            if result.returncode == 0:
                return
        subprocess.run(args + [text], timeout=30, check=True)

    def session_id(self):
        marker = self.marker.read_text().strip() if self.marker.exists() else ""
        if marker == "__new__":
            return None
        sessions = []
        for path in self.sessions.glob("*.jsonl"):
            try:
                with path.open() as handle:
                    session_id = json.loads(handle.readline())["id"]
                if session_id == marker:
                    return marker
                sessions.append((path.stat().st_mtime_ns, session_id))
            except (OSError, ValueError, KeyError):
                continue
        return max(sessions)[1] if sessions else None

    def start(self, generation):
        args = ["pi", "--mode", "rpc", "--session-dir", str(self.sessions),
                "--append-system-prompt", self.system_prompt]
        session_id = self.session_id()
        if session_id:
            args += ["--session", session_id]
        self.runtime_generation = generation
        # Only this worker owns Pi shutdown, including on bridge/service signals.
        self.process = subprocess.Popen(args, cwd=self.cwd, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, start_new_session=True)
        os.set_blocking(self.process.stdout.fileno(), False)
        self.selector.register(self.process.stdout, selectors.EVENT_READ)
        self.pid_file.write_text(str(self.process.pid) + "\n")
        self.buffer = b""
        self.command({"type": "get_state", "id": "session-state"})

    def command(self, record):
        self.process.stdin.write((json.dumps(record, ensure_ascii=False) + "\n").encode())
        self.process.stdin.flush()

    def stop(self):
        if self.process:
            try:
                self.process.stdin.close()
            except BrokenPipeError:
                pass
            # EOF is the sole graceful shutdown request. Drain output so cleanup
            # cannot stall on backpressure; a second signal aborts Pi 1.4 cleanup.
            deadline = time.monotonic() + 30
            while self.process.poll() is None and time.monotonic() < deadline:
                for key, _ in self.selector.select(timeout=0.05):
                    if not os.read(key.fileobj.fileno(), 65536):
                        self.selector.unregister(key.fileobj)
            if self.process.poll() is None:
                print("herald-pi-rpc: cleanup timed out; forcing exit, Cloud browser may remain billable", file=sys.stderr)
                os.killpg(self.process.pid, signal.SIGKILL)
            self.process.wait()
            if self.process.stdout in self.selector.get_map():
                self.selector.unregister(self.process.stdout)
            self.process.stdout.close()
            self.process = None
        self.pid_file.unlink(missing_ok=True)
        self.busy_file.unlink(missing_ok=True)
        self.dialog = None
        self.buffer = b""

    @staticmethod
    def task(path):
        return {"path": path, "chat": (path / "chat_id").read_text().strip(),
                "prompt": (path / "prompt").read_text(),
                "generation": (path / "generation").read_text().strip(),
                "images": (path / "images").read_text().splitlines()}

    @staticmethod
    def discard(task):
        for image in task["images"]:
            Path(image).unlink(missing_ok=True)
        shutil.rmtree(task["path"], ignore_errors=True)

    def finish(self, text=None):
        if self.active:
            try:
                if self.active["generation"] == self.generation() and text:
                    self.send(self.active["chat"], text)
            finally:
                self.discard(self.active)
                self.active = None
        self.busy_file.unlink(missing_ok=True)
        self.dialog = None
        self.request_id = None
        self.last_text = self.error = ""

    def submit(self, task):
        images = []
        for name in task["images"]:
            data = Path(name).read_bytes()
            if data.startswith(b"\x89PNG\r\n\x1a\n"):
                mime = "image/png"
            elif data.startswith(b"\xff\xd8\xff"):
                mime = "image/jpeg"
            elif data.startswith((b"GIF87a", b"GIF89a")):
                mime = "image/gif"
            elif data.startswith(b"RIFF") and data[8:12] == b"WEBP":
                mime = "image/webp"
            else:
                raise ValueError("unsupported image format")
            images.append({"type": "image", "data": base64.b64encode(data).decode(), "mimeType": mime})
        if not self.process:
            self.start(task["generation"])
        if task["generation"] != self.generation():
            self.discard(task)
            self.stop()
            return
        self.active = task
        self.busy_file.write_text(task["chat"])
        self.request_id = str(uuid.uuid4())
        self.command({"type": "prompt", "id": self.request_id,
                      "message": task["prompt"], "images": images})

    def handle(self, record):
        kind = record.get("type")
        if kind == "response" and record.get("id") == "session-state":
            session_id = record.get("data", {}).get("sessionId")
            if session_id and self.runtime_generation == self.generation():
                self.marker.write_text(session_id + "\n")
        if not self.active or self.active["generation"] != self.generation():
            return
        if kind == "message_end" and record.get("message", {}).get("role") == "assistant":
            message = record["message"]
            self.last_text = "\n".join(block["text"] for block in message.get("content", [])
                                       if block.get("type") == "text")
            self.error = message.get("errorMessage", "") if message.get("stopReason") in ("error", "aborted") else ""
        elif kind == "response" and record.get("id") == self.request_id:
            if not record.get("success"):
                self.finish("⚠️ Pi a refusé le message : " + html.escape(record.get("error", "erreur RPC")))
            elif record.get("data", {}).get("disposition") == "handled":
                self.command({"type": "get_state", "id": "session-state"})
                self.finish()
        elif kind == "agent_settled":
            self.command({"type": "get_state", "id": "session-state"})
            text = self.last_text or "(pi: réponse vide)"
            if self.error:
                text = "⚠️ Pi : " + html.escape(self.error) + "\n" + self.last_text
            elif record.get("aborted"):
                text = "⚠️ Réponse Pi interrompue.\n" + self.last_text
            self.finish(text)
        elif kind == "extension_ui_request":
            method = record.get("method")
            if method in ("input", "editor", "confirm", "select"):
                self.dialog = dict(record, reply_token=uuid.uuid4().hex,
                                   deadline=(time.monotonic() + record["timeout"] / 1000)
                                   if record.get("timeout") is not None else float("inf"))
                title = html.escape(record.get("title", "Confirmation Pi"))
                body = html.escape(record.get("message", ""))
                options = "\n".join(html.escape(option) for option in record.get("options", []))
                example = "oui" if method == "confirm" else "ta réponse"
                hint = (f"Réponds avec : /answer {self.dialog['reply_token']} {example}\n"
                        f"Pour annuler : /answer {self.dialog['reply_token']} /cancel")
                self.send(self.active["chat"], "\n".join(filter(None, [title, body, options, hint])))
            elif method == "notify":
                self.send(self.active["chat"], html.escape(record.get("message", "Notification Pi")))

    def answer_dialog(self, task):
        if self.dialog and time.monotonic() >= self.dialog["deadline"]:
            self.dialog = None
        if not self.dialog or task["chat"] != self.active["chat"]:
            return False
        prefix = f"/answer {self.dialog['reply_token']} "
        if not task["prompt"].startswith(prefix):
            return False
        value = task["prompt"][len(prefix):].strip()
        response = {"type": "extension_ui_response", "id": self.dialog["id"]}
        if value == "/cancel":
            response["cancelled"] = True
        elif self.dialog["method"] == "confirm":
            if value.lower() not in ("oui", "non", "yes", "no"):
                self.send(task["chat"], f"Réponds avec {prefix}oui, {prefix}non ou {prefix}/cancel.")
                self.discard(task)
                return True
            response["confirmed"] = value.lower() in ("oui", "yes")
        elif self.dialog["method"] == "select" and value not in self.dialog.get("options", []):
            self.send(task["chat"], f"Réponds avec {prefix} suivi d’un choix proposé (ou /cancel).")
            self.discard(task)
            return True
        else:
            response["value"] = value
        self.command(response)
        self.dialog = None
        self.discard(task)
        return True

    def tick(self):
        generation = self.generation()
        if self.process and self.runtime_generation != generation:
            self.finish()
            self.stop()
        if self.acknowledged_generation != generation:
            (self.queue / "stopped-generation").write_text(generation)
            self.acknowledged_generation = generation
        if self.process:
            for key, _ in self.selector.select(timeout=0.1):
                chunk = os.read(key.fileobj.fileno(), 65536)
                if not chunk:
                    self.finish("⚠️ Le processus Pi s’est arrêté. Renvoie ton message pour réessayer ; le navigateur peut avoir été fermé.")
                    self.stop()
                    break
                self.buffer += chunk
                while b"\n" in self.buffer:
                    line, self.buffer = self.buffer.split(b"\n", 1)
                    self.handle(json.loads(line))
        else:
            time.sleep(0.1)
        if not self.running:
            return
        for path in sorted(self.queue.glob("task-*")):
            if self.active and path == self.active["path"]:
                continue
            try:
                task = self.task(path)
            except FileNotFoundError:  # /new may clear queued tasks concurrently
                continue
            if task["generation"] != self.generation():
                self.discard(task)
                continue
            if self.active:
                if self.answer_dialog(task):
                    break
                if self.dialog:
                    continue  # An ordinary queued prompt must not block a correlated reply.
                break
            try:
                self.submit(task)
            except (OSError, ValueError) as error:
                self.active = task
                self.finish("⚠️ Impossible de transmettre le message à Pi. Vérifie les journaux du bridge.")
                print(f"herald-pi-rpc: {type(error).__name__}", file=sys.stderr)
                self.stop()
            break

    def run(self):
        def shutdown(_signum, _frame):
            self.running = False
        signal.signal(signal.SIGTERM, shutdown)
        signal.signal(signal.SIGINT, shutdown)
        try:
            while self.running:
                try:
                    self.tick()
                except (OSError, ValueError, subprocess.SubprocessError) as error:
                    print(f"herald-pi-rpc: {type(error).__name__}", file=sys.stderr)
                    self.finish("⚠️ Erreur de communication avec Pi. Renvoie ton message pour réessayer.")
                    self.stop()
        finally:
            self.stop()
            self.selector.close()


if __name__ == "__main__":
    Worker(*sys.argv[1:]).run()
