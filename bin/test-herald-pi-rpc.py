#!/usr/bin/env python3
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("herald_rpc", Path(__file__).with_name("herald-pi-rpc.py"))
rpc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rpc)


class WorkerTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.queue = self.root / "queue"
        self.queue.mkdir()
        (self.queue / "generation").write_text("0")
        self.worker = rpc.Worker(str(self.queue), str(self.root), str(self.root),
                                 str(self.root / "pid"), str(self.root / "marker"), "HTML", "system")
        self.sent = []
        self.worker.send = lambda chat, text: self.sent.append((chat, text))
        self.worker.process = types.SimpleNamespace(stdin=io.BytesIO())
        self.worker.runtime_generation = "0"

    def tearDown(self):
        self.worker.selector.close()
        self.tmp.cleanup()

    def task(self, name="task-1", prompt="hello", chat="123", images=()):
        path = self.queue / name
        path.mkdir()
        (path / "chat_id").write_text(chat)
        (path / "prompt").write_text(prompt)
        (path / "generation").write_text("0")
        (path / "images").write_text("\n".join(str(image) for image in images))
        return self.worker.task(path)

    def message(self, text, **extra):
        self.worker.handle({"type": "message_end", "message": {
            "role": "assistant", "content": [{"type": "text", "text": text}], **extra}})

    def commands(self):
        return [json.loads(line) for line in self.worker.process.stdin.getvalue().split(b"\n") if line]

    def test_waits_for_settled_and_preserves_unicode(self):
        self.worker.submit(self.task(prompt="écrit\u2028ici"))
        self.assertEqual(self.commands()[0]["message"], "écrit\u2028ici")
        self.message("temporary error", stopReason="error", errorMessage="transient")
        self.worker.handle({"type": "agent_end", "willRetry": True})
        self.assertEqual(self.sent, [])
        self.message("final answer")
        self.worker.handle({"type": "agent_settled"})
        self.assertEqual(self.sent, [("123", "final answer")])
        self.assertIsNone(self.worker.active)
        self.assertFalse(self.worker.busy_file.exists())
        self.assertIsNotNone(self.worker.process)

    def test_images_use_rpc_blocks_and_are_removed_after_response(self):
        image = self.root / "image.png"
        image.write_bytes(b"\x89PNG\r\n\x1a\nfixture")
        self.worker.submit(self.task(images=[image]))
        blocks = self.commands()[0]["images"]
        self.assertEqual(blocks[0]["mimeType"], "image/png")
        self.assertEqual(blocks[0]["type"], "image")
        self.assertTrue(image.exists())
        self.message("image seen")
        self.worker.handle({"type": "agent_settled"})
        self.assertFalse(image.exists())

    def test_rejected_and_handled_prompts_do_not_wait_forever(self):
        for success in (False, True):
            with self.subTest(success=success):
                self.worker.submit(self.task(name="task-" + str(success)))
                self.worker.handle({"type": "response", "id": self.worker.request_id,
                                    "success": success, "error": "bad <input>",
                                    "data": {"disposition": "handled"}})
                self.assertIsNone(self.worker.active)
        self.assertEqual(len(self.sent), 1)
        self.assertIn("&lt;input&gt;", self.sent[0][1])

    def test_dialog_is_forwarded_before_settled_and_answered_without_new_prompt(self):
        self.worker.submit(self.task())
        self.worker.handle({"type": "extension_ui_request", "id": "dialog-1",
                            "method": "confirm", "title": "Continuer ?"})
        self.assertIn("Continuer ?", self.sent[0][1])
        answer = self.task("task-2", f"/answer {self.worker.dialog['reply_token']} oui")
        self.worker.answer_dialog(answer)
        self.assertEqual(self.commands()[-1], {"type": "extension_ui_response",
                                              "id": "dialog-1", "confirmed": True})
        self.assertEqual(len(self.commands()), 2)
        self.assertFalse(answer["path"].exists())
        self.assertIsNotNone(self.worker.active)

    def test_preexisting_yes_does_not_answer_later_confirmation(self):
        self.worker.submit(self.task())
        old_message = self.task("task-2", "oui")
        self.worker.handle({"type": "extension_ui_request", "id": "confirm-1",
                            "method": "confirm", "title": "Autoriser ?"})
        self.assertFalse(self.worker.answer_dialog(old_message))
        self.assertTrue(old_message["path"].exists())
        self.assertEqual(len(self.commands()), 1)
        token = self.worker.dialog["reply_token"]
        wrong_chat = self.task("task-3", f"/answer {token} oui", chat="456")
        self.assertFalse(self.worker.answer_dialog(wrong_chat))
        self.assertTrue(wrong_chat["path"].exists())
        correct = self.task("task-4", f"/answer {token} oui")
        self.assertTrue(self.worker.answer_dialog(correct))
        self.assertEqual(self.commands()[-1]["confirmed"], True)
        self.assertTrue(old_message["path"].exists())

    def test_expired_dialog_leaves_later_message_as_normal_queued_prompt(self):
        self.worker.submit(self.task())
        with patch.object(rpc.time, "monotonic", return_value=100):
            self.worker.handle({"type": "extension_ui_request", "id": "expired",
                                "method": "confirm", "title": "Autoriser ?", "timeout": 1})
        ordinary = self.task("task-2", "nouvelle instruction")
        token = self.worker.dialog["reply_token"]
        late_answer = self.task("task-3", f"/answer {token} oui")
        with patch.object(rpc.time, "monotonic", return_value=101):
            self.assertFalse(self.worker.answer_dialog(ordinary))
            self.assertFalse(self.worker.answer_dialog(late_answer))
        self.assertIsNone(self.worker.dialog)
        self.assertTrue(ordinary["path"].exists())
        self.assertTrue(late_answer["path"].exists())
        self.assertEqual(len(self.commands()), 1)
        self.assertEqual(len(self.sent), 1)  # only the dialog, no rejected-message notice

    def test_old_generation_cannot_send_response_or_overwrite_session_marker(self):
        self.worker.submit(self.task())
        (self.queue / "generation").write_text("1")
        self.worker.marker.write_text("__new__")
        self.message("stale output")
        self.worker.handle({"type": "agent_settled"})
        self.worker.handle({"type": "response", "id": "session-state",
                            "data": {"sessionId": "old-session"}})
        self.assertEqual(self.sent, [])
        self.assertEqual(self.worker.marker.read_text(), "__new__")
        self.worker.finish()

    def test_stop_closes_stdin_and_allows_runtime_cleanup(self):
        executable = self.root / "pi"
        done = self.root / "cleanup-done"
        executable.write_text("#!/usr/bin/env python3\nimport sys, time\nfrom pathlib import Path\n"
                              "for line in sys.stdin: pass\n"
                              "sys.stdout.write('x' * 200000)\nsys.stdout.flush()\ntime.sleep(0.2)\n"
                              f"Path({str(done)!r}).write_text('cleaned')\n")
        executable.chmod(0o755)
        self.worker.process = None
        with patch.dict(os.environ, {"PATH": str(self.root) + os.pathsep + os.environ["PATH"]}):
            self.worker.start("0")
            process = self.worker.process
            self.worker.stop()
        self.assertEqual(process.returncode, 0)
        self.assertEqual(done.read_text(), "cleaned")
        self.assertFalse(self.worker.pid_file.exists())

    def test_session_selection_preserves_marker_and_new_semantics(self):
        for name, session_id in [("a", "first"), ("b", "second")]:
            (self.root / (name + ".jsonl")).write_text(json.dumps({"id": session_id}) + "\n")
        self.worker.marker.write_text("first")
        self.assertEqual(self.worker.session_id(), "first")
        self.worker.marker.write_text("__new__")
        self.assertIsNone(self.worker.session_id())


if __name__ == "__main__":
    unittest.main()
