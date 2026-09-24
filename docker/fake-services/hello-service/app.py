
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import json
import os


CONFIG_PATH = Path(
    os.getenv("CONFIG_PATH", "/app/config.yaml")
)


def is_healthy() -> bool:
    if not CONFIG_PATH.exists():
        return False

    content = CONFIG_PATH.read_text()

    for line in content.splitlines():
        line = line.strip()

        if line == "healthy: true":
            return True

        if line == "healthy: false":
            return False

    return False


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            healthy = is_healthy()

            body = json.dumps({
                "status": "ok" if healthy else "unhealthy"
            }).encode()

            self.send_response(
                200 if healthy else 503
            )

            self.send_header(
                "Content-Type",
                "application/json"
            )

            self.send_header(
                "Content-Length",
                str(len(body))
            )

            self.end_headers()
            self.wfile.write(body)

            return

        if self.path == "/config":
            if not CONFIG_PATH.exists():
                self.send_response(404)
                self.end_headers()
                return

            body = CONFIG_PATH.read_bytes()

            self.send_response(200)

            self.send_header(
                "Content-Type",
                "text/plain"
            )

            self.send_header(
                "Content-Length",
                str(len(body))
            )

            self.end_headers()
            self.wfile.write(body)

            return

        self.send_response(404)
        self.end_headers()

    def log_message(self, format, *args):
        print(
            "%s - %s"
            % (self.address_string(), format % args)
        )


server = ThreadingHTTPServer(
    ("0.0.0.0", 8080),
    Handler,
)

print("hello-service started on :8080")
print(f"config={CONFIG_PATH}")

server.serve_forever()