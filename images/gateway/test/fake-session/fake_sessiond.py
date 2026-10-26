"""Stand-in sessiond for a server-mode gateway test: /healthz, /status,
/rpc/<op>; on start it posts a heartbeat to the gateway with its token."""
import json
import os
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SID = os.environ.get("PG_SESSION_ID", "")
TOKEN = os.environ.get("PG_SESSION_TOKEN", "")
GW = os.environ.get("PG_GATEWAY_URL", "")


class H(BaseHTTPRequestHandler):
    def reply(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.reply(200, {"ok": True})
        elif self.path == "/status":
            self.reply(200, {"sessionId": SID, "emacs": {"version": "fake", "pid": 1, "uptimeMs": 1, "agent": True, "responsive": True},
                             "frames": {"terminal": True, "gui": False}, "mode": "terminal", "step": None, "modules": [],
                             "activeWebSockets": 0, "user": self.headers.get("X-Playground-User")})
        else:
            self.reply(404, {"status": 404, "code": "not_found", "message": "no"})

    def do_POST(self):
        self.reply(200, {"ok": True, "result": {}})


def heartbeat():
    time.sleep(1)
    try:
        req = urllib.request.Request(GW + "/internal/sessions/" + SID + "/heartbeat", data=b'{"activeWebSockets":0}',
                                     headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"}, method="POST")
        with urllib.request.urlopen(req, timeout=5) as r:
            print("heartbeat", r.status, flush=True)
    except Exception as e:  # noqa: BLE001
        print("heartbeat failed", e, flush=True)


threading.Thread(target=heartbeat, daemon=True).start()
ThreadingHTTPServer(("0.0.0.0", 7000), H).serve_forever()
