#!/usr/bin/env python3
"""Replay lesson demos against a real Emacs 31.1 and check every step.

A stand-in for `playctl curriculum play` (CURRICULUM.md Part B.5 step 3)
until the session image and sessiond exist. For each lesson it copies the
fixtures into a scratch workspace, starts Emacs in the pg-curriculum-play
container (see Dockerfile; build it first), applies the scenario's setup,
feeds each step's demo to the command loop and records a snapshot after
every command (driver.el). The conditions are then evaluated on the
snapshots with the same semantics as sessiond's step engine:

  * a step completes at the first snapshot where every condition holds;
  * it FAILS when no snapshot satisfies all conditions;
  * it WARNS when it completes before the demo has finished (the remaining
    keys would spill into the next step) or is already complete at start.

Usage:
  docker build -t pg-curriculum-play curriculum/tools/play
  python3 curriculum/tools/play/play.py [--keep] [-v] LESSON_ID... | --all

Scratch files go to artifacts/play/ (git-ignored). Shell conditions are not
evaluated here (reported as unknown).
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))

import yaml  # noqa: E402

import pgcurriculum as pc  # noqa: E402

CURRICULUM = HERE.parent.parent
REPO = CURRICULUM.parent
SCRATCH = REPO / "artifacts" / "play"
IMAGE = os.environ.get("PG_PLAY_IMAGE", "pg-curriculum-play")
LESSON_TIMEOUT = int(os.environ.get("PG_PLAY_TIMEOUT", "300"))


def load_yaml(p: Path):
    with open(p, encoding="utf-8") as f:
        return yaml.safe_load(f)


def text_of(t) -> str:
    if isinstance(t, dict):
        return t.get("en") or next(iter(t.values()), "")
    return t or ""


def collect(cond: dict, elisp: list, files: list):
    if "elisp" in cond:
        cond["_elisp_index"] = len(elisp)
        elisp.append(cond["elisp"])
    if "file" in cond:
        p = cond["file"]["path"]
        if p not in files:
            files.append(p)
    for k in ("all", "any"):
        for c in cond.get(k) or []:
            collect(c, elisp, files)
    if "not" in cond:
        collect(cond["not"], elisp, files)


def chunks_of(demo: str) -> list[dict]:
    out = []
    for t in pc.parse_demo(demo):
        if t.kind == "key":
            out.append({"kind": "key", "key": t.key})
        elif t.kind == "text":
            out.append({"kind": "text", "text": t.text})
        else:
            out.append({"kind": "pause", "ms": t.pause_ms})
    return out


def file_pred(fc: dict, content) -> bool:
    if content is None:
        return fc.get("exists") is False
    if "exists" in fc:
        return bool(fc["exists"])
    if "contains" in fc:
        return fc["contains"] in content
    if "not_contains" in fc:
        return fc["not_contains"] not in content
    if "regex" in fc:
        import re

        return re.search(fc["regex"], content, re.M) is not None
    if "json_path" in fc:
        try:
            doc = json.loads(content)
        except ValueError:
            return False
        vals, found = pc.resolve(doc, fc["json_path"])
        return found and any(pc._equal(v, fc.get("equals")) for v in vals)
    return False


def evaluate(cond: dict, snap: dict, lesson_counts: dict) -> bool:
    if "event" in cond:
        ev = cond["event"]
        counts = snap.get("counts") or {}
        n = counts.get(ev["command"], 0)
        if ev.get("since") == "lesson":
            n += lesson_counts.get(ev["command"], 0)
        return n >= (ev.get("min") or 1)
    if "elisp" in cond:
        return snap["elisp"][cond["_elisp_index"]] is True
    if "file" in cond:
        return file_pred(cond["file"], (snap.get("files") or {}).get(cond["file"]["path"]))
    if "shell" in cond:
        return False
    if "layout" in cond:
        st = snap["state"]
        bufs = {w["id"]: w["buf"] for w in st.get("windows", [])}
        return pc.match_layout(pc.parse_layout_pattern(cond["layout"]), st.get("layout"), lambda i: bufs.get(i, ""))
    if "state" in cond:
        return pc.predicate_match(cond["state"], pc.state_doc(snap["state"]))
    if "all" in cond:
        return all(evaluate(c, snap, lesson_counts) for c in cond["all"])
    if "any" in cond:
        return any(evaluate(c, snap, lesson_counts) for c in cond["any"])
    if "not" in cond:
        return not evaluate(cond["not"], snap, lesson_counts)
    raise ValueError(f"condition without a kind: {cond}")


def run_lesson(lesson_id: str, keep: bool, verbose: bool) -> bool:
    ldir = CURRICULUM / lesson_id
    sc = load_yaml(ldir / "scenario.yaml")
    setup = dict(sc.get("setup") or {})
    flat = lesson_id.replace("/", "__")
    job = SCRATCH / flat
    if job.exists():
        shutil.rmtree(job)
    work = job / "work"
    work.mkdir(parents=True)
    root = setup.get("root") or ""
    if setup.get("workspace"):
        shutil.copytree(ldir / setup["workspace"], work / root, dirs_exist_ok=True)
    steps = []
    for st in sc["steps"]:
        elisp: list[str] = []
        files: list[str] = []
        for c in st["conditions"]:
            collect(c, elisp, files)
        steps.append({"id": st["id"], "chunks": chunks_of(st["demo"]), "elisp": elisp, "files": files})
    jobdoc = {"modules": sc.get("modules") or [], "setup": setup, "steps": steps}
    (job / "job.json").write_text(json.dumps(jobdoc), encoding="utf-8")
    for p in [job, *job.rglob("*")]:
        os.chmod(p, 0o777 if p.is_dir() else 0o666)
    cmd = [
        "docker", "run", "--rm", "-t",
        "--user", f"{os.getuid()}:{os.getgid()}",
        "-e", "HOME=/tmp/home", "-e", "TERM=xterm-256color", "-e", "COLORTERM=truecolor",
        "-v", f"{work}:/work", "-v", f"{job}:/job", "-v", f"{HERE}:/play:ro",
        IMAGE, "script", "-qc",
        "mkdir -p /tmp/home; cd /work; stty rows 36 cols 120; "
        "emacs -nw -Q -l /play/assumed-config.el -l /play/driver.el",
        "/dev/null",
    ]
    name = "pg-play-" + flat.replace("_", "-")[:60]
    cmd[3:3] = ["--name", name]
    try:
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=LESSON_TIMEOUT)
    except subprocess.TimeoutExpired:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    res_path = job / "result.json"
    if not res_path.exists():
        prog = job / "progress.log"
        last = prog.read_text(encoding="utf-8").strip().splitlines()[-3:] if prog.exists() else []
        print(f"FAIL {lesson_id}: no result (Emacs did not finish); last progress: {last}")
        return False
    res = json.loads(res_path.read_text(encoding="utf-8"))
    ok = True
    for e in res.get("errors") or []:
        print(f"FAIL {lesson_id}: driver error: {e}")
        ok = False
    lesson_counts: dict[str, int] = {}
    by_id = {s["id"]: s for s in sc["steps"]}
    for r in res["steps"]:
        st = by_id[r["id"]]
        snaps = r["snapshots"]
        done_at = None
        for i, snap in enumerate(snaps):
            if all(evaluate(c, snap, lesson_counts) for c in st["conditions"]):
                done_at = i
                break
        n = len(snaps)
        label = f"{lesson_id} step {r['id']}"
        if done_at is None:
            ok = False
            final = snaps[-1]
            print(f"FAIL {label}: never complete; final condition results:")
            for j, c in enumerate(st["conditions"]):
                print(f"       {j + 1}. {'ok ' if evaluate(c, final, lesson_counts) else 'NO '} {text_of(c.get('text'))}")
            s = final["state"]
            print(f"       state: buf={s.get('buf')!r} mode={s.get('mode')} line={s.get('line')} col={s.get('col')} "
                  f"layout={s.get('layout')} windows={[(w['id'], w['buf']) for w in s.get('windows', [])]} "
                  f"mini={s.get('minibuffer')}")
            print(f"       counts: {final.get('counts')}")
            print(f"       echo: {final.get('echo')!r}")
            if r.get("timedOut"):
                print(f"       (timed out with {r.get('pendingChunks')} chunks pending)")
        elif done_at == 0:
            ok = False
            print(f"FAIL {label}: already complete when the step starts")
        elif done_at < n - 2 and snaps[done_at]["why"] != "final":
            # n-1 is the final snapshot; n-2 the last command.
            rest = [s["why"] for s in snaps[done_at + 1:] if s["why"] != "final"]
            print(f"WARN {label}: completes at command {done_at}/{n - 2} ({snaps[done_at]['why']}); "
                  f"later commands spill into the next step: {rest}")
        else:
            if verbose:
                print(f"ok   {label} (at {snaps[done_at]['why']})")
        if verbose and done_at is not None:
            s = snaps[done_at]["state"]
            print(f"       state: buf={s.get('buf')!r} line={s.get('line')} layout={s.get('layout')}")
        for k, v in (snaps[-1].get("counts") or {}).items():
            lesson_counts[k] = lesson_counts.get(k, 0) + v
    if ok:
        print(f"PASS {lesson_id} ({len(res['steps'])} steps)")
    if not keep and ok:
        shutil.rmtree(job, ignore_errors=True)
    return ok


def all_lessons() -> list[str]:
    idx = load_yaml(CURRICULUM / "index.yaml")
    out = []
    for part in idx["parts"]:
        for l in part["lessons"]:
            if l.get("status") == "ready" and (CURRICULUM / l["id"] / "scenario.yaml").exists():
                out.append(l["id"])
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("lessons", nargs="*")
    ap.add_argument("--all", action="store_true")
    ap.add_argument("--keep", action="store_true", help="keep artifacts/play/<lesson> even on success")
    ap.add_argument("-v", "--verbose", action="store_true")
    ap.add_argument("-j", "--jobs", type=int, default=4)
    a = ap.parse_args()
    lessons = all_lessons() if a.all else a.lessons
    if not lessons:
        ap.print_usage()
        return 2
    from concurrent.futures import ThreadPoolExecutor

    with ThreadPoolExecutor(max_workers=a.jobs) as ex:
        results = list(ex.map(lambda l: run_lesson(l, a.keep, a.verbose), lessons))
    failed = [l for l, r in zip(lessons, results) if not r]
    print(f"\n{len(lessons) - len(failed)}/{len(lessons)} lessons pass")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
