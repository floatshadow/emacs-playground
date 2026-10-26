#!/usr/bin/env python3
"""Validate the curriculum against CURRICULUM.md Part B (B.2 to B.5).

A stand-in for, and later a cross-check of, `playctl curriculum validate`.
It checks, without Emacs:

  index.yaml   15 parts in order; lesson ids are "<part dir>/<lesson dir>";
               every ready lesson has lesson.md and scenario.yaml; no
               lesson directory is missing from the index
  scenario     every field of protocol.Scenario (internal/protocol/
               scenario.go) and nothing else; modules are known and include
               base and agent; setup paths exist in the fixtures; layouts
               parse; Lisp forms are balanced; step ids are unique; every
               step has a demo that tokenises and 2 to 5 conditions, each
               with text and hint and exactly one kind; nested conditions
               are well formed; state paths name state fields; regexes
               compile; stuck rules use the known thresholds
  lesson.md    front matter (title, summary, minutes); a :::vscode box;
               known directives only; :try keys parse; :jargon terms are
               in the glossary; images exist under assets/
  tables       exits (predicates, state paths, templates), habits (single
               keys), gloss, cheats (key columns parse), glossary
  fixtures     lesson copies of shared trees match tools/fixture-src
               (sync_fixtures.py --check)

Usage:  python3 curriculum/tools/validate.py [CURRICULUM_DIR] [-q]
Exit status 1 when there are errors; warnings do not fail.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import pgcurriculum as pc  # noqa: E402

MODULES = [
    "base", "agent", "editing", "completion", "files", "windows", "git", "terminals",
    "lsp", "lang-rust", "lang-ocaml", "lang-python", "writing", "org", "power",
    "remote", "ui", "input",
]
SCENARIO_KEYS = {"id", "title", "minutes", "mode", "tags", "modules", "vscode", "preview",
                 "remote", "setup", "steps"}
SETUP_KEYS = {"workspace", "root", "reset", "open", "layout", "eval", "compile"}
STEP_KEYS = {"id", "goal", "detail", "demo", "conditions", "stuck", "checkpoint"}
KIND_KEYS = ["event", "elisp", "file", "shell", "layout", "state", "all", "any", "not"]
COND_KEYS = set(KIND_KEYS) | {"id", "text", "hint"}
EVENT_KEYS = {"command", "min", "since"}
FILE_KEYS = {"path", "exists", "contains", "not_contains", "regex", "json_path", "equals"}
FILE_PREDS = ["exists", "contains", "not_contains", "regex", "json_path"]
SHELL_KEYS = {"run", "cwd", "timeout"}
STUCK_KEYS = {"id", "when", "say", "title", "kind"}
STUCK_WHEN = {"minibuffer_seconds", "idle_seconds", "step_seconds", "quits_in_window",
              "unbound_repeats"}
TAGS = {"remote", "gui", "project"}
STEP_ID = re.compile(r"^[a-z0-9-]+$")
LOCALE = re.compile(r"^[a-z]{2}(-[A-Z][A-Za-z]{1,3})?$")
DIRECTIVES_TEXT = {"kbd", "try", "jargon"}
DIRECTIVES_CONTAINER = {"vscode", "aside", "gui-only"}
DIRECTIVES_LEAF = {"steps"}


class Report:
    def __init__(self) -> None:
        self.errors: list[str] = []
        self.warnings: list[str] = []

    def err(self, where: str, msg: str) -> None:
        self.errors.append(f"{where}: {msg}")

    def warn(self, where: str, msg: str) -> None:
        self.warnings.append(f"{where}: {msg}")


def load_yaml(p: Path):
    with open(p, encoding="utf-8") as f:
        return yaml.safe_load(f)


def is_localtext(v) -> bool:
    if isinstance(v, str):
        return v.strip() != ""
    if isinstance(v, dict) and v:
        return all(isinstance(k, str) and LOCALE.match(k) and isinstance(s, str) and s.strip()
                   for k, s in v.items())
    return False


def text_en(v) -> str:
    if isinstance(v, dict):
        return v.get("en") or ""
    return v or ""


# --- Lisp forms -----------------------------------------------------------


def lisp_balanced(src: str) -> str | None:
    """Return an error when SRC is not one balanced Lisp form (rough reader)."""
    depth = 0
    i = 0
    n = len(src)
    started = False
    closed_at_zero = False
    while i < n:
        c = src[i]
        if c == ";":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if c == '"':
            i += 1
            while i < n and src[i] != '"':
                if src[i] == "\\":
                    i += 1
                i += 1
            if i >= n:
                return "unterminated string"
            i += 1
            started = True
            continue
        if c == "?":
            # character literal: ?x ?\( ?\\
            i += 2 if i + 1 < n and src[i + 1] == "\\" else 1
            i += 1
            continue
        if c == "\\":
            i += 2
            continue
        if c in "([":
            if closed_at_zero:
                return "more than one form"
            depth += 1
            started = True
        elif c in ")]":
            depth -= 1
            if depth < 0:
                return "unbalanced )"
            if depth == 0:
                closed_at_zero = True
        elif not c.isspace() and depth == 0 and closed_at_zero:
            return "more than one form"
        i += 1
    if depth != 0:
        return f"{depth} unclosed ("
    if not started:
        return "empty form"
    return None


# --- Conditions -----------------------------------------------------------


def check_regex(where: str, rx: str, rep: Report) -> None:
    # RE2 is close to Python's re for the patterns used here; reject the
    # Python-only constructs RE2 does not have.
    if re.search(r"\(\?[=!<]|\\[1-9]", rx):
        rep.err(where, f"regex {rx!r} uses lookaround or backreferences, which RE2 does not support")
    try:
        re.compile(rx)
    except re.error as e:
        rep.err(where, f"regex {rx!r}: {e}")


def check_predicate(where: str, pred, rep: Report, allow_session: bool = False) -> None:
    if not isinstance(pred, dict) or not pred:
        rep.err(where, "predicate must be a non-empty mapping")
        return
    try:
        pc.validate_predicate(pred)
    except pc.PredicateError as e:
        rep.err(where, str(e))
    for path, want in pred.items():
        msg = pc.check_state_path(path)
        if msg:
            rep.err(where, msg)
        elif path.split(".")[0] == "session" and not allow_session:
            rep.err(where, f"{path!r}: the session root exists only in the browser (exits table)")
        w = want
        while isinstance(w, dict) and len(w) == 1 and "not" in w:
            w = w["not"]
        if isinstance(w, dict) and "regex" in w and isinstance(w["regex"], str):
            check_regex(where, w["regex"], rep)


def check_condition(where: str, c, rep: Report, top: bool, ids: set, commands: set) -> None:
    if not isinstance(c, dict):
        rep.err(where, "condition must be a mapping")
        return
    unknown = set(c) - COND_KEYS
    if unknown:
        rep.err(where, f"unknown condition keys {sorted(unknown)}")
    kinds = [k for k in KIND_KEYS if k in c]
    if len(kinds) != 1:
        rep.err(where, f"exactly one kind expected, found {kinds or 'none'}")
        return
    kind = kinds[0]
    if top:
        if not is_localtext(c.get("text")):
            rep.err(where, "text is required")
        if not is_localtext(c.get("hint")):
            rep.err(where, "hint is required (one per condition)")
        if "id" in c:
            if c["id"] in ids:
                rep.err(where, f"duplicate condition id {c['id']!r}")
            ids.add(c["id"])
    else:
        for k in ("text", "hint", "id"):
            if k in c:
                rep.warn(where, f"{k} on a nested condition is ignored")
    v = c[kind]
    if kind == "event":
        if not isinstance(v, dict) or set(v) - EVENT_KEYS or not isinstance(v.get("command"), str) or not v["command"]:
            rep.err(where, f"event wants {{command, min?, since?}}, got {v!r}")
            return
        commands.add(v["command"])
        if "min" in v and (not isinstance(v["min"], int) or isinstance(v["min"], bool) or v["min"] < 1):
            rep.err(where, "event.min must be a positive integer")
        if "since" in v and v["since"] not in ("step", "lesson"):
            rep.err(where, "event.since must be step or lesson")
    elif kind == "elisp":
        if not isinstance(v, str) or not v.strip():
            rep.err(where, "elisp must be a non-empty string")
            return
        e = lisp_balanced(v)
        if e:
            rep.err(where, f"elisp: {e}")
    elif kind == "file":
        if not isinstance(v, dict) or set(v) - FILE_KEYS:
            rep.err(where, f"file wants {sorted(FILE_KEYS)}, got {v!r}")
            return
        p = v.get("path")
        if not isinstance(p, str) or not p or p.startswith("/") or ".." in p.split("/"):
            rep.err(where, f"file.path must be workspace-relative, got {p!r}")
        preds = [k for k in FILE_PREDS if k in v]
        if len(preds) != 1:
            rep.err(where, f"file needs exactly one of {FILE_PREDS}, found {preds}")
        if "equals" in v and "json_path" not in v:
            rep.err(where, "file.equals needs json_path")
        if "exists" in v and not isinstance(v["exists"], bool):
            rep.err(where, "file.exists must be true or false")
        if "regex" in v:
            check_regex(where, v["regex"], rep)
    elif kind == "shell":
        if not isinstance(v, dict) or set(v) - SHELL_KEYS or not v.get("run"):
            rep.err(where, f"shell wants {{run, cwd?, timeout?}}, got {v!r}")
    elif kind == "layout":
        if not isinstance(v, str):
            rep.err(where, "layout must be a string")
            return
        try:
            pc.parse_layout_pattern(v)
        except pc.LayoutError as e:
            rep.err(where, str(e))
    elif kind == "state":
        check_predicate(where, v, rep)
    elif kind in ("all", "any"):
        if not isinstance(v, list) or len(v) < 1:
            rep.err(where, f"{kind} wants a non-empty list")
            return
        for i, sub in enumerate(v):
            check_condition(f"{where}.{kind}[{i}]", sub, rep, False, ids, commands)
    elif kind == "not":
        check_condition(f"{where}.not", v, rep, False, ids, commands)


# --- Scenario -------------------------------------------------------------


def check_scenario(lesson_id: str, ldir: Path, rep: Report, commands: set) -> dict | None:
    where = f"{lesson_id}/scenario.yaml"
    try:
        sc = load_yaml(ldir / "scenario.yaml")
    except yaml.YAMLError as e:
        rep.err(where, f"YAML: {e}")
        return None
    if not isinstance(sc, dict):
        rep.err(where, "not a mapping")
        return None
    unknown = set(sc) - SCENARIO_KEYS
    if unknown:
        rep.err(where, f"unknown keys {sorted(unknown)}")
    if sc.get("id") != lesson_id:
        rep.err(where, f"id {sc.get('id')!r} must be the lesson id {lesson_id!r}")
    if not is_localtext(sc.get("title")):
        rep.err(where, "title is required")
    m = sc.get("minutes")
    if not isinstance(m, int) or isinstance(m, bool) or m <= 0:
        rep.err(where, "minutes must be a positive integer")
    if sc.get("mode", "terminal") not in ("terminal", "gui", "any"):
        rep.err(where, "mode must be terminal, gui or any")
    tags = sc.get("tags") or []
    if not isinstance(tags, list) or set(tags) - TAGS:
        rep.err(where, f"tags must be a subset of {sorted(TAGS)}")
    mods = sc.get("modules")
    if not isinstance(mods, list) or not mods:
        rep.err(where, "modules must be a non-empty list")
    else:
        for x in mods:
            if x not in MODULES:
                rep.err(where, f"unknown module {x!r} (emacs.md §3)")
        if mods[:2] != ["base", "agent"]:
            rep.err(where, "modules must start with base, agent")
        if len(set(mods)) != len(mods):
            rep.err(where, "modules has duplicates")
    if "vscode" in sc and not isinstance(sc["vscode"], str):
        rep.err(where, "vscode must be a string")
    if "preview" in sc:
        pv = sc["preview"]
        if not isinstance(pv, dict) or set(pv) - {"tabs", "follow"}:
            rep.err(where, "preview wants {tabs, follow}")
    if "remote" in sc:
        rv = sc["remote"]
        if not isinstance(rv, dict) or set(rv) != {"box"} or not isinstance(rv["box"], bool):
            rep.err(where, "remote wants {box: bool}")

    setup = sc.get("setup") or {}
    if not isinstance(setup, dict):
        rep.err(where, "setup must be a mapping")
        setup = {}
    unknown = set(setup) - SETUP_KEYS
    if unknown:
        rep.err(where, f"unknown setup keys {sorted(unknown)}")
    ws = setup.get("workspace")
    root = setup.get("root") or ""
    wsdir = None
    if ws:
        wsdir = (ldir / ws).resolve()
        if not str(wsdir).startswith(str(ldir.resolve())) or not wsdir.is_dir():
            rep.err(where, f"setup.workspace {ws!r} is not a directory inside the lesson")
            wsdir = None
        if not root or "/" in root:
            rep.err(where, "setup.root must be one directory name when workspace is set")
    for f in setup.get("open") or []:
        if not isinstance(f, str) or f.startswith("/"):
            rep.err(where, f"setup.open entries are workspace-relative paths, got {f!r}")
            continue
        if wsdir is not None:
            if not f.startswith(root + "/"):
                rep.warn(where, f"setup.open {f!r} is outside the fixture root {root!r}")
            elif not (wsdir / f[len(root) + 1:]).is_file():
                rep.err(where, f"setup.open {f!r} is not in the fixtures")
    lay = setup.get("layout")
    if lay:
        if lay.endswith(".eld"):
            if not (ldir / lay).is_file():
                rep.err(where, f"setup.layout file {lay!r} does not exist")
        else:
            try:
                pat = pc.parse_layout_pattern(lay)
            except pc.LayoutError as e:
                rep.err(where, f"setup.layout: {e}")
            else:
                def leaves(p):
                    if p.split:
                        for ch in p.children:
                            yield from leaves(ch)
                    elif not p.single:
                        yield p.name
                for name in leaves(pat):
                    if "/" in name and wsdir is not None and name.startswith(root + "/") \
                            and not (wsdir / name[len(root) + 1:]).is_file():
                        rep.err(where, f"setup.layout names {name!r}, which is not in the fixtures")
    for k in ("eval",):
        if setup.get(k):
            e = lisp_balanced(setup[k])
            if e:
                rep.err(where, f"setup.{k}: {e}")
    if "reset" in setup and not isinstance(setup["reset"], bool):
        rep.err(where, "setup.reset must be a boolean")

    steps = sc.get("steps")
    if not isinstance(steps, list) or not steps:
        rep.err(where, "steps must be a non-empty list")
        return sc
    seen = set()
    for si, st in enumerate(steps):
        sw = f"{where} step {si + 1}"
        if not isinstance(st, dict):
            rep.err(sw, "step must be a mapping")
            continue
        sid = st.get("id")
        sw = f"{where} step {sid}"
        unknown = set(st) - STEP_KEYS
        if unknown:
            rep.err(sw, f"unknown step keys {sorted(unknown)}")
        if not isinstance(sid, str) or not STEP_ID.match(sid):
            rep.err(sw, "step id must match [a-z0-9-]+")
        elif sid in seen:
            rep.err(sw, "duplicate step id")
        seen.add(sid)
        if not is_localtext(st.get("goal")):
            rep.err(sw, "goal is required")
        if "detail" in st and not is_localtext(st["detail"]):
            rep.err(sw, "detail must be text")
        demo = st.get("demo")
        if not isinstance(demo, str) or not demo.strip():
            rep.err(sw, "demo is required (ADR-017)")
        else:
            try:
                pc.parse_demo(demo)
            except pc.DemoError as e:
                rep.err(sw, str(e))
        conds = st.get("conditions")
        if not isinstance(conds, list) or not conds:
            rep.err(sw, "conditions must be a non-empty list")
            conds = []
        elif not 2 <= len(conds) <= 5:
            rep.err(sw, f"{len(conds)} conditions; write 2 to 5 ordered conditions per step")
        ids: set = set()
        for ci, c in enumerate(conds):
            check_condition(f"{sw} condition {ci + 1}", c, rep, True, ids, commands)
        for ri, r in enumerate(st.get("stuck") or []):
            rw = f"{sw} stuck {ri + 1}"
            if not isinstance(r, dict) or set(r) - STUCK_KEYS:
                rep.err(rw, f"stuck rule wants {sorted(STUCK_KEYS)}")
                continue
            w = r.get("when")
            if not isinstance(w, dict) or not w or set(w) - STUCK_WHEN:
                rep.err(rw, f"when wants some of {sorted(STUCK_WHEN)}")
            else:
                for k, val in w.items():
                    if k == "quits_in_window":
                        if not isinstance(val, dict) or set(val) != {"count", "seconds"}:
                            rep.err(rw, "quits_in_window wants {count, seconds}")
                    elif not isinstance(val, int) or isinstance(val, bool) or val <= 0:
                        rep.err(rw, f"{k} must be a positive integer")
            if not is_localtext(r.get("say")):
                rep.err(rw, "say is required")
            if r.get("kind", "calm") not in ("calm", "ask"):
                rep.err(rw, "kind must be calm or ask")
        if "checkpoint" in st and not isinstance(st["checkpoint"], bool):
            rep.err(sw, "checkpoint must be a boolean")
    return sc


# --- lesson.md ------------------------------------------------------------

FRONT = re.compile(r"\A---\n(.*?)\n---\n", re.S)
TEXT_DIRECTIVE = re.compile(r"(?<![:\w]):([a-z][a-z-]*)\[([^\]]*)\]")
CONTAINER_OPEN = re.compile(r"^(:{3,})([a-z][a-z-]*)(\{[^}]*\})?\s*$")
CONTAINER_CLOSE = re.compile(r"^(:{3,})\s*$")
LEAF = re.compile(r"^::([a-z][a-z-]*)(\{[^}]*\})?\s*$")
IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")


def glossary_has(term: str, glossary: dict) -> bool:
    keys = {k.lower() for k in glossary}
    t = term.lower().strip()
    return t in keys or (t.endswith("s") and t[:-1] in keys) or (t.endswith("es") and t[:-2] in keys)


def check_lesson_md(lesson_id: str, ldir: Path, sc: dict | None, glossary: dict, rep: Report) -> None:
    where = f"{lesson_id}/lesson.md"
    p = ldir / "lesson.md"
    src = p.read_text(encoding="utf-8")
    m = FRONT.match(src)
    if not m:
        rep.err(where, "front matter (--- title, summary, minutes ---) is missing")
        body = src
    else:
        try:
            fm = yaml.safe_load(m.group(1)) or {}
        except yaml.YAMLError as e:
            rep.err(where, f"front matter: {e}")
            fm = {}
        for k in ("title", "summary", "minutes"):
            if k not in fm:
                rep.err(where, f"front matter lacks {k}")
        if sc and fm.get("title") and fm["title"] != text_en(sc.get("title")):
            rep.warn(where, f"title {fm['title']!r} differs from the scenario's {text_en(sc.get('title'))!r}")
        if sc and fm.get("minutes") and fm["minutes"] != sc.get("minutes"):
            rep.warn(where, "minutes differ from the scenario's")
        body = src[m.end():]
    stack: list[tuple[str, int]] = []
    saw_vscode = False
    in_code = False
    for ln, line in enumerate(body.splitlines(), 1):
        if line.startswith("```"):
            in_code = not in_code
            continue
        if in_code:
            continue
        mo = CONTAINER_OPEN.match(line)
        if mo:
            name = mo.group(2)
            if name not in DIRECTIVES_CONTAINER:
                rep.err(where, f"line {ln}: unknown container directive :::{name}")
            if name == "vscode":
                saw_vscode = True
            stack.append((name, len(mo.group(1))))
            continue
        if CONTAINER_CLOSE.match(line):
            if not stack:
                rep.err(where, f"line {ln}: ::: closes nothing")
            else:
                stack.pop()
            continue
        mo = LEAF.match(line)
        if mo:
            if mo.group(1) not in DIRECTIVES_LEAF:
                rep.err(where, f"line {ln}: unknown leaf directive ::{mo.group(1)}")
            continue
        stripped = re.sub(r"`[^`]*`", "", line)
        for d in TEXT_DIRECTIVE.finditer(stripped):
            name, arg = d.group(1), d.group(2)
            if name not in DIRECTIVES_TEXT:
                rep.err(where, f"line {ln}: unknown directive :{name}[...]")
            elif name in ("kbd", "try"):
                try:
                    pc.parse_keys(arg)
                except pc.KeyError_ as e:
                    rep.err(where, f"line {ln}: :{name}[{arg}]: {e}")
            elif name == "jargon" and not glossary_has(arg, glossary):
                rep.err(where, f"line {ln}: :jargon[{arg}] is not in tables/glossary.yaml")
        for img in IMAGE.finditer(line):
            path = img.group(1)
            if not path.startswith("assets/") or not (ldir / path).is_file():
                rep.err(where, f"line {ln}: image {path!r} must exist under assets/")
    if stack:
        rep.err(where, f"unclosed container :::{stack[-1][0]}")
    if not saw_vscode:
        rep.err(where, "the :::vscode box (Coming from VS Code) is missing")
    if ":try[" not in body:
        rep.warn(where, "no :try[...] keys")


# --- Tables ---------------------------------------------------------------


def check_cheat_keys(where: str, keys: str, rep: Report) -> None:
    for alt in keys.split(" / "):
        toks = alt.split()
        i = 0
        while i < len(toks):
            t = toks[i]
            if t == "M-x" and i + 1 < len(toks):
                i += 2
                continue
            try:
                pc.parse_key(t)
            except pc.KeyError_ as e:
                rep.err(where, f"{keys!r}: {e}")
                break
            i += 1


def check_tables(tdir: Path, rep: Report) -> dict:
    out = {}
    for name in ("exits", "habits", "gloss", "cheats", "glossary"):
        p = tdir / f"{name}.yaml"
        if not p.is_file():
            rep.err("tables", f"{name}.yaml is missing")
            out[name] = None
            continue
        try:
            out[name] = load_yaml(p)
        except yaml.YAMLError as e:
            rep.err(f"tables/{name}.yaml", f"YAML: {e}")
            out[name] = None
    exits = out.get("exits")
    if not isinstance(exits, list) or not exits:
        rep.err("tables/exits.yaml", "must be a non-empty list")
    else:
        ids = set()
        for i, row in enumerate(exits):
            w = f"tables/exits.yaml row {i + 1}"
            if not isinstance(row, dict) or set(row) - {"id", "when", "why", "how", "flag"}:
                rep.err(w, "row wants {id, when, why, how, flag?}")
                continue
            rid = row.get("id")
            w = f"tables/exits.yaml {rid}"
            if not rid or rid in ids:
                rep.err(w, "id missing or duplicate")
            ids.add(rid)
            check_predicate(w, row.get("when"), rep, allow_session=True)
            for k in ("why", "how"):
                v = row.get(k)
                if not isinstance(v, str) or not v.strip():
                    rep.err(w, f"{k} must be a non-empty string")
                    continue
                for path in pc.template_paths(v):
                    msg = pc.check_state_path(path)
                    if msg:
                        rep.err(w, f"{k} template: {msg}")
    habits = out.get("habits")
    if not isinstance(habits, list) or not habits:
        rep.err("tables/habits.yaml", "must be a non-empty list")
    else:
        keys = set()
        for i, row in enumerate(habits):
            w = f"tables/habits.yaml row {i + 1}"
            if not isinstance(row, dict) or set(row) != {"key", "vscode", "say"}:
                rep.err(w, "row wants exactly {key, vscode, say}")
                continue
            try:
                ks = pc.parse_keys(row["key"])
                if len(ks) != 1:
                    rep.err(w, "key must be a single key")
            except pc.KeyError_ as e:
                rep.err(w, str(e))
            if row["key"] in keys:
                rep.err(w, f"duplicate key {row['key']}")
            keys.add(row["key"])
    gloss = out.get("gloss")
    if not isinstance(gloss, dict) or not all(isinstance(k, str) and isinstance(v, str) and v for k, v in gloss.items()):
        rep.err("tables/gloss.yaml", "must map command names to non-empty strings")
        out["gloss"] = {}
    cheats = out.get("cheats")
    if not isinstance(cheats, dict) or "common" not in cheats:
        rep.err("tables/cheats.yaml", "must be a mapping with a common entry")
    else:
        for mode, rows in cheats.items():
            if not isinstance(rows, list):
                rep.err(f"tables/cheats.yaml {mode}", "must be a list of [keys, description]")
                continue
            for row in rows:
                if not (isinstance(row, list) and len(row) == 2 and all(isinstance(x, str) and x for x in row)):
                    rep.err(f"tables/cheats.yaml {mode}", f"bad row {row!r}")
                    continue
                check_cheat_keys(f"tables/cheats.yaml {mode}", row[0], rep)
    glossary = out.get("glossary")
    if not isinstance(glossary, dict) or not all(isinstance(v, str) and v for v in glossary.values()):
        rep.err("tables/glossary.yaml", "must map terms to non-empty strings")
        out["glossary"] = {}
    return out


# --- Index ----------------------------------------------------------------


def check_index(cur: Path, rep: Report) -> list[str]:
    idx = load_yaml(cur / "index.yaml")
    ready: list[str] = []
    parts = idx.get("parts") or []
    if [p.get("n") for p in parts] != list(range(15)):
        rep.err("index.yaml", "parts must be numbered 0 to 14 in order")
    listed = set()
    for p in parts:
        pid = p.get("id")
        for k in ("title", "minutes", "outcome", "status"):
            if k not in p:
                rep.err(f"index.yaml part {pid}", f"{k} is missing")
        for mod in p.get("modules") or []:
            if mod not in MODULES:
                rep.err(f"index.yaml part {pid}", f"unknown module {mod!r}")
        statuses = set()
        for les in p.get("lessons") or []:
            lid = les.get("id", "")
            w = f"index.yaml {lid}"
            if lid.count("/") != 1:
                rep.err(w, "lesson id must be <part dir>/<lesson dir>")
            elif not (lid.startswith(pid + "/") or lid.startswith("projects/")):
                rep.err(w, f"lesson id must start with the part id {pid!r}")
            if lid in listed:
                rep.err(w, "listed twice")
            listed.add(lid)
            st = les.get("status")
            statuses.add(st)
            ldir = cur / lid
            if st == "ready":
                for f in ("lesson.md", "scenario.yaml"):
                    if not (ldir / f).is_file():
                        rep.err(w, f"ready lesson lacks {f}")
                ready.append(lid)
            elif st == "planned":
                if ldir.exists():
                    rep.warn(w, "planned lesson already has a directory")
            else:
                rep.err(w, "status must be ready or planned")
        want = "ready" if statuses == {"ready"} else "planned"
        if p.get("status") != want:
            rep.err(f"index.yaml part {pid}", f"status should be {want}")
    for sc in sorted(cur.glob("*/*/scenario.yaml")):
        lid = str(sc.parent.relative_to(cur))
        if lid.startswith("tools/"):
            continue
        if lid not in listed:
            rep.err(lid, "lesson directory is not listed in index.yaml")
    return ready


def main() -> int:
    args = [a for a in sys.argv[1:] if not a.startswith("-")]
    quiet = "-q" in sys.argv[1:]
    cur = Path(args[0]).resolve() if args else HERE.parent
    rep = Report()
    tables = check_tables(cur / "tables", rep)
    ready = check_index(cur, rep)
    commands: set = set()
    for lid in ready:
        ldir = cur / lid
        if not (ldir / "scenario.yaml").is_file():
            continue
        sc = check_scenario(lid, ldir, rep, commands)
        if (ldir / "lesson.md").is_file():
            check_lesson_md(lid, ldir, sc, tables.get("glossary") or {}, rep)
    gloss = tables.get("gloss") or {}
    for c in sorted(commands - set(gloss)):
        rep.warn("tables/gloss.yaml", f"no gloss for {c}, which a condition checks")
    sys.path.insert(0, str(HERE))
    import sync_fixtures  # noqa: E402

    for d in sync_fixtures.drift():
        rep.err("fixtures", d)
    if not quiet:
        for w in rep.warnings:
            print(f"warning: {w}")
    for e in rep.errors:
        print(f"error: {e}")
    print(f"{len(ready)} ready lessons, {len(gloss)} glosses, {len(rep.errors)} errors, {len(rep.warnings)} warnings")
    return 1 if rep.errors else 0


if __name__ == "__main__":
    sys.exit(main())
