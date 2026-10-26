/* mock-emacs.js — a deliberately small fake Emacs for the design demo.
 *
 * It is NOT Emacs. It simulates a few dozen commands, the minibuffer,
 * windows, a handful of "oops" states (narrowing, overwrite, macros,
 * closed frame) so that the key ticker, the "where am I" panel and the
 * rescue flow can be felt before the real system exists.
 *
 * Runs in the browser (DOM renderer) and in node (headless, for tests).
 */
(function (global) {
'use strict';

/* ------------------------------------------------------------------ */
/* Fixture files                                                       */
/* ------------------------------------------------------------------ */

const RUST_HELLO = `use std::env;

fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    println!("{}", greet(&name));
}
`;

const HELLO_FILES = {
  'main.rs':    { mode: 'rust-ts-mode', modeName: 'Rust', minors: 'Eglot', dir: '/work/hello/src/', text: RUST_HELLO },
  'lib.rs':     { mode: 'rust-ts-mode', modeName: 'Rust', minors: 'Eglot', dir: '/work/hello/src/',
                  text: 'pub fn shout(s: &str) -> String {\n    s.to_uppercase()\n}\n' },
  'Cargo.toml': { mode: 'conf-toml-mode', modeName: 'Conf[TOML]', dir: '/work/hello/',
                  text: '[package]\nname = "hello"\nversion = "0.1.0"\nedition = "2024"\n\n[dependencies]\n' },
  'README.md':  { mode: 'markdown-mode', modeName: 'Markdown', dir: '/work/hello/',
                  text: '# hello\n\nA tiny Rust program used by lesson 2.3.\n\n- `cargo run` prints a greeting\n- `cargo run -- Ada` greets Ada\n' },
  '*scratch*':  { mode: 'lisp-interaction-mode', modeName: 'Lisp Interaction',
                  text: ';; This buffer is for text that is not saved, and for Lisp evaluation.\n;; To create a file, visit it with C-x C-f and enter text in its buffer.\n\n' },
  '*Messages*': { mode: 'messages-buffer-mode', modeName: 'Messages', special: true, readonly: true,
                  text: 'Loading /home/learner/.config/emacs/init.el (source)...done\nFor information about GNU Emacs and the GNU system, type C-h C-a.\n' },
};

const RUST_POLY = `use polyglot::greet;
use std::env;

/// Entry point for the \`polyglot\` service CLI.
fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    // Rust borrows: greet() wants &str, we hold a String.
    //   fix: pass a reference ->  greet(&name)
    println!("{}", greet(name));
}
`;

const COMPILE_FAIL = `-*- mode: compilation; default-directory: "/work/polyglot/" -*-
Compilation started at Mon Oct 26 18:02:11

cargo build
   Compiling polyglot v0.1.0 (/work/polyglot)
error[E0308]: mismatched types
 --> src/main.rs:9:26
  |
9 |     println!("{}", greet(name));
  |                    ----- ^^^^ expected \`&str\`, found \`String\`
  |                    |
  |                    arguments to this function are incorrect
  |
help: consider borrowing here
  |
9 |     println!("{}", greet(&name));
  |                          +

error: could not compile \`polyglot\` (bin "polyglot") due to 1 previous error

Compilation exited abnormally with code 101 at Mon Oct 26 18:02:12
`;

const COMPILE_OK = `-*- mode: compilation; default-directory: "/work/polyglot/" -*-
Compilation started at Mon Oct 26 18:04:40

cargo build
   Compiling polyglot v0.1.0 (/work/polyglot)
    Finished \`dev\` profile [unoptimized + debuginfo] target(s) in 0.61s

Compilation finished at Mon Oct 26 18:04:41
`;

const POLY_FILES = {
  'main.rs':   { mode: 'rust-ts-mode', modeName: 'Rust', minors: 'Eglot Flymake', dir: '/work/polyglot/src/', text: RUST_POLY,
                 diags: [{ line: 8, col: 25, len: 4, msg: 'error: expected `&str`, found `String` [E0308]' }] },
  'lib.rs':    { mode: 'rust-ts-mode', modeName: 'Rust', minors: 'Eglot Flymake', dir: '/work/polyglot/src/',
                 text: 'pub fn greet(name: &str) -> String {\n    format!("Hello, {name}!")\n}\n' },
  'report.typ':{ mode: 'typst-ts-mode', modeName: 'Typst', minors: 'Eglot', dir: '/work/polyglot/docs/',
                 text: '#set page(paper: "a5", margin: 1.6cm)\n#set text(font: "Libertinus Serif", 10.5pt)\n\n= Polyglot: a tiny service, measured\n\nWe greet in Rust, score in Python and\nprove termination in OCaml.\n\n== Latency\n\n#table(\n  columns: 3,\n  [lang], [p50], [p99],\n  [Rust], [0.4 ms], [1.1 ms],\n  [Python], [3.2 ms], [9.8 ms],\n)\n\n$ E = sum_(i=1)^n x_i / n $\n' },
  'score.py':  { mode: 'python-ts-mode', modeName: 'Python', minors: 'Eglot', dir: '/work/polyglot/tools/',
                 text: 'import json, sys\n\ndef score(rows: list[dict]) -> float:\n    return sum(r["p99"] for r in rows) / len(rows)\n\nif __name__ == "__main__":\n    print(score(json.load(sys.stdin)))\n' },
  'term.ml':   { mode: 'tuareg-mode', modeName: 'Tuareg', minors: 'Eglot', dir: '/work/polyglot/proof/',
                 text: 'let rec fib n = if n < 2 then n else fib (n - 1) + fib (n - 2)\n\nlet () = Printf.printf "%d\\n" (fib 10)\n' },
  '*compilation*': { mode: 'compilation-mode', modeName: 'Compilation', special: true, readonly: true, text: COMPILE_FAIL },
  '*scratch*':  HELLO_FILES['*scratch*'],
  '*Messages*': HELLO_FILES['*Messages*'],
};

/* ------------------------------------------------------------------ */
/* Key bindings and commands                                           */
/* ------------------------------------------------------------------ */

const BINDINGS = {
  'C-x C-f': 'find-file',
  'C-x C-s': 'save-buffer',
  'C-x C-b': 'list-buffers',
  'C-x b':   'switch-to-buffer',
  'C-x k':   'kill-buffer',
  'C-x 0':   'delete-window',
  'C-x 1':   'delete-other-windows',
  'C-x 2':   'split-window-below',
  'C-x 3':   'split-window-right',
  'C-x o':   'other-window',
  'C-x C-c': 'save-buffers-kill-terminal',
  'C-x C-q': 'read-only-mode',
  'C-x n n': 'narrow-to-region',
  'C-x n d': 'narrow-to-defun',
  'C-x n w': 'widen',
  'C-x (':   'kmacro-start-macro',
  'C-x )':   'kmacro-end-macro',
  '<f3>':    'kmacro-start-macro',
  '<f4>':    'kmacro-end-or-call-macro',
  'C-x u':   'undo',
  'C-/':     'undo',
  'C-_':     'undo',
  'C-x C-e': 'eval-last-sexp',
  'C-x h':   'mark-whole-buffer',
  'C-g':     'keyboard-quit',
  'ESC ESC ESC': 'keyboard-escape-quit',
  'C-s':     'isearch-forward',
  'C-r':     'isearch-backward',
  'M-x':     'execute-extended-command',
  'M-%':     'query-replace',
  'C-h k':   'describe-key',
  'C-h f':   'describe-function',
  'C-h m':   'describe-mode',
  'C-h t':   'help-with-tutorial',
  'C-h .':   'display-local-help',
  'C-z':     'suspend-frame',
  'C-x C-z': 'suspend-frame',
  'C-a':     'move-beginning-of-line',
  'C-e':     'move-end-of-line',
  'C-f':     'forward-char',
  'C-b':     'backward-char',
  'C-n':     'next-line',
  'C-p':     'previous-line',
  '<left>':  'backward-char',
  '<right>': 'forward-char',
  '<up>':    'previous-line',
  '<down>':  'next-line',
  '<home>':  'move-beginning-of-line',
  '<end>':   'move-end-of-line',
  'M-f':     'forward-word',
  'M-b':     'backward-word',
  'M-<':     'beginning-of-buffer',
  'M->':     'end-of-buffer',
  'C-v':     'scroll-up-command',
  'M-v':     'scroll-down-command',
  'C-l':     'recenter-top-bottom',
  'C-k':     'kill-line',
  'C-y':     'yank',
  'C-w':     'kill-region',
  'M-w':     'kill-ring-save',
  'C-SPC':   'set-mark-command',
  'C-d':     'delete-char',
  'DEL':     'delete-backward-char',
  'M-d':     'kill-word',
  'RET':     'newline',
  'TAB':     'indent-for-tab-command',
  '<insert>':'overwrite-mode',
  'C-u':     'universal-argument',
  'M-g M-n': 'next-error',
  'M-g g':   'goto-line',
  'M-.':     'xref-find-definitions',
  'M-,':     'xref-go-back',
  'C-c C-c': 'compile',
  'M-;':     'comment-dwim',
  'C-x C-;': 'comment-line',
};

/* Bindings that only exist in special buffers (local keymaps). */
const LOCAL = {
  'Buffer-menu-mode': { 'q': 'quit-window', 'RET': 'Buffer-menu-this-window', 'n': 'next-line', 'p': 'previous-line' },
  'help-mode':        { 'q': 'quit-window', 'n': 'next-line', 'p': 'previous-line' },
  'dired-mode':       { 'q': 'quit-window', 'RET': 'dired-find-file', 'n': 'next-line', 'p': 'previous-line' },
  'compilation-mode': { 'q': 'quit-window', 'RET': 'compile-goto-error', 'n': 'next-error-no-select', 'g': 'recompile' },
  'messages-buffer-mode': { 'q': 'quit-window' },
};

const GLOSS = {
  'find-file': 'Open a file (asks for a path)',
  'save-buffer': 'Save this buffer to its file',
  'list-buffers': 'List all open buffers in a second window',
  'switch-to-buffer': 'Switch to another open buffer',
  'kill-buffer': 'Close a buffer (not a window)',
  'delete-window': 'Close this window; the buffer stays open',
  'delete-other-windows': 'Keep only this window',
  'split-window-below': 'Split: one window above the other',
  'split-window-right': 'Split: two windows side by side',
  'other-window': 'Move the cursor to the next window',
  'save-buffers-kill-terminal': 'Quit Emacs (here: closes the frame, we reopen it)',
  'read-only-mode': 'Toggle read-only for this buffer',
  'narrow-to-region': 'Show only the selected region (hides the rest!)',
  'narrow-to-defun': 'Show only the current function (hides the rest!)',
  'widen': 'Show the whole buffer again',
  'kmacro-start-macro': 'Start recording a keyboard macro',
  'kmacro-end-macro': 'Stop recording the macro',
  'kmacro-end-or-call-macro': 'Stop recording, or replay the macro',
  'undo': 'Undo the last change',
  'eval-last-sexp': 'Evaluate the Lisp expression before the cursor',
  'mark-whole-buffer': 'Select everything',
  'keyboard-quit': 'Cancel whatever is in progress',
  'keyboard-escape-quit': 'Get out of everything: prompt, extra windows, recursive edits',
  'isearch-forward': 'Search forward as you type',
  'isearch-backward': 'Search backward as you type',
  'isearch-repeat-forward': 'Jump to the next match',
  'isearch-exit': 'Stop searching and stay here',
  'execute-extended-command': 'Run any command by name',
  'query-replace': 'Replace text, asking at each match',
  'describe-key': 'Explain what a key does',
  'describe-function': 'Explain a command by name',
  'describe-mode': 'Explain the current major and minor modes',
  'help-with-tutorial': 'Open the built-in tutorial',
  'display-local-help': 'Show the diagnostic under the cursor',
  'suspend-frame': 'Suspend Emacs to the shell (the playground resumes it)',
  'move-beginning-of-line': 'Go to the start of the line',
  'move-end-of-line': 'Go to the end of the line',
  'forward-char': 'Move right one character',
  'backward-char': 'Move left one character',
  'next-line': 'Move down one line',
  'previous-line': 'Move up one line',
  'forward-word': 'Move right one word',
  'backward-word': 'Move left one word',
  'beginning-of-buffer': 'Jump to the top',
  'end-of-buffer': 'Jump to the bottom',
  'scroll-up-command': 'Scroll down a page',
  'scroll-down-command': 'Scroll up a page',
  'recenter-top-bottom': 'Scroll so the cursor line is centred (then top, then bottom)',
  'kill-line': 'Cut to the end of the line',
  'yank': 'Paste the last cut text',
  'kill-region': 'Cut the selected region',
  'kill-ring-save': 'Copy the selected region',
  'set-mark-command': 'Start selecting from here',
  'delete-char': 'Delete the character under the cursor',
  'delete-backward-char': 'Delete the character before the cursor',
  'kill-word': 'Cut to the end of the word',
  'newline': 'Insert a line break',
  'indent-for-tab-command': 'Indent this line',
  'overwrite-mode': 'Toggle overwrite mode (typing replaces text)',
  'universal-argument': 'Give the next command a numeric argument',
  'next-error': 'Jump to the next compiler error',
  'goto-line': 'Jump to a line number',
  'xref-find-definitions': 'Jump to the definition (via the language server)',
  'xref-go-back': 'Jump back to where you came from',
  'compile': 'Run a build command and show its output',
  'comment-dwim': 'Comment or uncomment the region or line',
  'comment-line': 'Comment or uncomment this line',
  'self-insert-command': 'Insert the character you typed',
  'quit-window': 'Close this special window and go back',
  'Buffer-menu-this-window': 'Open the buffer on this line here',
  'dired-find-file': 'Open the file on this line',
  'compile-goto-error': 'Jump to this error in the source',
  'next-error-no-select': 'Show the next error',
  'recompile': 'Run the last build command again',
  'minibuffer-complete': 'Complete the name (TAB)',
  'exit-minibuffer': 'Accept what you typed',
  'abort-minibuffers': 'Cancel the prompt',
};

/* Which-key style lists for pending prefixes (what the popup shows). */
const WHICHKEY = {
  'C-x': [['C-f','find-file'],['C-s','save-buffer'],['C-b','list-buffers'],['b','switch-to-buffer'],['k','kill-buffer'],['1','delete-other-windows'],['2','split-window-below'],['3','split-window-right'],['0','delete-window'],['o','other-window'],['C-c','save-buffers-kill-terminal'],['C-q','read-only-mode'],['n','+narrow'],['u','undo'],['h','mark-whole-buffer'],['(','kmacro-start-macro'],[')','kmacro-end-macro']],
  'C-x n': [['n','narrow-to-region'],['d','narrow-to-defun'],['w','widen']],
  'C-h': [['k','describe-key'],['f','describe-function'],['m','describe-mode'],['t','help-with-tutorial'],['.','display-local-help']],
  'M-g': [['M-n','next-error'],['g','goto-line']],
  'C-c': [['C-c','compile']],
  'ESC': [['ESC ESC','keyboard-escape-quit'],['x','execute-extended-command (same as M-x)']],
  'ESC ESC': [['ESC','keyboard-escape-quit']],
};


/* VS Code muscle memory: what the learner probably meant vs what Emacs did.
   Shown once per key per session. Universal: needs no per-lesson authoring. */
const HABITS = {
  'C-z':   ['undo', 'C-z suspends Emacs to the shell. Undo is C-/ (also C-x u).'],
  'C-f':   ['find', 'C-f moves the cursor right one character. Search is C-s.'],
  'C-a':   ['select all', 'C-a goes to the start of the line. Select all is C-x h.'],
  'C-v':   ['paste', 'C-v scrolls down a page. Paste is C-y.'],
  'C-s':   ['save', 'C-s starts an incremental search (I-search at the bottom). Save is C-x C-s. C-g leaves the search.'],
  'C-p':   ['command palette', 'C-p moves up one line. The command palette is M-x.'],
  'C-o':   ['open file', 'C-o inserts a blank line. Open a file with C-x C-f.'],
  'C-w':   ['close tab', 'C-w cuts the selected region. Close a buffer with C-x k. Browsers often grab this key; use Focus mode.'],
  'C-n':   ['new file', 'C-n moves down one line. For a new file: C-x C-f and type a name that does not exist yet.'],
  'C-d':   ['select next match', 'C-d deletes the character under the cursor. Multiple cursors arrive in lesson 1.7.'],
  'C-b':   ['toggle sidebar', 'C-b moves left one character. The file sidebar is lesson 2.5.'],
  'C-e':   ['quick open', 'C-e goes to the end of the line. Quick open is C-x b (buffers) or C-x C-f (files).'],
  'C-k':   ['chord prefix', 'C-k cut the rest of the line into the kill ring. C-y brings it back.'],
  'C-r':   ['recent files', 'C-r searches backward. Recent files: C-x C-r.'],
  'C-l':   ['select line', 'C-l re-centres the screen on your line; nothing was selected.'],
  'C-h':   ['replace', 'C-h opens the help system. C-h k explains any key. Replace is M-%.'],
  'C-y':   ['redo', 'C-y pastes the last cut text. To redo after an undo: press C-g, then C-/ again.'],
  'C-SPC': ['trigger completion', 'C-SPC starts a selection (sets the mark). Completion pops up as you type; C-M-i forces it.'],
  'C-/':   ['toggle comment', 'C-/ is undo. Comment a line with M-; or C-x C-;.'],
};

const PRINTABLE = /^[\x20-\x7e]$/;
function isPrintable(k) { return k === 'SPC' || (k.length === 1 && PRINTABLE.test(k)); }
function keyChar(k) { return k === 'SPC' ? ' ' : k; }

function isPrefix(seq, local) {
  const s = seq + ' ';
  const maps = local ? [local, BINDINGS] : [BINDINGS];
  return maps.some(m => Object.keys(m).some(k => k.startsWith(s)));
}

/* ------------------------------------------------------------------ */
/* Engine                                                              */
/* ------------------------------------------------------------------ */

function makeBuffer(name, f) {
  return {
    name, mode: f.mode, modeName: f.modeName, minors: f.minors || '',
    dir: f.dir || null, text: f.text, orig: f.text, point: 0, mark: null,
    modified: false, readonly: !!f.readonly, special: !!f.special,
    narrow: null, overwrite: false, undo: [], scroll: 0,
    diags: f.diags ? f.diags.map(d => ({ ...d })) : [],
  };
}

function createEngine(opts) {
  opts = opts || {};
  const files = opts.files || HELLO_FILES;
  const st = {
    buffers: {},
    windows: (opts.windows || [{ buf: 'main.rs' }]).map(w => ({ ...w })),
    sel: 0,
    mini: null,          // { kind, prompt, input, ... }
    echo: '',
    prefix: [],
    prefixAt: 0,
    argPending: false,
    macro: false,
    closed: false,
    suspended: false,
    recursive: 0,
    kill: '',
    ticker: [],
    counts: {},
    total: 0,
    visited: [],
    listedAt: -1,
    lastCmd: null,
    miniSince: 0,
    prevBuf: null,
    describe: null,
    overlay: null,
    habitsSeen: {},
    dismissed: {},
    cards: [],
    lastHabitEntry: null,
    now: () => Date.now(),
  };
  for (const [name, f] of Object.entries(files)) st.buffers[name] = makeBuffer(name, f);
  const listeners = { change: [], save: [], command: [] };
  const timers = [];

  function emit(ev) { (listeners[ev || 'change'] || []).forEach(fn => fn(st)); }
  function on(ev, fn) { listeners[ev].push(fn); return () => { listeners[ev] = listeners[ev].filter(f => f !== fn); }; }
  function later(ms, fn) { if (typeof setTimeout === 'function') timers.push(setTimeout(fn, ms)); else fn(); }

  function cur() { return st.buffers[st.windows[st.sel].buf]; }
  function otherBufferName() {
    const curName = cur().name;
    if (st.prevBuf && st.buffers[st.prevBuf] && st.prevBuf !== curName) return st.prevBuf;
    return Object.keys(st.buffers).find(n => n !== curName && !st.buffers[n].special) || '*scratch*';
  }
  function showBuffer(name, winIndex) {
    const w = st.windows[winIndex == null ? st.sel : winIndex];
    if (w.buf !== name) st.prevBuf = w.buf;
    w.buf = name;
  }
  function ensureBuffer(name, f) {
    if (!st.buffers[name]) st.buffers[name] = makeBuffer(name, f || { mode: 'fundamental-mode', modeName: 'Fundamental', text: '' });
    return st.buffers[name];
  }

  /* --- text helpers --- */
  function lineOf(b, p) { const t = b.text.slice(0, p); const l = (t.match(/\n/g) || []).length; return [l, p - (t.lastIndexOf('\n') + 1)]; }
  function lineStart(b, p) { return b.text.lastIndexOf('\n', p - 1) + 1; }
  function lineEnd(b, p) { const e = b.text.indexOf('\n', p); return e < 0 ? b.text.length : e; }
  function clamp(b, p) { const lo = b.narrow ? b.narrow[0] : 0, hi = b.narrow ? b.narrow[1] : b.text.length; return Math.max(lo, Math.min(hi, p)); }
  function snapshot(b) { b.undo.push({ text: b.text, point: b.point }); if (b.undo.length > 100) b.undo.shift(); }
  function insert(b, s) {
    if (b.readonly) { st.echo = 'Buffer is read-only: #<buffer ' + b.name + '>'; return false; }
    snapshot(b);
    if (b.overwrite && s !== '\n' && b.point < b.text.length && b.text[b.point] !== '\n') b.text = b.text.slice(0, b.point) + s + b.text.slice(b.point + 1);
    else b.text = b.text.slice(0, b.point) + s + b.text.slice(b.point);
    if (b.narrow) b.narrow[1] += s.length;
    b.point += s.length; b.modified = b.text !== b.orig; shiftDiags(b, b.point - s.length, s.length);
    return true;
  }
  function del(b, from, to) {
    if (from >= to) return '';
    if (b.readonly) { st.echo = 'Buffer is read-only: #<buffer ' + b.name + '>'; return ''; }
    snapshot(b);
    const s = b.text.slice(from, to); b.text = b.text.slice(0, from) + b.text.slice(to);
    if (b.narrow) b.narrow[1] -= s.length;
    b.point = from; b.modified = b.text !== b.orig; shiftDiags(b, from, -s.length);
    return s;
  }
  function shiftDiags(b, at, delta) {
    for (const d of b.diags) {
      const [l] = lineOf(b, at);
      if (d.line === l) { const [, c] = lineOf(b, at); if (c <= d.col) d.col += delta; else if (c < d.col + d.len) d.len += delta; }
      else if (d.line > l && delta !== 0 && b.text.slice(at, at + Math.max(0, delta)).includes('\n')) d.line += 1;
    }
    b.diags = b.diags.filter(d => d.len > 0);
  }
  function moveLine(b, dir) {
    const [l, c] = lineOf(b, b.point);
    const lines = b.text.split('\n');
    const nl = l + dir; if (nl < 0 || nl >= lines.length) { st.echo = dir < 0 ? 'Beginning of buffer' : 'End of buffer'; return; }
    let p = 0; for (let i = 0; i < nl; i++) p += lines[i].length + 1;
    b.point = clamp(b, p + Math.min(c, lines[nl].length));
  }
  function wordMove(b, dir) {
    const re = dir > 0 ? /[A-Za-z0-9_]+/g : /[A-Za-z0-9_]+(?![A-Za-z0-9_])/g;
    if (dir > 0) { re.lastIndex = b.point; const m = re.exec(b.text); b.point = clamp(b, m ? m.index + m[0].length : b.text.length); }
    else { const t = b.text.slice(0, b.point); const ms = [...t.matchAll(/[A-Za-z0-9_]+/g)]; b.point = clamp(b, ms.length ? ms[ms.length - 1].index : 0); }
  }
  function diagAtPoint(b) {
    const [l, c] = lineOf(b, b.point);
    return b.diags.find(d => d.line === l && c >= d.col && c <= d.col + d.len);
  }

  /* --- ticker / bookkeeping --- */
  function pushTicker(e) { if (e.kind !== 'prefix') while (st.ticker.length && st.ticker[st.ticker.length - 1].kind === 'prefix') st.ticker.pop(); e.t = st.now(); st.ticker.push(e); if (st.ticker.length > 40) st.ticker.shift(); }
  function count(name) { st.counts[name] = (st.counts[name] || 0) + 1; st.total++; }

  /* --- minibuffer --- */
  function openMini(m) { st.mini = { input: '', ...m }; st.miniSince = st.now(); st.echo = ''; }
  function closeMini() { st.mini = null; st.miniSince = 0; }

  function completeMini() {
    const m = st.mini; let cands = [];
    if (m.kind === 'find-file') {
      const dir = m.input.slice(0, m.input.lastIndexOf('/') + 1); const part = m.input.slice(dir.length);
      cands = Object.values(st.buffers).filter(b => b.dir && b.dir.endsWith(dir.replace(/^~\//, '').replace(/^\/work\/hello\//, '') || '/')).map(b => b.name).filter(n => n.startsWith(part));
      if (cands.length === 1) m.input = dir + cands[0];
      else if (cands.length > 1) { const common = commonPrefix(cands); m.input = dir + common; st.echo = '{' + cands.join(' | ') + '}'; }
      else st.echo = '[No match]';
    } else if (m.kind === 'switch-buffer' || m.kind === 'kill-buffer') {
      cands = Object.keys(st.buffers).filter(n => n.toLowerCase().includes(m.input.toLowerCase()));
      if (cands.length === 1) m.input = cands[0]; else if (cands.length) st.echo = '{' + cands.join(' | ') + '}'; else st.echo = '[No match]';
    } else if (m.kind === 'M-x') {
      cands = Object.keys(GLOSS).filter(n => n.startsWith(m.input));
      if (cands.length === 1) m.input = cands[0]; else if (cands.length) { m.input = commonPrefix(cands); st.echo = '{' + cands.slice(0, 8).join(' | ') + (cands.length > 8 ? ' | …' : '') + '}'; } else st.echo = '[No match]';
    }
    pushTicker({ keys: ['TAB'], cmd: 'minibuffer-complete', gloss: GLOSS['minibuffer-complete'], kind: 'ok' });
  }
  function commonPrefix(xs) { let p = xs[0]; for (const x of xs) { while (!x.startsWith(p)) p = p.slice(0, -1); } return p; }

  function submitMini() {
    const m = st.mini; const input = m.input;
    pushTicker({ keys: ['RET'], cmd: 'exit-minibuffer', gloss: GLOSS['exit-minibuffer'], kind: 'ok' });
    if (m.kind === 'find-file') {
      closeMini();
      const path = input.trim();
      if (path === '' || path.endsWith('/')) { openDired(path); return; }
      const name = path.slice(path.lastIndexOf('/') + 1);
      const existing = st.buffers[name];
      if (existing && existing.dir) { showBuffer(name); st.visited.push(name); st.echo = ''; }
      else { const b = ensureBuffer(name, { mode: guessMode(name).mode, modeName: guessMode(name).modeName, text: '', dir: path.slice(0, path.lastIndexOf('/') + 1) || '/work/hello/' }); showBuffer(name); st.visited.push(name); st.echo = '(New file)'; }
    } else if (m.kind === 'switch-buffer') {
      closeMini(); const name = input || m.def; ensureBuffer(name); showBuffer(name); st.echo = '';
    } else if (m.kind === 'kill-buffer') {
      closeMini(); const name = input || m.def; if (!st.buffers[name]) { st.echo = 'No such buffer: ' + name; return; }
      delete st.buffers[name]; for (const w of st.windows) if (w.buf === name) w.buf = Object.keys(st.buffers).find(n => !st.buffers[n].special) || '*scratch*'; ensureBuffer('*scratch*', HELLO_FILES['*scratch*']); st.echo = '';
    } else if (m.kind === 'M-x') {
      if (!GLOSS[input] || input === 'self-insert-command') { st.echo = '[No match]'; return; }
      closeMini(); runCommand(input, ['M-x', input, 'RET'], null, true);
    } else if (m.kind === 'query-replace-from') {
      if (!input) { st.echo = '[Empty string]'; return; }
      openMini({ kind: 'query-replace-to', prompt: 'Query replace ' + input + ' with: ', from: input });
    } else if (m.kind === 'query-replace-to') {
      closeMini(); const b = cur(); const n = b.text.split(m.from).length - 1;
      if (n) { snapshot(b); b.text = b.text.split(m.from).join(input); b.modified = b.text !== b.orig; }
      st.echo = 'Replaced ' + n + ' occurrence' + (n === 1 ? '' : 's');
    } else if (m.kind === 'goto-line') {
      closeMini(); const b = cur(); const lines = b.text.split('\n'); const n = Math.max(1, Math.min(lines.length, parseInt(input || '1', 10) || 1));
      let p = 0; for (let i = 0; i < n - 1; i++) p += lines[i].length + 1; b.point = clamp(b, p); st.echo = '';
    } else if (m.kind === 'compile') {
      closeMini(); doCompile(input || 'cargo build');
    } else if (m.kind === 'describe-function') {
      closeMini(); openHelp(input + ' is an interactive command.\n\n' + (GLOSS[input] || '(not in this demo)') + '\n\nIt is bound to ' + (Object.keys(BINDINGS).filter(k => BINDINGS[k] === input).join(', ') || 'no key') + '.');
    } else { closeMini(); }
  }
  function abortMini() {
    const m = st.mini; closeMini();
    if (m.kind === 'isearch') cur().point = m.start;
    st.echo = 'Quit';
  }
  function guessMode(name) {
    if (name.endsWith('.rs')) return { mode: 'rust-ts-mode', modeName: 'Rust' };
    if (name.endsWith('.md')) return { mode: 'markdown-mode', modeName: 'Markdown' };
    if (name.endsWith('.toml')) return { mode: 'conf-toml-mode', modeName: 'Conf[TOML]' };
    if (name.endsWith('.py')) return { mode: 'python-ts-mode', modeName: 'Python' };
    if (name.endsWith('.typ')) return { mode: 'typst-ts-mode', modeName: 'Typst' };
    if (name.endsWith('.ml')) return { mode: 'tuareg-mode', modeName: 'Tuareg' };
    return { mode: 'fundamental-mode', modeName: 'Fundamental' };
  }

  function openDired(path) {
    const dir = path || '/work/hello/src/';
    const entries = Object.values(st.buffers).filter(b => b.dir).map(b => '  -rw-r--r-- 1 learner learner ' + String(b.text.length).padStart(5) + ' Oct 26 17:55 ' + b.name);
    const text = '  ' + dir.replace(/\/$/, '') + ':\n  total used in directory 24 available 1.2 TiB\n  drwxr-xr-x 1 learner learner  4096 Oct 26 17:55 .\n  drwxr-xr-x 1 learner learner  4096 Oct 26 17:55 ..\n' + entries.join('\n') + '\n';
    const name = dir; const b = ensureBuffer(name, { mode: 'dired-mode', modeName: 'Dired by name', special: true, readonly: true, text }); b.text = text; b.point = text.indexOf('\n', text.indexOf(' ..')) + 1 + 36;
    showBuffer(name); st.echo = '';
  }
  function openHelp(text) {
    const b = ensureBuffer('*Help*', { mode: 'help-mode', modeName: 'Help', special: true, readonly: true, text }); b.text = text; b.point = 0;
    showInOther('*Help*');
  }
  function showInOther(name) {
    if (st.windows.length === 1) st.windows.push({ buf: name });
    else st.windows[(st.sel + 1) % st.windows.length].buf = name;
  }
  function bufferList() {
    const rows = Object.values(st.buffers).map(b => ' ' + (b.readonly ? '%' : ' ') + (b.modified ? '*' : ' ') + ' ' + b.name.padEnd(16) + String(b.text.length).padStart(6) + '  ' + b.modeName.padEnd(18) + (b.dir ? b.dir + b.name : ''));
    return ' CRM Buffer           Size  Mode              File\n' + rows.join('\n') + '\n';
  }
  function doCompile(cmdline) {
    const main = st.buffers['main.rs']; const ok = main && /println!\("\{\}", greet\(&name\)\)/.test(main.text);
    const b = ensureBuffer('*compilation*', { mode: 'compilation-mode', modeName: 'Compilation', special: true, readonly: true, text: '' });
    b.text = (ok ? COMPILE_OK : COMPILE_FAIL).replace('cargo build', cmdline); b.point = 0;
    if (ok && main) main.diags = [];
    showInOther('*compilation*');
    st.echo = ok ? 'Compilation finished' : 'Compilation exited abnormally with code 101';
    emit('save');
  }

  /* --- isearch --- */
  function searchFrom(m) {
    const b = cur(); if (!m.input) { b.point = m.start; m.fail = false; return; }
    const i = b.text.indexOf(m.input, m.origin);
    if (i >= 0) { b.point = i + m.input.length; m.match = [i, i + m.input.length]; m.fail = false; }
    else { m.fail = true; m.match = null; }
  }

  /* --- commands --- */
  const CMD = {
    'find-file': () => openMini({ kind: 'find-file', prompt: 'Find file: ', input: (cur().dir || '/work/hello/').replace('/work/hello/', '~/hello/') }),
    'save-buffer': () => { const b = cur(); if (b.special) { st.echo = 'Buffer ' + b.name + ' is not visiting a file'; return; } if (!b.modified) { st.echo = '(No changes need to be saved)'; return; } b.modified = false; b.orig = b.text; st.echo = 'Wrote ' + (b.dir || '/work/hello/') + b.name; emit('save'); },
    'list-buffers': () => { const b = ensureBuffer('*Buffer List*', { mode: 'Buffer-menu-mode', modeName: 'Buffer Menu', special: true, readonly: true, text: '' }); b.text = bufferList(); b.point = b.text.indexOf('\n') + 1; showInOther('*Buffer List*'); st.listedAt = st.total; st.echo = 'Commands: d, s, x, u; f, o, 1, 2, m, v; ~, %; q to quit; ? for help.'; },
    'switch-to-buffer': () => openMini({ kind: 'switch-buffer', prompt: 'Switch to buffer (default ' + otherBufferName() + '): ', def: otherBufferName() }),
    'kill-buffer': () => openMini({ kind: 'kill-buffer', prompt: 'Kill buffer (default ' + cur().name + '): ', def: cur().name }),
    'delete-window': () => { if (st.windows.length === 1) { st.echo = 'Attempt to delete sole ordinary window'; return; } st.windows.splice(st.sel, 1); st.sel = Math.max(0, st.sel - 1); },
    'delete-other-windows': () => { st.windows = [st.windows[st.sel]]; st.sel = 0; },
    'split-window-below': () => { if (st.windows.length >= 3) { st.echo = 'Window too small for splitting'; return; } st.windows.splice(st.sel + 1, 0, { buf: cur().name }); },
    'split-window-right': () => { if (st.windows.length >= 3) { st.echo = 'Window too small for splitting'; return; } st.windows.splice(st.sel + 1, 0, { buf: cur().name, side: true }); },
    'other-window': () => { st.sel = (st.sel + 1) % st.windows.length; },
    'save-buffers-kill-terminal': () => { const dirty = Object.values(st.buffers).filter(b => b.modified && b.dir); if (dirty.length) { st.echo = 'Save file ' + dirty[0].dir + dirty[0].name + '? (y, n, !, ., q, C-r, C-f, d or C-h)  [demo: assume n]'; } st.closed = true; st.overlay = 'closed'; later(2600, () => { st.closed = false; st.overlay = null; st.echo = 'Playground: your frame was closed by C-x C-c; it has been reopened. Buffers are intact.'; emit(); }); },
    'read-only-mode': () => { const b = cur(); b.readonly = !b.readonly; st.echo = b.readonly ? 'Read-Only mode enabled in current buffer' : 'Read-Only mode disabled in current buffer'; },
    'narrow-to-region': () => { const b = cur(); if (b.mark == null) { st.echo = 'The mark is not set now, so there is no region'; return; } b.narrow = [Math.min(b.mark, b.point), Math.max(b.mark, b.point)]; b.mark = null; },
    'narrow-to-defun': () => { const b = cur(); const t = b.text; const before = t.slice(0, b.point); const s = Math.max(before.lastIndexOf('\nfn '), before.lastIndexOf('\npub fn '), before.lastIndexOf('\ndef '), before.lastIndexOf('\nlet ')); const start = s < 0 ? 0 : s + 1; let e = t.indexOf('\n}\n', start); e = e < 0 ? t.length : e + 3; b.narrow = [start, e]; b.point = clamp(b, b.point); },
    'widen': () => { cur().narrow = null; },
    'kmacro-start-macro': () => { if (st.macro) { st.echo = 'Already defining keyboard macro'; return; } st.macro = true; st.echo = 'Defining kbd macro...'; },
    'kmacro-end-macro': () => { if (!st.macro) { st.echo = 'Not defining kbd macro'; return; } st.macro = false; st.echo = 'Keyboard macro defined'; },
    'kmacro-end-or-call-macro': () => { if (st.macro) { st.macro = false; st.echo = 'Keyboard macro defined'; } else st.echo = 'No kbd macro has been defined'; },
    'undo': () => { const b = cur(); const u = b.undo.pop(); if (!u) { st.echo = 'No further undo information'; return; } b.text = u.text; b.point = u.point; b.modified = b.text !== b.orig; st.echo = 'Undo'; },
    'eval-last-sexp': () => { st.echo = cur().mode === 'lisp-interaction-mode' ? '42' : 'End of file during parsing'; },
    'mark-whole-buffer': () => { const b = cur(); b.mark = b.text.length; b.point = 0; st.echo = 'Mark set'; },
    'keyboard-quit': () => { if (st.mini) { abortMini(); return; } if (st.macro) { st.macro = false; st.echo = 'Quit (keyboard macro cancelled)'; return; } cur().mark = null; st.echo = 'Quit'; },
    'keyboard-escape-quit': () => { if (st.mini) closeMini(); st.recursive = 0; if (st.windows.length > 1) { st.windows = [st.windows[st.sel]]; st.sel = 0; } cur().mark = null; st.echo = ''; },
    'isearch-forward': () => { const b = cur(); openMini({ kind: 'isearch', prompt: 'I-search: ', start: b.point, origin: b.point }); },
    'isearch-backward': () => { const b = cur(); openMini({ kind: 'isearch', prompt: 'I-search backward: ', start: b.point, origin: 0 }); },
    'execute-extended-command': () => openMini({ kind: 'M-x', prompt: 'M-x ' }),
    'query-replace': () => openMini({ kind: 'query-replace-from', prompt: 'Query replace: ' }),
    'describe-key': () => { st.describe = []; st.echo = 'Describe key (or click or menu item): '; },
    'describe-function': () => openMini({ kind: 'describe-function', prompt: 'Describe function: ' }),
    'describe-mode': () => openHelp(cur().name + ' is in ' + cur().modeName + ' mode.\n\nThe major mode is ' + cur().mode + '.\nMinor modes enabled: ' + (cur().minors || 'none') + '.\n\nkey  binding\n---  -------\nC-c C-c  compile\nM-.      xref-find-definitions\n'),
    'help-with-tutorial': () => openHelp('Emacs tutorial.  See end for copying conditions.\n\nEmacs commands generally involve the CONTROL key...\n\n(In the playground this is replaced by the lesson pane on the left.)'),
    'display-local-help': () => { const d = diagAtPoint(cur()); st.echo = d ? d.msg : 'No local help at point'; },
    'suspend-frame': () => { st.suspended = true; st.overlay = 'suspended'; later(1800, () => { st.suspended = false; st.overlay = null; st.echo = 'Playground: Emacs was suspended with C-z and resumed automatically. On a real terminal you would type fg.'; emit(); }); },
    'move-beginning-of-line': () => { const b = cur(); b.point = clamp(b, lineStart(b, b.point)); },
    'move-end-of-line': () => { const b = cur(); b.point = clamp(b, lineEnd(b, b.point)); },
    'forward-char': () => { const b = cur(); if (b.point >= (b.narrow ? b.narrow[1] : b.text.length)) { st.echo = 'End of buffer'; return; } b.point++; },
    'backward-char': () => { const b = cur(); if (b.point <= (b.narrow ? b.narrow[0] : 0)) { st.echo = 'Beginning of buffer'; return; } b.point--; },
    'next-line': () => moveLine(cur(), 1),
    'previous-line': () => moveLine(cur(), -1),
    'forward-word': () => wordMove(cur(), 1),
    'backward-word': () => wordMove(cur(), -1),
    'beginning-of-buffer': () => { const b = cur(); b.mark = b.point; b.point = clamp(b, 0); st.echo = 'Mark set'; },
    'end-of-buffer': () => { const b = cur(); b.mark = b.point; b.point = clamp(b, b.text.length); st.echo = 'Mark set'; },
    'scroll-up-command': () => { const b = cur(); for (let i = 0; i < 10; i++) moveLine(b, 1); },
    'scroll-down-command': () => { const b = cur(); for (let i = 0; i < 10; i++) moveLine(b, -1); },
    'recenter-top-bottom': () => {},
    'kill-line': () => { const b = cur(); const e = lineEnd(b, b.point); st.kill = e === b.point ? del(b, b.point, Math.min(b.text.length, b.point + 1)) : del(b, b.point, e); },
    'yank': () => { if (!st.kill) { st.echo = 'Kill ring is empty'; return; } insert(cur(), st.kill); },
    'kill-region': () => { const b = cur(); if (b.mark == null) { st.echo = 'The mark is not set now, so there is no region'; return; } st.kill = del(b, Math.min(b.mark, b.point), Math.max(b.mark, b.point)); b.mark = null; },
    'kill-ring-save': () => { const b = cur(); if (b.mark == null) { st.echo = 'The mark is not set now, so there is no region'; return; } st.kill = b.text.slice(Math.min(b.mark, b.point), Math.max(b.mark, b.point)); b.mark = null; st.echo = 'Copied'; },
    'set-mark-command': () => { const b = cur(); b.mark = b.point; st.echo = 'Mark set'; },
    'delete-char': () => { const b = cur(); if (b.point >= b.text.length) { st.echo = 'End of buffer'; return; } del(b, b.point, b.point + 1); },
    'delete-backward-char': () => { const b = cur(); if (b.point <= (b.narrow ? b.narrow[0] : 0)) { st.echo = 'Beginning of buffer'; return; } del(b, b.point - 1, b.point); },
    'kill-word': () => { const b = cur(); const p = b.point; wordMove(b, 1); const e = b.point; b.point = p; st.kill = del(b, p, e); },
    'newline': () => insert(cur(), '\n'),
    'indent-for-tab-command': () => { const b = cur(); const ls = lineStart(b, b.point); const want = '    '; if (!b.text.startsWith(want, ls)) { const p = b.point; b.point = ls; insert(b, want); b.point = p + want.length; } },
    'overwrite-mode': () => { const b = cur(); b.overwrite = !b.overwrite; st.echo = b.overwrite ? 'Overwrite mode enabled in current buffer' : 'Overwrite mode disabled in current buffer'; },
    'universal-argument': () => { st.argPending = true; st.echo = 'C-u-'; },
    'next-error': () => { const main = st.buffers['main.rs']; if (!main || !main.diags.length) { st.echo = 'No more errors'; return; } let wi = st.windows.findIndex(w => w.buf === 'main.rs'); if (wi < 0) { showBuffer('main.rs'); wi = st.sel; } st.sel = wi; const d = main.diags[0]; const lines = main.text.split('\n'); let p = 0; for (let i = 0; i < d.line; i++) p += lines[i].length + 1; main.point = p + d.col; st.echo = d.msg; },
    'goto-line': () => openMini({ kind: 'goto-line', prompt: 'Goto line: ' }),
    'xref-find-definitions': () => { const b = cur(); const [l] = lineOf(b, b.point); const line = b.text.split('\n')[l] || ''; if (/greet/.test(line) && st.buffers['lib.rs']) { st.prevBuf = b.name; showBuffer('lib.rs'); st.buffers['lib.rs'].point = 7; st.echo = ''; } else st.echo = 'No definitions found for: ' + (line.match(/[A-Za-z_]+/) || ['identifier'])[0]; },
    'xref-go-back': () => { if (st.prevBuf && st.buffers[st.prevBuf]) showBuffer(st.prevBuf); else st.echo = 'At start of xref history.'; },
    'compile': () => openMini({ kind: 'compile', prompt: 'Compile command: ', input: 'cargo build' }),
    'comment-dwim': () => { const b = cur(); const ls = lineStart(b, b.point); const p = b.point; b.point = ls; insert(b, '// '); b.point = p + 3; },
    'comment-line': () => CMD['comment-dwim'](),
    'self-insert-command': (ch) => insert(cur(), ch),
    'quit-window': () => { const name = cur().name; if (st.windows.length > 1) { st.windows.splice(st.sel, 1); st.sel = Math.max(0, st.sel - 1); } else { showBuffer(otherBufferName()); } st.echo = ''; void name; },
    'Buffer-menu-this-window': () => { const b = cur(); const [l] = lineOf(b, b.point); const line = b.text.split('\n')[l] || ''; const name = line.slice(4, 20).trim(); if (st.buffers[name]) { showBuffer(name); st.echo = ''; } else st.echo = 'No buffer on this line'; },
    'dired-find-file': () => { const b = cur(); const [l] = lineOf(b, b.point); const line = b.text.split('\n')[l] || ''; const name = line.trim().split(/\s+/).pop(); if (st.buffers[name] && st.buffers[name].dir) { showBuffer(name); st.visited.push(name); st.echo = ''; } else st.echo = 'No file on this line'; },
    'compile-goto-error': () => CMD['next-error'](),
    'next-error-no-select': () => CMD['next-error'](),
    'recompile': () => doCompile('cargo build'),
  };

  function runCommand(name, seq, arg, viaMx) {
    const fn = CMD[name];
    st.lastCmd = name; count(name);
    if (!viaMx) pushTicker({ keys: seq, cmd: name, gloss: GLOSS[name] || '', kind: name === 'keyboard-quit' || name === 'keyboard-escape-quit' ? 'quit' : 'ok' });
    else pushTicker({ keys: ['M-x'], cmd: name, gloss: GLOSS[name] || '', kind: 'ok', mx: true });
    st.echo = '';
    if (fn) fn(arg); else st.echo = '(' + name + ' is not simulated in this demo)';
    st.argPending = false;
    const b = cur(); const d = diagAtPoint(b); if (d && !st.echo && !st.mini) st.echo = d.msg;
    emit('command'); emit();
  }

  function miniKey(key) {
    const m = st.mini;
    if (key === 'C-g') { abortMini(); pushTicker({ keys: ['C-g'], cmd: 'abort-minibuffers', gloss: GLOSS['abort-minibuffers'], kind: 'quit' }); count('keyboard-quit'); emit(); return true; }
    if (m.kind === 'isearch') {
      if (key === 'RET') { closeMini(); st.echo = ''; pushTicker({ keys: ['RET'], cmd: 'isearch-exit', gloss: GLOSS['isearch-exit'], kind: 'ok' }); emit(); return true; }
      if (key === 'C-s') { if (m.match) m.origin = m.match[0] + 1; searchFrom(m); if (m.fail) m.origin = 0; pushTicker({ keys: ['C-s'], cmd: 'isearch-repeat-forward', gloss: GLOSS['isearch-repeat-forward'], kind: 'ok' }); emit(); return true; }
      if (key === 'DEL') { m.input = m.input.slice(0, -1); searchFrom(m); emit(); return true; }
      if (isPrintable(key)) { m.input += keyChar(key); searchFrom(m); emit(); return true; }
      closeMini(); st.echo = ''; return false;   // other keys leave isearch and run normally
    }
    if (key === 'RET') { submitMini(); emit(); return true; }
    if (key === 'DEL') { m.input = m.input.slice(0, -1); st.echo = ''; emit(); return true; }
    if (key === 'TAB') { completeMini(); emit(); return true; }
    if (isPrintable(key)) { m.input += keyChar(key); st.echo = ''; emit(); return true; }
    if (['C-a', 'C-e', 'C-f', 'C-b', '<left>', '<right>', '<home>', '<end>'].includes(key)) return true;
    if (key === 'M-x' || key === 'ESC') { st.echo = 'Command attempted to use minibuffer while in minibuffer'; pushTicker({ keys: [key], cmd: '(blocked)', gloss: 'A prompt is already open. Finish it with RET or cancel with C-g.', kind: 'unbound' }); emit(); return true; }
    if (key.startsWith('C-') || key.startsWith('M-') || key.startsWith('<')) { st.echo = 'Command attempted to use minibuffer while in minibuffer'; pushTicker({ keys: [key], cmd: '(blocked)', gloss: 'A prompt is already open. Finish it with RET or cancel with C-g.', kind: 'unbound' }); emit(); return true; }
    return true;
  }

  function handleKey(key) {
    if (st.closed || st.suspended) return;
    if (st.describe) {   // C-h k is reading a key sequence
      st.describe.push(key); const s = st.describe.join(' ');
      const local = cur().special ? LOCAL[cur().mode] : null;
      const name = (local && local[s]) || BINDINGS[s];
      if (name) { st.describe = null; count('describe-key'); pushTicker({ keys: ['C-h', 'k', ...s.split(' ')], cmd: 'describe-key', gloss: 'Explained ' + s, kind: 'ok' }); openHelp(s + ' runs the command ' + name + ', which is an interactive command.\n\n' + (GLOSS[name] || '') + '.\n\nIt is bound to ' + Object.keys(BINDINGS).filter(k => BINDINGS[k] === name).join(', ') + '.'); st.echo = ''; emit(); }
      else if (isPrefix(s, local)) { st.echo = 'Describe key (or click or menu item): ' + s + '-'; emit(); }
      else { st.describe = null; st.echo = s + ' is undefined'; emit(); }
      return;
    }
    if (st.prefix.length === 1 && st.prefix[0] === 'ESC' && key !== 'ESC') { st.prefix = []; key = 'M-' + key; }
    if (st.mini && st.prefix.length === 0 && miniKey(key)) return;
    const seq = [...st.prefix, key]; const s = seq.join(' ');
    const b = cur(); const local = b.special ? LOCAL[b.mode] : null;
    const name = (local && local[s]) || BINDINGS[s];
    if (name) { st.prefix = []; runCommand(name, seq); return; }
    if (isPrefix(s, local)) { st.prefix = seq; st.prefixAt = st.now(); st.echo = s + '-'; pushTicker({ keys: seq, kind: 'prefix' }); emit(); later(450, () => emit()); return; }
    if (st.prefix.length === 0 && isPrintable(key)) { runCommand('self-insert-command', [key], keyChar(key)); return; }
    st.prefix = []; st.echo = s + ' is undefined'; count('(unbound)');
    pushTicker({ keys: seq, cmd: '(unbound)', gloss: 'Nothing is bound to this key here', kind: 'unbound' }); emit();
  }

  function sendKeys(str) { for (const k of str.trim().split(/\s+/)) if (k) handleKey(k); }
  function typeText(text) { for (const ch of text) handleKey(ch === ' ' ? 'SPC' : ch === '\n' ? 'RET' : ch); }

  function rescue() {
    const did = [];
    if (st.describe) { st.describe = null; did.push('cancelled the C-h k prompt'); }
    if (st.mini) { abortMini(); did.push('cancelled the prompt'); }
    if (st.prefix.length) { st.prefix = []; did.push('dropped the pending prefix'); }
    if (st.macro) { st.macro = false; did.push('stopped the keyboard macro'); }
    if (st.recursive) { st.recursive = 0; did.push('left the recursive edit'); }
    if (st.windows.length > 1) { const keep = st.windows.find((w, i) => !st.buffers[w.buf].special && i === st.sel) || st.windows.find(w => !st.buffers[w.buf].special) || st.windows[st.sel]; st.windows = [keep]; st.sel = 0; did.push('closed the extra windows'); }
    const b = cur();
    if (b.special && b.mode !== 'lisp-interaction-mode') { showBuffer(otherBufferName()); did.push('left ' + b.name); }
    const c = cur();
    if (c.narrow) { c.narrow = null; did.push('widened the buffer'); }
    if (c.overwrite) { c.overwrite = false; did.push('turned off overwrite mode'); }
    if (c.mark != null) { c.mark = null; }
    st.echo = did.length ? 'Rescue: ' + did.join(', ') + '.' : 'Rescue: nothing to undo — you were already in a normal state.';
    pushTicker({ keys: ['Rescue'], cmd: 'playground-rescue', gloss: st.echo.replace('Rescue: ', ''), kind: 'quit' });
    count('playground-rescue'); emit();
    return did;
  }


  /* Universal diagnostics over the recent key stream. Pure function of state;
     no lesson knowledge. Each nudge has an id so the page can dismiss it. */
  function nudges() {
    const out = []; const now = st.now(); const t = st.ticker;
    const last = t[t.length - 1];
    if (last && last.kind !== 'prefix' && !last.mx && last !== st.lastHabitEntry) {
      st.lastHabitEntry = last;
      const first = last.keys[0];
      const h = HABITS[first];
      if (h && last.keys.length === 1 && !st.habitsSeen[first]) {
        st.habitsSeen[first] = true;   // one card per habit per session
        st.cards.push({ id: 'habit:' + first, kind: 'habit', title: 'Coming from VS Code? ' + first + ' is not ' + h[0] + '.', body: h[1], at: st.total });
      }
    }
    st.cards = st.cards.filter(c => st.total - c.at <= 12);   // cards fade after a dozen commands
    out.push(...st.cards);
    const recentQuits = t.filter(e => (e.cmd === 'keyboard-quit' || e.cmd === 'abort-minibuffers') && now - e.t < 8000).length;
    if (recentQuits >= 3) out.push({ id: 'panic', kind: 'calm', title: 'Three C-g in a row.', body: 'Nothing is broken; C-g is always safe. If the screen still looks wrong, press Rescue, or read "Where am I" on the right.' });
    const unb = t.filter(e => e.kind !== 'prefix').slice(-2);
    if (unb.length === 2 && unb.every(e => e.kind === 'unbound') && unb[0].keys.join(' ') === unb[1].keys.join(' ')) out.push({ id: 'unbound:' + unb[1].keys.join(' '), kind: 'ask', title: unb[1].keys.join(' ') + ' is not bound here.', body: 'Ask Emacs what a key does: press C-h k, then the key. In this buffer the common keys are in the cheat sheet below.' });
    if (last && last.cmd === 'self-insert-command' && cur().readonly) out.push({ id: 'ro:' + cur().name, kind: 'ask', title: 'You typed into ' + cur().name + ', which is read-only.', body: cur().special ? 'Press q to leave it, then type in your file.' : 'C-x C-q makes this buffer writable.' });
    if (st.mini && now - st.miniSince > 20000) out.push({ id: 'mini-stuck', kind: 'calm', title: 'Still in the prompt?', body: 'RET accepts what you typed. C-g cancels and puts you back where you were. TAB completes names.' });
    if (st.windows.length >= 3) out.push({ id: 'many-windows', kind: 'ask', title: 'Three windows is usually an accident.', body: 'C-x 1 keeps only the current window. (C-x 2 and C-x 3 split; it is easy to press them twice.)' });
    return out.filter(n => !st.dismissed[n.id]);
  }
  function dismiss(id) { st.dismissed[id] = true; emit(); }

  /* --- scripted playback --- */
  function play(steps, hooks) {
    hooks = hooks || {}; let i = 0; let cancelled = false;
    function next() {
      if (cancelled || i >= steps.length) { if (hooks.done && !cancelled) hooks.done(); return; }
      const s = steps[i++];
      if (s.say && hooks.say) hooks.say(s.say);
      if (s.wait) { later(s.wait, next); return; }
      if (s.keys) { const ks = s.keys.trim().split(/\s+/); let j = 0; (function tick() { if (cancelled) return; if (j < ks.length) { handleKey(ks[j++]); later(s.slow || 170, tick); } else later(80, next); })(); return; }
      if (s.type != null) { const cs = [...s.type]; let j = 0; (function tick() { if (cancelled) return; if (j < cs.length) { const ch = cs[j++]; handleKey(ch === ' ' ? 'SPC' : ch); later(s.slow || 70, tick); } else later(80, next); })(); return; }
      if (s.rescue) { rescue(); later(300, next); return; }
      next();
    }
    next();
    return () => { cancelled = true; };
  }

  /* --- derived views --- */
  function whereAmI() {
    const b = cur(); const exits = []; const flags = [];
    if (st.overlay === 'closed') exits.push(['The Emacs frame was closed with C-x C-c.', 'The playground reopens it in a moment. On your own machine this quits Emacs for real.']);
    if (st.overlay === 'suspended') exits.push(['Emacs is suspended (C-z sends it to the background).', 'The playground resumes it. In a plain terminal you would type fg.']);
    if (st.describe) exits.push(['C-h k is waiting for a key to explain.', 'Press the key you are curious about, or C-g to cancel.']);
    if (st.mini) {
      if (st.mini.kind === 'isearch') { flags.push('searching'); exits.push(['You are searching (I-search).', 'RET stops at this match. C-g jumps back to where you started.']); }
      else { flags.push('prompt open'); exits.push(['A prompt is open: ' + st.mini.prompt.trim(), 'RET accepts, C-g cancels. TAB completes names.']); }
    }
    if (st.prefix.length) { flags.push('prefix ' + st.prefix.join(' ')); exits.push(['Prefix ' + st.prefix.join(' ') + ' is waiting for the next key.', 'C-g cancels it. The popup lists what you can press.']); }
    if (st.argPending) exits.push(['C-u is waiting: the next command gets an argument.', 'C-g cancels it.']);
    if (st.macro) { flags.push('Def'); exits.push(['Recording a keyboard macro (Def in the mode line).', 'F4 stops recording.']); }
    if (b.overwrite) { flags.push('Ovwrt'); exits.push(['Overwrite mode: typing replaces text (Ovwrt in the mode line).', 'Press Insert to go back to inserting.']); }
    if (b.narrow) { flags.push('Narrow'); exits.push(['Narrowed: you see only part of this buffer (Narrow in the mode line).', 'C-x n w shows everything again.']); }
    if (b.readonly && !b.special) { flags.push('read-only'); exits.push(['Read-only buffer (%% in the mode line).', 'C-x C-q makes it writable.']); }
    if (b.mark != null) flags.push('region active');
    if (st.windows.length > 1) {
      const others = st.windows.filter((w, i) => i !== st.sel); const special = others.find(w => st.buffers[w.buf].special);
      if (special) exits.push(['A second window shows ' + special.buf + '.', 'Press q inside it, or C-x 1 here to keep only this window.']);
      else exits.push(['The frame is split into ' + st.windows.length + ' windows.', 'C-x 1 keeps only this one. C-x o moves between them.']);
    }
    if (b.mode === 'dired-mode') exits.push(['This is Dired, a directory listing (you opened a folder).', 'RET on a file opens it. q leaves the listing.']);
    if (b.mode === 'help-mode' || b.mode === 'Buffer-menu-mode' || b.mode === 'compilation-mode') exits.push(['You are inside ' + b.name + '.', 'q closes it and returns you to the previous buffer.']);
    return { buffer: b, exits, flags, windows: st.windows.length, mini: st.mini, mode: b.mode, modeName: b.modeName, minors: b.minors };
  }

  function whichKeyEntries() {
    if (!st.prefix.length || st.now() - st.prefixAt < 400) return null;
    const s = st.prefix.join(' ');
    const list = WHICHKEY[s];
    if (list) return { prefix: s, entries: list };
    const entries = Object.keys(BINDINGS).filter(k => k.startsWith(s + ' ')).map(k => [k.slice(s.length + 1), BINDINGS[k]]);
    return entries.length ? { prefix: s, entries } : null;
  }

  function dispose() { timers.forEach(t => clearTimeout(t)); }

  return { st, on, handleKey, sendKeys, typeText, rescue, play, whereAmI, whichKeyEntries, nudges, dismiss, cur, emit, dispose, lineOf, diagAtPoint, GLOSS, BINDINGS, HABITS };
}

/* ------------------------------------------------------------------ */
/* Renderer (DOM)                                                      */
/* ------------------------------------------------------------------ */

function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }

const HL = {
  'rust-ts-mode': [/(\/\/.*$)|("(?:[^"\\]|\\.)*")|(\b(?:fn|let|mut|pub|use|struct|impl|enum|match|if|else|return|for|in|while|loop|mod|crate|self|Self|as|where|trait|type|const|static|ref|move|dyn|unsafe|async|await)\b)|(\b[A-Z][A-Za-z0-9_]*\b)|(\b[a-z_][a-z0-9_]*!)|(\b[a-z_][a-z0-9_]*(?=\())|(\b\d+\b)/gm, ['cm', 'str', 'kw', 'ty', 'mac', 'fn', 'num']],
  'python-ts-mode': [/(#.*$)|("(?:[^"\\]|\\.)*")|(\b(?:def|return|if|else|elif|for|in|import|from|class|while|with|as|lambda|yield|not|and|or|None|True|False)\b)|(\b[a-z_][a-z0-9_]*(?=\())|(\b\d+\b)/gm, ['cm', 'str', 'kw', 'fn', 'num']],
  'tuareg-mode': [/(\(\*.*?\*\))|("(?:[^"\\]|\\.)*")|(\b(?:let|rec|in|if|then|else|fun|function|match|with|type|module|struct|sig|end|open|and|of|begin|done|do|while|for|to)\b)|(\b\d+\b)/gm, ['cm', 'str', 'kw', 'num']],
  'typst-ts-mode': [/(\/\/.*$)|("(?:[^"\\]|\\.)*")|(^=+ .*$)|(#[a-z]+)|(\$[^$]*\$)|(\b\d+(?:\.\d+)?(?:cm|pt|mm|em)?\b)/gm, ['cm', 'str', 'hd', 'kw', 'math', 'num']],
  'conf-toml-mode': [/(#.*$)|(^\[.*\]$)|("(?:[^"\\]|\\.)*")|(^[a-z_-]+(?=\s*=))/gm, ['cm', 'hd', 'str', 'kw']],
  'markdown-mode': [/(^#+ .*$)|(`[^`]+`)|(^\s*[-*] )/gm, ['hd', 'str', 'kw']],
  'lisp-interaction-mode': [/(;.*$)|("(?:[^"\\]|\\.)*")|(\b(?:defun|let|setq|lambda|if|when|unless)\b)/gm, ['cm', 'str', 'kw']],
  'compilation-mode': [/(^error(?:\[E\d+\])?:.*$)|(^warning:.*$)|(^\s*-->.*$|^src\/.*$)|(^help:.*$)|(^\s*Finished.*$|^Compilation finished.*$)|(^Compilation exited abnormally.*$)/gm, ['err', 'warn', 'fn', 'ok', 'ok', 'err']],
  'help-mode': [/(\b[a-z]+(?:-[a-z]+)+\b)|(^.*runs the command.*$)/gm, ['fn', 'hd']],
  'Buffer-menu-mode': [/(^ CRM.*$)|(^\s*%?\*?\s+\S+)/gm, ['hd', 'fn']],
  'dired-mode': [/(^\s*\/.*:$)|(\bd[rwx-]{9}\b.*$)|(\S+\.(?:rs|md|toml|py|typ|ml)$)/gm, ['hd', 'kw', 'fn']],
};

function classesFor(line, mode) {
  const cls = new Array(line.length).fill('');
  const rule = HL[mode]; if (!rule) return cls;
  const [re, names] = rule; re.lastIndex = 0; let m;
  while ((m = re.exec(line))) {
    if (m[0].length === 0) { re.lastIndex++; continue; }
    const gi = m.slice(1).findIndex(g => g !== undefined);
    for (let i = m.index; i < m.index + m[0].length; i++) cls[i] = names[gi] || '';
  }
  return cls;
}

function renderLine(line, mode, marks) {
  // marks: { cur: col|null, region: [a,b]|null, diag: [{col,len}], match: [a,b]|null }
  const cls = classesFor(line, mode);
  const text = line + ' ';  // trailing cell so the cursor can sit at end of line
  const attrs = cls.concat(['']);
  for (let i = 0; i < text.length; i++) {
    let a = attrs[i];
    if (marks.region && i >= marks.region[0] && i < marks.region[1]) a += ' region';
    if (marks.match && i >= marks.match[0] && i < marks.match[1]) a += ' match';
    for (const d of marks.diag || []) if (i >= d.col && i < d.col + d.len) a += ' diag';
    if (marks.cur === i) a += ' cur';
    attrs[i] = a.trim();
  }
  let out = '', run = '', runCls = null;
  for (let i = 0; i < text.length; i++) {
    if (attrs[i] !== runCls) { if (run) out += runCls ? '<span class="' + runCls + '">' + esc(run) + '</span>' : esc(run); run = ''; runCls = attrs[i]; }
    run += text[i];
  }
  if (run) out += runCls ? '<span class="' + runCls + '">' + esc(run) + '</span>' : esc(run);
  return out;
}

function renderScreen(eng, root, opts) {
  opts = opts || {}; const st = eng.st; const rows = opts.rows || 30;
  const wk = eng.whichKeyEntries();
  const wkRows = wk ? Math.min(6, Math.ceil(wk.entries.length / 3) + 1) : 0;
  const avail = rows - 1 - wkRows;
  const n = st.windows.length;
  const per = (opts.layout === 'side') ? avail - 1 : Math.floor((avail - n) / n);
  const side = opts.layout === 'side';
  let html = '<div class="wins' + (side ? ' side' : '') + '">';
  st.windows.forEach((w, wi) => {
    const b = st.buffers[w.buf]; const selected = wi === st.sel;
    const lo = b.narrow ? b.narrow[0] : 0, hi = b.narrow ? b.narrow[1] : b.text.length;
    const vis = b.text.slice(lo, hi); const lines = vis.split('\n');
    const [cl, cc] = eng.lineOf(b, b.point); const [nl0] = eng.lineOf(b, lo);
    const curLine = cl - nl0;
    if (curLine < b.scroll) b.scroll = curLine; if (curLine >= b.scroll + per) b.scroll = curLine - per + 1;
    if (b.scroll < 0) b.scroll = 0;
    let region = null, matchL = null;
    if (b.mark != null && selected) { const a = Math.min(b.mark, b.point), z = Math.max(b.mark, b.point); region = [a, z]; }
    if (st.mini && st.mini.kind === 'isearch' && st.mini.match && selected) matchL = st.mini.match;
    let body = '';
    for (let i = 0; i < per; i++) {
      const li = b.scroll + i; const line = li < lines.length ? lines[li] : null;
      if (line == null) { body += '<div class="ln empty">~</div>'; continue; }
      let off = lo; for (let k = 0; k < li; k++) off += lines[k].length + 1;
      const marks = { cur: selected && li === curLine && !(st.mini && st.mini.kind !== 'isearch') ? cc : null, diag: b.diags.filter(d => d.line === li + nl0) };
      if (region && region[1] > off && region[0] < off + line.length + 1) marks.region = [Math.max(0, region[0] - off), Math.min(line.length + 1, region[1] - off)];
      if (matchL && matchL[1] > off && matchL[0] <= off + line.length) marks.match = [Math.max(0, matchL[0] - off), Math.min(line.length + 1, matchL[1] - off)];
      body += '<div class="ln">' + renderLine(line, b.mode, marks) + '</div>';
    }
    const pct = lines.length <= per ? 'All' : b.scroll === 0 ? 'Top' : b.scroll + per >= lines.length ? 'Bot' : Math.round(100 * b.scroll / lines.length) + '%';
    const flag = b.readonly ? (b.modified ? '%*' : '%%') : (b.modified ? '**' : '--');
    const extras = (b.narrow ? ' Narrow' : '') + (b.overwrite ? ' Ovwrt' : '') + (st.macro ? ' Def' : '');
    const ml = ' -:' + flag + '-  ' + b.name.padEnd(16) + ' L' + (cl + 1) + ' C' + cc + '  ' + pct.padEnd(4) + '  (' + b.modeName + (b.minors ? ' ' + b.minors : '') + ')' + extras + (st.recursive ? ' [[ ]]' : '');
    if (b.mode === 'pdf-view-mode' && opts.pdfHtml && side) body = '<div class="pdfpage">' + opts.pdfHtml + '</div>';
    html += '<div class="win' + (selected ? ' on' : '') + '"><div class="body">' + body + '</div><div class="ml">' + esc(ml) + '</div></div>';
  });
  html += '</div>';
  if (wk) {
    const cols = 3; const items = wk.entries.map(([k, c]) => '<span class="wk-k">' + esc(k) + '</span> <span class="wk-c">' + esc(c) + '</span>');
    html += '<div class="whichkey" style="--rows:' + wkRows + '"><div class="wk-title">' + esc(wk.prefix) + '-</div><div class="wk-grid" style="--cols:' + cols + '">' + items.join('') + '</div></div>';
  }
  let mini;
  if (st.mini) mini = '<span class="prompt">' + esc(st.mini.prompt) + '</span>' + esc(st.mini.input) + '<span class="cur"> </span>' + (st.mini.fail ? ' <span class="err">[Failing]</span>' : '');
  else mini = esc(st.echo || '');
  html += '<div class="mini">' + mini + '</div>';
  if (st.overlay === 'closed') html += '<div class="overlay"><div class="overlay-card"><strong>The Emacs frame closed.</strong><span>C-x C-c ran save-buffers-kill-terminal. The playground is reopening your frame; buffers are intact because Emacs runs as a daemon. On your own machine this would quit Emacs.</span></div></div>';
  if (st.overlay === 'suspended') html += '<div class="overlay"><div class="overlay-card"><strong>Emacs is suspended.</strong><span>C-z sent Emacs to the background of the shell. Resuming it for you. (In a real terminal you would type fg.)</span></div></div>';
  root.innerHTML = html;
}

function renderTicker(eng, root, max) {
  const st = eng.st; const items = st.ticker.slice(-(max || 6)).reverse();   // newest first, on the left
  if (!items.length) { root.innerHTML = '<div class="tk-empty">Press a key in the Emacs pane. Every chord shows up here with the command it ran.</div>'; return; }
  root.innerHTML = items.map((e, i) => {
    const last = i === 0;
    const caps = e.keys.map(k => '<kbd>' + esc(k) + '</kbd>').join('');
    let label, gloss;
    if (e.kind === 'prefix') { label = 'waiting for the next key'; gloss = 'This is a prefix: it does nothing alone.'; }
    else { label = esc(e.cmd); gloss = esc(e.gloss || ''); }
    return '<div class="tk ' + e.kind + (last ? ' last' : '') + '"><div class="tk-keys">' + caps + (e.mx ? '<span class="tk-mx">typed name</span>' : '') + '</div><div class="tk-cmd">' + label + '</div><div class="tk-gloss">' + gloss + '</div></div>';
  }).join('');
}

function renderWhere(eng, root) {
  const w = eng.whereAmI(); const b = w.buffer;
  const rows = [
    ['Buffer', esc(b.name) + (b.modified ? ' <em>(unsaved changes)</em>' : '')],
    ['Mode', esc(b.modeName) + (b.minors ? ' <span class="dim">+ ' + esc(b.minors) + '</span>' : '')],
    ['Windows', w.windows === 1 ? 'one' : w.windows + ' (cursor is in the highlighted one)'],
    ['Prompt', w.mini ? esc(w.mini.prompt.trim()) + ' <em>open</em>' : 'none'],
  ];
  let html = '<dl class="where">' + rows.map(([k, v]) => '<dt>' + k + '</dt><dd>' + v + '</dd>').join('') + '</dl>';
  if (w.flags.length) html += '<div class="flags">' + w.flags.map(f => '<span class="flag">' + esc(f) + '</span>').join('') + '</div>';
  if (w.exits.length) html += '<div class="exits">' + w.exits.map(([why, how]) => '<div class="exit"><div class="why">' + esc(why) + '</div><div class="how">' + keyify(how) + '</div></div>').join('') + '</div>';
  else html += '<div class="calm">Nothing unusual is going on. You are editing normally.</div>';
  root.innerHTML = html;
}

/* Wrap key names like C-x 1, RET, q, F4 in <kbd>. A sequence may only continue with
   key-like tokens (single characters, modifiers, named keys), never ordinary words. */
const KEY_TOKEN = '(?:(?:C-|M-|S-)+(?:<[a-z0-9]+>|RET|TAB|DEL|ESC|SPC|[^\\s.,;])|RET|TAB|DEL|ESC|SPC|F[0-9]{1,2}|Insert)';
const KEY_CONT = '(?:(?:C-|M-|S-)+(?:<[a-z0-9]+>|RET|TAB|DEL|ESC|SPC|[^\\s.,;])|RET|TAB|DEL|ESC|SPC|[^\\s.,;])';
const KEY_RE = new RegExp('(?<![\\w-])(' + KEY_TOKEN + '(?: ' + KEY_CONT + ')*|q)(?=[\\s.,;:)]|$)', 'g');
function keyify(s) {
  return esc(s).replace(KEY_RE, m => m.split(' ').map(k => '<kbd>' + k + '</kbd>').join(' '));
}

/* Decode a browser KeyboardEvent into an Emacs key name. */
function decodeKey(e) {
  const k = e.key;
  if (['Control', 'Alt', 'Shift', 'Meta', 'CapsLock', 'OS'].includes(k)) return null;
  let mods = '';
  if (e.ctrlKey) mods += 'C-';
  if (e.altKey || e.metaKey) mods += 'M-';
  let base;
  const named = { Enter: 'RET', Escape: 'ESC', Backspace: 'DEL', Tab: 'TAB', ' ': 'SPC', Insert: '<insert>', ArrowLeft: '<left>', ArrowRight: '<right>', ArrowUp: '<up>', ArrowDown: '<down>', Home: '<home>', End: '<end>', Delete: '<deletechar>' };
  if (named[k]) base = named[k];
  else if (/^F\d{1,2}$/.test(k)) base = '<' + k.toLowerCase() + '>';
  else if (k.length === 1) base = (e.ctrlKey || e.altKey || e.metaKey) ? (k === '_' ? '_' : k.toLowerCase()) : k;
  else return null;
  if (mods === 'C-' && base === 'SPC') return 'C-SPC';
  if (!mods && base.length === 1) return base;
  return mods + base;
}

function attachKeyboard(eng, el, onBrowserKey) {
  el.tabIndex = 0;
  el.addEventListener('keydown', e => {
    const key = decodeKey(e); if (!key) return;
    e.preventDefault(); e.stopPropagation();
    if ((e.ctrlKey && ['w', 't', 'n'].includes(e.key.toLowerCase())) && onBrowserKey) onBrowserKey(key);
    eng.handleKey(key);
  });
  el.addEventListener('mousedown', () => el.focus());
}

const MockEmacs = { createEngine, renderScreen, renderTicker, renderWhere, attachKeyboard, decodeKey, keyify, esc, HELLO_FILES, POLY_FILES, GLOSS, BINDINGS };
if (typeof module !== 'undefined' && module.exports) module.exports = MockEmacs; else global.MockEmacs = MockEmacs;
})(typeof window !== 'undefined' ? window : globalThis);
