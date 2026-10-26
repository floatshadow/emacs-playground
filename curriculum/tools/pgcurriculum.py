"""Python ports of the curriculum contracts in internal/protocol.

Used by curriculum/tools/validate.py and curriculum/tools/play/play.py. The
Go code is the reference; keep these in step with:

  keys.go       ParseKey, ParseKeys
  demo.go       ParseDemo
  layout.go     ParseLayoutPattern, MatchLayout, BufferBaseName
  predicate.go  ValidatePredicate, Predicate.Match, StateDoc, ExpandTemplate
  scenario.go   Scenario, Step, Condition, StuckRule field names

Only the standard library and PyYAML are needed.
"""

from __future__ import annotations

import json
import math
import posixpath
import re
from dataclasses import dataclass, field
from typing import Any, Callable

# ---------------------------------------------------------------------------
# Keys (keys.go)

NAMED_KEYS = {"RET", "TAB", "SPC", "ESC", "DEL", "LFD", "NUL"}
MOD_ORDER = ["A", "C", "H", "M", "S", "s"]


class KeyError_(ValueError):
    pass


def _is_function_alias(s: str) -> bool:
    if len(s) < 2 or len(s) > 3 or s[0] != "F" or not s[1:].isdigit():
        return False
    n = int(s[1:])
    return 1 <= n <= 35


def parse_key(tok: str) -> str:
    """Parse one key token; return its canonical Emacs notation."""
    if tok == "":
        raise KeyError_("empty key")
    mods: set[str] = set()
    rest = tok
    while len(rest) >= 3 and rest[1] == "-":
        m = rest[0]
        if m not in MOD_ORDER:
            raise KeyError_(f"key {tok!r}: unknown modifier {rest[:2]!r}")
        if m in mods:
            raise KeyError_(f"key {tok!r}: repeated modifier {rest[:2]!r}")
        mods.add(m)
        rest = rest[2:]
    func = False
    if len(rest) > 2 and rest[0] == "<" and rest[-1] == ">":
        name = rest[1:-1]
        for ch in name:
            if not (ch in "-_" or ch.isalnum()):
                raise KeyError_(f"key {tok!r}: bad function key name {name!r}")
        base, func = name, True
    elif rest in NAMED_KEYS:
        base = rest
    elif _is_function_alias(rest):
        base, func = "f" + rest[1:], True
    else:
        if len(rest) != 1 or rest.isspace():
            raise KeyError_(
                f"key {tok!r}: {rest!r} is not a single character, a named key or <function-key>"
            )
        base = rest
    prefix = "".join(m + "-" for m in MOD_ORDER if m in mods)
    return prefix + (f"<{base}>" if func else base)


def parse_keys(seq: str) -> list[str]:
    fields = seq.split()
    if not fields:
        raise KeyError_("empty key sequence")
    return [parse_key(f) for f in fields]


# ---------------------------------------------------------------------------
# Demo strings (demo.go)


@dataclass
class DemoToken:
    kind: str  # key | text | pause
    key: str = ""
    text: str = ""
    pause_ms: int = 0


class DemoError(ValueError):
    pass


def parse_demo(s: str) -> list[DemoToken]:
    out: list[DemoToken] = []
    i = 0
    n = len(s)
    while i < n:
        if s[i].isspace():
            i += 1
            continue
        if s[i] == '"':
            buf = []
            j = i + 1
            closed = False
            while j < n:
                c = s[j]
                if c == "\\" and j + 1 < n:
                    e = s[j + 1]
                    if e == '"':
                        buf.append('"')
                    elif e == "\\":
                        buf.append("\\")
                    elif e == "n":
                        buf.append("\n")
                    elif e == "t":
                        buf.append("\t")
                    else:
                        raise DemoError(f"demo: bad escape \\{e} at offset {j}")
                    j += 2
                    continue
                if c == '"':
                    closed = True
                    j += 1
                    break
                buf.append(c)
                j += 1
            if not closed:
                raise DemoError(f"demo: unterminated string at offset {i}")
            if j < n and not s[j].isspace():
                raise DemoError(f"demo: missing space after string at offset {j}")
            if not buf:
                raise DemoError(f"demo: empty string at offset {i}")
            out.append(DemoToken("text", text="".join(buf)))
            i = j
        else:
            j = i
            while j < n and not s[j].isspace():
                j += 1
            tok = s[i:j]
            if len(tok) > 1 and tok[0] == "~":
                try:
                    ms = int(tok[1:])
                except ValueError:
                    raise DemoError(f"demo: bad pause {tok!r} (want ~0 to ~60000)")
                if ms < 0 or ms > 60000:
                    raise DemoError(f"demo: bad pause {tok!r} (want ~0 to ~60000)")
                out.append(DemoToken("pause", pause_ms=ms))
            else:
                try:
                    out.append(DemoToken("key", key=parse_key(tok)))
                except KeyError_ as e:
                    raise DemoError(f"demo: {e}")
            i = j
    if not out:
        raise DemoError("demo: empty")
    return out


# ---------------------------------------------------------------------------
# Layout patterns (layout.go)


@dataclass
class LayoutPattern:
    split: str = ""  # "" for a leaf, else vsplit | hsplit
    name: str = ""
    single: bool = False
    children: list["LayoutPattern"] = field(default_factory=list)


class LayoutError(ValueError):
    pass


def _lex_layout(s: str) -> list[str]:
    toks: list[str] = []
    i = 0
    while i < len(s):
        c = s[i]
        if c in " \t\n\r":
            i += 1
        elif c in "()":
            toks.append(c)
            i += 1
        elif c == '"':
            j = i + 1
            buf = []
            while j < len(s) and s[j] != '"':
                if s[j] == "\\" and j + 1 < len(s):
                    j += 1
                buf.append(s[j])
                j += 1
            if j >= len(s):
                raise LayoutError("layout pattern: unterminated string")
            toks.append("\x00" + "".join(buf))
            i = j + 1
        else:
            j = i
            while j < len(s) and s[j] not in ' \t\n\r()"':
                j += 1
            toks.append(s[i:j])
            i = j
    return toks


def _parse_layout_toks(toks: list[str]) -> tuple[LayoutPattern, list[str]]:
    if not toks:
        raise LayoutError("layout pattern: unexpected end")
    t = toks[0]
    if t == ")":
        raise LayoutError("layout pattern: unexpected )")
    if t == "(":
        if len(toks) < 2:
            raise LayoutError("layout pattern: unexpected end after (")
        kind = toks[1]
        if kind not in ("vsplit", "hsplit"):
            raise LayoutError(
                f"layout pattern: want vsplit or hsplit after (, got {kind.lstrip(chr(0))!r}"
            )
        p = LayoutPattern(split=kind)
        rest = toks[2:]
        while True:
            if not rest:
                raise LayoutError("layout pattern: missing )")
            if rest[0] == ")":
                rest = rest[1:]
                break
            c, rest = _parse_layout_toks(rest)
            p.children.append(c)
        if len(p.children) < 2:
            raise LayoutError(f"layout pattern: {kind} needs at least two children")
        return p, rest
    if t == "single":
        raise LayoutError("layout pattern: single is only valid on its own")
    return LayoutPattern(name=t.lstrip("\x00") if t.startswith("\x00") else t), toks[1:]


def parse_layout_pattern(s: str) -> LayoutPattern:
    toks = _lex_layout(s)
    if not toks:
        raise LayoutError("layout pattern: empty")
    if len(toks) == 1 and toks[0] == "single":
        return LayoutPattern(single=True)
    p, rest = _parse_layout_toks(toks)
    if rest:
        raise LayoutError(f"layout pattern: trailing tokens {rest}")
    return p


def buffer_base_name(name: str) -> str:
    if name.endswith(">"):
        i = name.rfind("<")
        if i > 0:
            return name[:i]
    return name


# A Layout value is the JSON form: int (leaf), list ["vsplit", child, ...], or None.


def _flatten_layout(l: Any) -> Any:
    if not isinstance(l, list):
        return l
    out = [l[0]]
    for c in l[1:]:
        c = _flatten_layout(c)
        if isinstance(c, list) and c[0] == l[0]:
            out.extend(c[1:])
        else:
            out.append(c)
    if len(out) == 2:
        return out[1]
    return out


def _flatten_pattern(p: LayoutPattern) -> LayoutPattern:
    if not p.split:
        return p
    out = LayoutPattern(split=p.split)
    for c in p.children:
        c = _flatten_pattern(c)
        if c.split == p.split:
            out.children.extend(c.children)
        else:
            out.children.append(c)
    return out


def match_layout(p: LayoutPattern, layout: Any, buf_of: Callable[[int], str]) -> bool:
    layout = _flatten_layout(layout)
    p = _flatten_pattern(p)
    if p.single:
        return layout is not None and not isinstance(layout, list)
    return _match_layout(p, layout, buf_of)


def _match_layout(p: LayoutPattern, l: Any, buf_of: Callable[[int], str]) -> bool:
    if not p.split:
        if l is None or isinstance(l, list):
            return False
        if p.name == "*":
            return True
        want = p.name
        if "/" in want:
            want = posixpath.basename(want)
        return buffer_base_name(buf_of(l)) == want
    if not isinstance(l, list) or l[0] != p.split or len(l) - 1 != len(p.children):
        return False
    return all(_match_layout(pc, lc, buf_of) for pc, lc in zip(p.children, l[1:]))


# ---------------------------------------------------------------------------
# Predicates (predicate.go)

PREDICATE_OPS = {"in", "not", "gt", "gte", "lt", "lte", "exists", "contains", "regex"}


class PredicateError(ValueError):
    pass


def _is_num(v: Any) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def validate_expectation(want: Any) -> None:
    if not isinstance(want, dict):
        return
    if len(want) != 1:
        raise PredicateError(f"operator object must have exactly one key, has {len(want)}")
    (op, arg), = want.items()
    if op not in PREDICATE_OPS:
        raise PredicateError(f"unknown operator {op!r}")
    if op == "in" and not isinstance(arg, list):
        raise PredicateError("in: want a list")
    if op in ("gt", "gte", "lt", "lte") and not _is_num(arg):
        raise PredicateError(f"{op}: want a number")
    if op == "exists" and not isinstance(arg, bool):
        raise PredicateError("exists: want true or false")
    if op == "regex":
        if not isinstance(arg, str):
            raise PredicateError("regex: want a string")
        try:
            re.compile(arg)
        except re.error as e:
            raise PredicateError(f"regex: {e}")
    if op == "not":
        validate_expectation(arg)


def validate_predicate(p: dict) -> None:
    if not isinstance(p, dict):
        raise PredicateError("predicate: want a mapping of path to expectation")
    for path, want in p.items():
        if not path:
            raise PredicateError("predicate: empty path")
        try:
            validate_expectation(want)
        except PredicateError as e:
            raise PredicateError(f"predicate {path!r}: {e}")


def _step(v: Any, seg: str) -> list:
    if isinstance(v, dict):
        return [v[seg]] if seg in v else []
    if isinstance(v, list):
        if seg == "length":
            return [float(len(v))]
        if re.fullmatch(r"-?\d+", seg):
            i = int(seg)
            return [v[i]] if 0 <= i < len(v) else []
        out = []
        for e in v:
            out.extend(_step(e, seg))
        return out
    if isinstance(v, str) and seg == "length":
        return [float(len(v))]
    return []


def resolve(doc: Any, path: str) -> tuple[list, bool]:
    cur = [doc]
    for seg in path.split("."):
        nxt = []
        for v in cur:
            nxt.extend(_step(v, seg))
        if not nxt:
            return [], False
        cur = nxt
    return cur, True


def _equal(a: Any, b: Any) -> bool:
    if _is_num(a):
        return _is_num(b) and float(a) == float(b)
    if _is_num(b):
        return False
    return a == b


def _in_list(v: Any, l: list) -> bool:
    return any(_equal(v, e) for e in l)


def _match_one(v: Any, want: Any) -> bool:
    if isinstance(want, list):
        return _in_list(v, want)
    if isinstance(want, dict):
        if len(want) != 1:
            return False
        (op, arg), = want.items()
        if op == "in":
            return _in_list(v, arg if isinstance(arg, list) else [])
        if op == "not":
            return not _match_one(v, arg)
        if op in ("gt", "gte", "lt", "lte"):
            if not (_is_num(v) and _is_num(arg)):
                return False
            a, b = float(v), float(arg)
            return {"gt": a > b, "gte": a >= b, "lt": a < b, "lte": a <= b}[op]
        if op == "contains":
            if isinstance(v, str):
                return isinstance(arg, str) and arg in v
            if isinstance(v, list):
                return _in_list(arg, v)
            return False
        if op == "regex":
            return isinstance(v, str) and re.search(str(arg), v) is not None
        if op == "exists":
            return bool(arg)
        return False
    return _equal(v, want)


def _match_any(vals: list, found: bool, want: Any) -> bool:
    if isinstance(want, dict) and len(want) == 1:
        (op, arg), = want.items()
        if op == "exists":
            return found == bool(arg)
        if op == "not":
            return not _match_any(vals, found, arg)
    if not found:
        return want is None
    return any(_match_one(v, want) for v in vals)


def predicate_match(p: dict, doc: Any) -> bool:
    for path, want in p.items():
        vals, found = resolve(doc, path)
        if not _match_any(vals, found, want):
            return False
    return True


def state_doc(state: dict) -> dict:
    doc = json.loads(json.dumps(state))
    others = []
    for w in doc.get("windows") or []:
        if isinstance(w, dict) and w.get("selected") is True:
            doc["selectedWindow"] = w
            continue
        others.append(w)
    doc["otherWindows"] = others
    return doc


_TEMPLATE_RE = re.compile(r"\{([A-Za-z0-9_.]+)\}")


def template_paths(tmpl: str) -> list[str]:
    return _TEMPLATE_RE.findall(tmpl or "")


# ---------------------------------------------------------------------------
# State fields known to predicates (state.go State, plus StateDoc roots and
# the browser-side `session` root documented in tables/exits.yaml).

STATE_FIELDS = {
    "buf", "file", "mode", "modeName", "minors", "modified", "readonly", "narrowed",
    "overwrite", "line", "col", "point", "markActive", "windows", "layout", "frame",
    "frames", "tabs", "minibuffer", "isearch", "recursive", "macro", "prefixArg",
    "readOnlyReason", "diagAtPoint",
    # virtual roots
    "otherWindows", "selectedWindow", "session",
}
WINDOW_FIELDS = {"id", "buf", "mode", "selected", "dedicated", "side", "length"}
SUBFIELDS = {
    "minibuffer": {"depth", "prompt", "contents", "kind"},
    "tabs": {"count", "current"},
    "diagAtPoint": {"severity", "message", "source", "line", "col"},
    "windows": WINDOW_FIELDS,
    "otherWindows": WINDOW_FIELDS,
    "selectedWindow": WINDOW_FIELDS - {"length"},
    "minors": {"length"},
    "session": {"prefix", "emacs", "mode", "echo"},
    "buf": {"length"},
    "file": {"length"},
}


def check_state_path(path: str) -> str | None:
    """Return an error message when path does not name a known State field."""
    segs = path.split(".")
    root = segs[0]
    if root not in STATE_FIELDS:
        return f"unknown state field {root!r}"
    if len(segs) == 1:
        return None
    sub = SUBFIELDS.get(root)
    if sub is None:
        return f"state field {root!r} has no sub-fields"
    seg = segs[1]
    if re.fullmatch(r"\d+", seg) and root in ("windows", "otherWindows"):
        if len(segs) == 2:
            return None
        seg = segs[2]
        if seg not in WINDOW_FIELDS - {"length"}:
            return f"unknown window field {seg!r} in {path!r}"
        return None
    if seg not in sub:
        return f"unknown field {seg!r} under {root!r} in {path!r}"
    return None
