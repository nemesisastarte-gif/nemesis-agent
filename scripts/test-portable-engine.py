#!/usr/bin/env python3
import argparse
import json
import os
import shlex
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

requests = []


class Provider(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(size)
        body_text = body.decode(errors="replace")
        requests.append(body_text)
        latest_user = ""
        try:
            payload = json.loads(body)
            for message in reversed(payload.get("messages", [])):
                if message.get("role") == "user":
                    latest_user = json.dumps(message.get("content", ""))
                    break
        except (TypeError, ValueError):
            latest_user = body_text

        if "PROVIDER_ERROR_TEST" in latest_user:
            payload = json.dumps({
                "error": {
                    "message": "FIREWORKS_PORTABLE_ERROR",
                    "type": "authentication_error",
                    "code": "invalid_api_key",
                }
            }).encode()
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        if "STREAM_TIMING_TEST" in latest_user:
            first = {
                "id": "stream",
                "object": "chat.completion.chunk",
                "created": 1,
                "model": "accounts/fireworks/models/e2e",
                "choices": [{"index": 0, "delta": {"role": "assistant", "content": "STREAM_FIRST"}, "finish_reason": None}],
            }
            last = {
                "id": "stream",
                "object": "chat.completion.chunk",
                "created": 1,
                "model": "accounts/fireworks/models/e2e",
                "choices": [{"index": 0, "delta": {"content": "STREAM_SECOND"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 8, "completion_tokens": 3, "total_tokens": 11},
            }
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(("data: " + json.dumps(first) + "\n\n").encode())
            self.wfile.flush()
            time.sleep(2.0)
            self.wfile.write(("data: " + json.dumps(last) + "\n\ndata: [DONE]\n\n").encode())
            self.wfile.flush()
            return

        if "TOOL_PORTABLE_TEST" in latest_user and '"role":"tool"' not in body_text:
            arguments = json.dumps({"file_path": "portable-tool-ok.txt", "content": "PORTABLE_TOOL_OK\n"})
            chunks = [
                {
                    "id": "tool",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "accounts/fireworks/models/e2e",
                    "choices": [{
                        "index": 0,
                        "delta": {
                            "role": "assistant",
                            "tool_calls": [{
                                "index": 0,
                                "id": "call_write",
                                "type": "function",
                                "function": {"name": "write", "arguments": arguments},
                            }],
                        },
                        "finish_reason": None,
                    }],
                },
                {
                    "id": "tool",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "accounts/fireworks/models/e2e",
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}],
                    "usage": {"prompt_tokens": 8, "completion_tokens": 3, "total_tokens": 11},
                },
            ]
        else:
            chunks = [
                {
                    "id": "portable",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "accounts/fireworks/models/e2e",
                    "choices": [{
                        "index": 0,
                        "delta": {"role": "assistant", "content": "FIREWORKS_PORTABLE_OK"},
                        "finish_reason": None,
                    }],
                },
                {
                    "id": "portable",
                    "object": "chat.completion.chunk",
                    "created": 1,
                    "model": "accounts/fireworks/models/e2e",
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 8, "completion_tokens": 3, "total_tokens": 11},
                },
            ]
        payload = ("".join("data: " + json.dumps(chunk) + "\n\n" for chunk in chunks) + "data: [DONE]\n\n").encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Connection", "close")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--engine", required=True)
    parser.add_argument("--runner", default="")
    args = parser.parse_args()

    server = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    with tempfile.TemporaryDirectory() as root:
        home = os.path.join(root, "home")
        workspace = os.path.join(root, "workspace")
        os.makedirs(home)
        os.makedirs(workspace)
        env = os.environ.copy()
        env.update({
            "HOME": home,
            "NEMESIS_PROTOCOL": "1",
            "NEMESIS_MODEL": "accounts/fireworks/models/e2e",
            "NEMESIS_BASE_URL": f"http://127.0.0.1:{server.server_port}/v1",
            "NEMESIS_INTERFACE_TYPE": "openai_chat",
            "NEMESIS_THINKING_ENABLED": "false",
            "NEMESIS_CONTEXT_LIMIT": "131072",
            "NEMESIS_OUTPUT_LIMIT": "8192",
            "OPENAI_API_KEY": "e2e-key",
            "ANTHROPIC_API_KEY": "e2e-key",
        })
        prefix = shlex.split(args.runner)

        def command_for(prompt):
            return prefix + [
                args.engine,
                "--prompt", prompt,
                "--output-format", "json",
                "--quiet",
                "--cwd", workspace,
            ]

        def turn_env(follow_up):
            result = env.copy()
            result["NEMESIS_CONTINUE"] = "1" if follow_up else "0"
            return result

        def run(prompt, follow_up=False):
            return subprocess.run(command_for(prompt), env=turn_env(follow_up), text=True, capture_output=True, timeout=90)

        first = run("FIRST_PORTABLE_TEST")
        assert first.returncode == 0, first.stderr
        assert "FIREWORKS_PORTABLE_OK" in first.stdout, first.stdout
        assert first.stdout.count('"type":"text"') == 2, "one event and one part type expected"
        assert requests[-1].count("FIRST_PORTABLE_TEST") == 1, "user prompt was duplicated"
        assert "You are NemesisCode" in requests[-1], "NemesisCode identity missing from system prompt"
        assert "You are operating as and within the OpenCode CLI" not in requests[-1]

        started = time.monotonic()
        streaming = subprocess.Popen(
            command_for("STREAM_TIMING_TEST"),
            env=turn_env(True),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        first_line = streaming.stdout.readline()
        first_delay = time.monotonic() - started
        assert "STREAM_FIRST" in first_line, first_line
        assert first_delay < 1.8, f"first streamed chunk took {first_delay:.2f}s"
        assert streaming.poll() is None, "engine waited for the complete response before streaming"
        remaining, stream_stderr = streaming.communicate(timeout=10)
        assert streaming.returncode == 0, stream_stderr
        assert "STREAM_SECOND" in remaining, remaining
        print(f"STREAM_FIRST_CHUNK_SECONDS={first_delay:.3f}")

        second = run("SECOND_PORTABLE_TEST", True)
        assert second.returncode == 0, second.stderr
        assert "FIREWORKS_PORTABLE_OK" in second.stdout, second.stdout
        assert "FIRST_PORTABLE_TEST" in requests[-1], "first turn missing from continued request"
        assert "SECOND_PORTABLE_TEST" in requests[-1], "second turn missing from continued request"

        tool = run("TOOL_PORTABLE_TEST", True)
        assert tool.returncode == 0, tool.stderr
        assert "FIREWORKS_PORTABLE_OK" in tool.stdout, tool.stdout
        assert '"type":"tool_use_start"' in tool.stdout, tool.stdout
        assert '"type":"tool_use_stop"' in tool.stdout, tool.stdout
        assert '"tool":"write"' in tool.stdout, tool.stdout
        assert "PORTABLE_TOOL_OK" in tool.stdout, tool.stdout
        tool_path = os.path.join(workspace, "portable-tool-ok.txt")
        with open(tool_path, encoding="utf-8") as handle:
            assert handle.read() == "PORTABLE_TOOL_OK\n", "write tool did not create expected file"

        failed = run("PROVIDER_ERROR_TEST", True)
        assert failed.returncode != 0, failed.stdout
        assert "FIREWORKS_PORTABLE_ERROR" in failed.stdout, failed.stdout

    server.shutdown()
    print("PORTABLE_ENGINE_E2E_OK")


if __name__ == "__main__":
    main()
