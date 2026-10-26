# web/dev: the fake session

`fake-session.ts` stands in for the gateway and one session (api.md §2 and §3)
so the web app can be developed and tested without Docker, Emacs or tmux. A
port of the design demo's mock engine (`mock-emacs.ts`) drives it: typing in
the terminal moves a fake Emacs that emits `key`, `prefix`, `state`, `echo`
and `minibuffer` events; scenarios load, steps are checked, checkpoints
restore, the GUI frame is a tiny RFB server, previews (Typst to PDF) rebuild
on save.

```sh
pnpm dev:fake                                    # fake on :8090 and Vite on :5173 proxying to it
node dev/fake-session.ts --port 8090 --static dist          # serve a built bundle (browser tests)
node dev/fake-session.ts --auth invite --author             # login screen (code: emacs), author panel
node dev/fake-session.ts --curriculum ../artifacts/curriculum   # a bundle from playctl curriculum build
```

Lessons with hand-written scenarios live in `lessons.ts` (0.2, 2.3, 3.1 and
project P6); every other ready lesson in `fixtures/index.json` gets a generic
one-step scenario. Like the demo engine it is small on purpose: windows are a
flat list with one orientation per frame (C-x 2 then C-x 3 draws three
columns), and only the commands in `mock-emacs.ts` exist.

## Contract test against a real gateway

`--sessiond` makes the fake also answer sessiond's own paths (`/events`,
`/pty`, `/vnc`, `/rpc/{op}`, `/preview/…`, `/status`) without the `/s/{id}`
prefix, and `--export-bundle DIR` writes the built-in lessons in the layout
of `playctl curriculum build` (internal/protocol/curriculum.go). Together they
put the real gateway (local mode, static backend) between the browser and the
fake:

```sh
node dev/fake-session.ts --export-bundle ~/.cache/pg-ct/bundle
node dev/fake-session.ts --port 8098 --sessiond &
pnpm build && make gateway        # bin/gateway with web/dist embedded (-tags embedweb)
cat > ~/.cache/pg-ct/config.json <<'EOF'
{ "mode": "local", "listen": "127.0.0.1:8097", "publicUrl": "http://127.0.0.1:8097",
  "dataDir": "/home/zsy/.cache/pg-ct/data", "auth": { "mode": "none" },
  "local": { "sessionUrl": "http://127.0.0.1:8098" },
  "curriculum": { "dir": "/home/zsy/.cache/pg-ct/bundle" } }
EOF
bin/gateway -config ~/.cache/pg-ct/config.json &
PG_CDP=http://127.0.0.1:9222 PG_URL=http://127.0.0.1:8097 PG_OUT=../artifacts node e2e/lesson.mjs
```

Use paths under `/home/zsy` (the remote Docker daemon, and so the Chrome
container, cannot see `/tmp`), and stop the background processes by pid.
