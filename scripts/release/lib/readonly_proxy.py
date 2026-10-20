#!/usr/bin/env python3
"""A local HTTP proxy to a live LaunchDarkly API that allows only reads.

verify.sh starts it for --phase units when RELEASE_VERIFY_LIVE_TOKEN and
RELEASE_VERIFY_LIVE_BASE_URI are set. Checks call ldcli with
--base-uri "$RELEASE_LIVE_BASE_URI" and any placeholder token. The proxy
replaces the Authorization header with the real token, so checks never see
the token. It refuses every method except GET and HEAD with 405, unless
--allow-writes is set, and it logs each request (method, path, status).

Usage: RV_TOKEN=... readonly_proxy.py --upstream URL --port-file FILE --log FILE [--allow-writes]
"""

import argparse
import http.server
import os
import socketserver
import sys
import threading
import urllib.error
import urllib.request

HOP = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers",
       "transfer-encoding", "upgrade", "host", "authorization", "content-length", "accept-encoding"}


def make_handler(upstream, token, allow_writes, log_path):
    lock = threading.Lock()

    def log(line):
        with lock, open(log_path, "a") as f:
            f.write(line + "\n")

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *_):
            pass

        def _send(self, status, body=b"", headers=None):
            self.send_response(status)
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(body)

        def _proxy(self):
            length = int(self.headers.get("Content-Length") or 0)
            data = self.rfile.read(length) if length else None
            if self.command not in ("GET", "HEAD") and not allow_writes:
                log(f"REFUSED {self.command} {self.path}")
                self._send(405, b'{"code":"read_only","message":"the release verifier allows only reads"}',
                           {"Content-Type": "application/json"})
                return
            req = urllib.request.Request(upstream.rstrip("/") + self.path, data=data, method=self.command)
            for k, v in self.headers.items():
                if k.lower() not in HOP:
                    req.add_header(k, v)
            req.add_header("Authorization", token)
            try:
                with urllib.request.urlopen(req, timeout=60) as resp:
                    body, status, headers = resp.read(), resp.status, resp.headers
            except urllib.error.HTTPError as e:
                body, status, headers = e.read(), e.code, e.headers
            except Exception as e:  # noqa: BLE001
                log(f"ERROR {self.command} {self.path} {type(e).__name__}")
                self._send(502, b'{"code":"proxy_error"}', {"Content-Type": "application/json"})
                return
            log(f"{status} {self.command} {self.path.split('?')[0]}")
            out = {k: v for k, v in headers.items() if k.lower() not in HOP}
            self._send(status, body, out)

        do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = _proxy

    return Handler


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream", required=True)
    ap.add_argument("--port-file", required=True)
    ap.add_argument("--log", required=True)
    ap.add_argument("--allow-writes", action="store_true")
    a = ap.parse_args()
    token = os.environ.pop("RV_TOKEN", "")
    if not token:
        sys.exit("RV_TOKEN is not set")

    class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True
        allow_reuse_address = True

    srv = Server(("127.0.0.1", 0), make_handler(a.upstream, token, a.allow_writes, a.log))
    with open(a.port_file, "w") as f:
        f.write(str(srv.server_address[1]))
    srv.serve_forever()


if __name__ == "__main__":
    main()
