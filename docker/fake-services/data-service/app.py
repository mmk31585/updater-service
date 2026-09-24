from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os

CONFIG_PATH = os.getenv("CONFIG_PATH", "/app/config")

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            body = json.dumps({"status": "ok"}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == "/config":
            if not os.path.exists(CONFIG_PATH):
                self.send_response(404)
                self.end_headers()
                return
            with open(CONFIG_PATH, "rb") as f:
                content = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)
            return

        self.send_response(404)
        self.end_headers()

    def log_message(self, format, *args):
        print("%s - %s" % (self.address_string(), format % args))

server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
print("data-service started on :8080")
print("config=" + CONFIG_PATH)
server.serve_forever()
