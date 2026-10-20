#!/usr/bin/env python3
"""A local fake of the LaunchDarkly API for agent checks.

Point ldcli at it with --base-uri. It answers from a routes file and records
each request (method, path, query, some headers, JSON body) as one JSON line.
The headers are LD-API-Version, User-Agent, Content-Type, Accept, and
"authorization" (only "present" or "absent"). So a check
can assert what ldcli sent, for example that a dry run sent no write.

Routes file:
  {"routes": [
     {"method": "GET", "path": "/api/v2/projects/demo", "status": 200, "body": {...}},
     {"method": "PATCH", "path": "re:^/api/v2/flags/demo/[^/]+$", "status": 200, "body": {}},
     {"method": "*", "path": "re:.*", "status": 404, "body": {"code": "not_found"}}
  ]}
The first route that matches wins. Without a match the answer is 404.

Usage: fake_api.py --routes FILE --record FILE --port-file FILE
"""

import argparse
import http.server
import json
import re
import socketserver
import threading
import urllib.parse


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--routes", required=True)
    ap.add_argument("--record", required=True)
    ap.add_argument("--port-file", required=True)
    a = ap.parse_args()
    with open(a.routes) as f:
        routes = json.load(f)["routes"]
    lock = threading.Lock()

    def match(method, path):
        for r in routes:
            if r.get("method", "*") not in ("*", method):
                continue
            p = r.get("path", "")
            if (p.startswith("re:") and re.search(p[3:], path)) or p == path:
                return r
        return {"status": 404, "body": {"code": "not_found", "message": f"no fake route for {method} {path}"}}

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *_):
            pass

        def handle_any(self):
            length = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(length) if length else b""
            try:
                body = json.loads(raw) if raw else None
            except json.JSONDecodeError:
                body = raw.decode("utf-8", "replace")
            u = urllib.parse.urlsplit(self.path)
            headers = {k: v for k, v in self.headers.items()
                       if k.lower() in ("ld-api-version", "user-agent", "content-type", "accept")}
            headers["authorization"] = "present" if self.headers.get("Authorization") else "absent"
            with lock, open(a.record, "a") as f:
                f.write(json.dumps({"method": self.command, "path": u.path, "query": u.query,
                                    "headers": headers, "body": body}) + "\n")
            r = match(self.command, u.path)
            out = json.dumps(r.get("body", {})).encode()
            self.send_response(r.get("status", 200))
            self.send_header("Content-Type", "application/json")
            for k, v in (r.get("headers") or {}).items():
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(out)))
            self.end_headers()
            self.wfile.write(out)

        do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = handle_any

    class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True

    srv = Server(("127.0.0.1", 0), Handler)
    with open(a.port_file, "w") as f:
        f.write(str(srv.server_address[1]))
    srv.serve_forever()


if __name__ == "__main__":
    main()
