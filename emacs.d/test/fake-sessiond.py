#!/usr/bin/env python3
"""fake-sessiond.py: a stand-in for sessiond's agent socket, for watching
what playground-agent.el sends while you press keys (emacs.md section 10.6).

    python3 fake-sessiond.py [--socket /run/playground/agent.sock] [--log FILE]

It listens on the Unix socket, prints every message from the agent (one
JSON object per line, pretty or compact), answers pings with pong, and
lets you send calls by typing lines on stdin (or writing them to the
FIFO given with --calls):

    eval (buffer-name)
    check (eq major-mode 'fundamental-mode) | (> (point) 1)
    rescue
    snapshot
    describe C-x C-f
    set-modules base agent editing
    open {"files":[{"path":"hello/README.md","point":1}],"layout":"single"}
    raw {"t":"call","id":99,"op":"eval","args":{"form":"1"},"timeoutMs":2000}

Only the Python standard library is used, so it runs in the session image.
"""

import argparse
import json
import os
import selectors
import socket
import sys
import time


def parse_command(line, next_id):
    """Turn a command line into a call message (dict), or None."""
    line = line.strip()
    if not line:
        return None
    op, _, rest = line.partition(" ")
    rest = rest.strip()
    if op == "raw":
        return json.loads(rest)
    if op == "eval":
        args = {"form": rest}
    elif op == "check":
        args = {"forms": [f.strip() for f in rest.split("|") if f.strip()]}
    elif op == "describe":
        args = {"keys": rest}
    elif op == "set-modules":
        args = {"enabled": rest.split()}
    elif op in ("open", "reset-step", "setup", "frame"):
        args = json.loads(rest) if rest else {}
        if op == "reset-step" and "spec" not in args:
            args = {"spec": args}
    else:
        args = json.loads(rest) if rest else {}
    return {"t": "call", "id": next_id, "op": op, "args": args, "timeoutMs": 5000}


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--socket", default=os.environ.get("PG_AGENT_SOCKET", "/run/playground/agent.sock"))
    ap.add_argument("--log", help="also append every received line to this file")
    ap.add_argument("--calls", help="read call lines from this FIFO as well as stdin")
    ap.add_argument("--compact", action="store_true", help="print one line per message")
    ap.add_argument("--quiet-pings", action="store_true", help="do not print pings")
    opts = ap.parse_args()

    path = opts.socket
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    if os.path.exists(path):
        os.unlink(path)
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    server.bind(path)
    server.listen(1)
    print(f"fake-sessiond listening on {path}", flush=True)

    log = open(opts.log, "a", buffering=1) if opts.log else None
    sel = selectors.DefaultSelector()
    sel.register(server, selectors.EVENT_READ, "accept")
    try:
        sel.register(sys.stdin, selectors.EVENT_READ, "stdin")
    except (ValueError, PermissionError, OSError):
        pass  # stdin is not selectable (e.g. a regular file); use --calls
    fifo = None
    if opts.calls:
        if not os.path.exists(opts.calls):
            os.mkfifo(opts.calls)
        fifo = os.open(opts.calls, os.O_RDONLY | os.O_NONBLOCK)
        sel.register(fifo, selectors.EVENT_READ, "fifo")

    conn = None
    buf = b""
    next_id = 1
    fifo_buf = b""

    def send(msg):
        if conn is None:
            print("! no agent connected", flush=True)
            return
        conn.sendall((json.dumps(msg) + "\n").encode())
        print(f">>> {json.dumps(msg)}", flush=True)

    while True:
        for key, _ in sel.select():
            if key.data == "accept":
                if conn is not None:
                    sel.unregister(conn)
                    conn.close()
                conn, _ = server.accept()
                buf = b""
                sel.register(conn, selectors.EVENT_READ, "conn")
                print("--- agent connected", flush=True)
            elif key.data == "conn":
                data = conn.recv(65536)
                if not data:
                    print("--- agent disconnected", flush=True)
                    sel.unregister(conn)
                    conn.close()
                    conn = None
                    continue
                buf += data
                while b"\n" in buf:
                    line, buf = buf.split(b"\n", 1)
                    text = line.decode("utf-8", errors="replace")
                    if log:
                        log.write(text + "\n")
                    try:
                        msg = json.loads(text)
                    except json.JSONDecodeError:
                        print(f"!!! not JSON: {text!r}", flush=True)
                        continue
                    if msg.get("t") == "ping":
                        conn.sendall(b'{"t":"pong"}\n')
                        if opts.quiet_pings:
                            continue
                    stamp = time.strftime("%H:%M:%S")
                    if opts.compact:
                        print(f"{stamp} {text}", flush=True)
                    else:
                        print(f"{stamp} {json.dumps(msg, indent=2, ensure_ascii=False)}", flush=True)
            elif key.data in ("stdin", "fifo"):
                if key.data == "stdin":
                    line = sys.stdin.readline()
                    if not line:
                        sel.unregister(sys.stdin)
                        continue
                    lines = [line]
                else:
                    try:
                        chunk = os.read(fifo, 65536)
                    except BlockingIOError:
                        chunk = b""
                    if not chunk:
                        # Writer closed: reopen so later writers are seen.
                        sel.unregister(fifo)
                        os.close(fifo)
                        fifo = os.open(opts.calls, os.O_RDONLY | os.O_NONBLOCK)
                        sel.register(fifo, selectors.EVENT_READ, "fifo")
                        continue
                    fifo_buf += chunk
                    lines = []
                    while b"\n" in fifo_buf:
                        one, fifo_buf = fifo_buf.split(b"\n", 1)
                        lines.append(one.decode())
                for line in lines:
                    try:
                        msg = parse_command(line, next_id)
                    except json.JSONDecodeError as err:
                        print(f"! bad JSON: {err}", flush=True)
                        continue
                    if msg is not None:
                        next_id += 1
                        send(msg)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
