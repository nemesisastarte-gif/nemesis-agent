#!/usr/bin/env python3
import argparse
import json
import os
import shlex
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

requests = []


class Provider(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(size)
        requests.append(body.decode(errors="replace"))
        if b"PROVIDER_ERROR_TEST" in body:
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
            "NEMESIS_CONTEXT_LIMIT": "131072",
            "NEMESIS_OUTPUT_LIMIT": "8192",
            "OPENAI_API_KEY": "e2e-key",
            "ANTHROPIC_API_KEY": "e2e-key",
        })
        prefix = shlex.split(args.runner)

        def run(prompt, follow_up=False):
            turn_env = env.copy()
            turn_env["NEMESIS_CONTINUE"] = "1" if follow_up else "0"
            command = prefix + [
                args.engine,
                "--prompt", prompt,
                "--output-format", "json",
                "--quiet",
                "--cwd", workspace,
            ]
            return subprocess.run(command, env=turn_env, text=True, capture_output=True, timeout=90)

        first = run("FIRST_PORTABLE_TEST")
        assert first.returncode == 0, first.stderr
        assert "FIREWORKS_PORTABLE_OK" in first.stdout, first.stdout

        second = run("SECOND_PORTABLE_TEST", True)
        assert second.returncode == 0, second.stderr
        assert "FIREWORKS_PORTABLE_OK" in second.stdout, second.stdout
        assert "FIRST_PORTABLE_TEST" in requests[-1], "first turn missing from continued request"
        assert "SECOND_PORTABLE_TEST" in requests[-1], "second turn missing from continued request"

        failed = run("PROVIDER_ERROR_TEST", True)
        assert failed.returncode != 0, failed.stdout
        assert "FIREWORKS_PORTABLE_ERROR" in failed.stdout, failed.stdout

    server.shutdown()
    print("PORTABLE_ENGINE_E2E_OK")


if __name__ == "__main__":
    main()
