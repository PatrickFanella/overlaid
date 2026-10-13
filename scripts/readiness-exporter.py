#!/usr/bin/env python3
"""Read-only Prometheus adapter for Overlaid's JSON readiness and web process."""
import json
import os
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

API_URL = os.environ.get("OVERLAID_READINESS_URL", "http://127.0.0.1:3084/readyz")
WEB_URL = os.environ.get("OVERLAID_WEB_URL", "http://10.0.0.56:3083/status")
CHECKS = ("process", "tenant_storage", "retention_worker", "access_policy")


def roles():
    result = {name: 0 for name in CHECKS}
    result["web"] = 0
    try:
        with urllib.request.urlopen(API_URL, timeout=3) as response:
            report = json.loads(response.read(65537))
        checks = {item["name"]: item["status"] for item in report["checks"]}
        for name in CHECKS:
            result[name] = int(checks.get(name) == "ok")
    except (OSError, ValueError, KeyError, TypeError):
        pass
    try:
        with urllib.request.urlopen(WEB_URL, timeout=3) as response:
            body = response.read(65537)
            result["web"] = int(response.status == 200 and b'<div id="root"' in body)
    except OSError:
        pass
    return result


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/metrics":
            self.send_error(404)
            return
        body = '# HELP overlaid_role_ready Role readiness from JSON checks, not HTTP status alone.\n# TYPE overlaid_role_ready gauge\n'
        body += "".join(f'overlaid_role_ready{{role="{role}"}} {value}\n' for role, value in roles().items())
        payload = body.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; version=0.0.4")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer((os.environ.get("OVERLAID_EXPORTER_HOST", "10.0.0.56"), int(os.environ.get("OVERLAID_EXPORTER_PORT", "3085"))), Handler).serve_forever()
