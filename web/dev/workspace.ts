/**
 * Fixture files for the fake session's workspace (from design/demo/mock-emacs.js):
 * the hello Rust program (lesson 2.3) and the polyglot project (P6).
 * Paths are relative to /work.
 */

export const RUST_HELLO = `use std::env;

fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    println!("{}", greet(&name));
}
`

export const HELLO: Record<string, string> = {
  'hello/src/main.rs': RUST_HELLO,
  'hello/src/lib.rs': 'pub fn shout(s: &str) -> String {\n    s.to_uppercase()\n}\n',
  'hello/Cargo.toml':
    '[package]\nname = "hello"\nversion = "0.1.0"\nedition = "2024"\n\n[dependencies]\n',
  'hello/README.md':
    '# hello\n\nA tiny Rust program used by lesson 2.3.\n\n- `cargo run` prints a greeting\n- `cargo run -- Ada` greets Ada\n',
}

export const RUST_POLY = `use polyglot::greet;
use std::env;

/// Entry point for the \`polyglot\` service CLI.
fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    // Rust borrows: greet() wants &str, we hold a String.
    //   fix: pass a reference ->  greet(&name)
    println!("{}", greet(name));
}
`

export const REPORT_TYP =
  '#set page(paper: "a5", margin: 1.6cm)\n#set text(font: "Libertinus Serif", 10.5pt)\n\n= Polyglot: a tiny service, measured\n\nWe greet in Rust, score in Python and\nprove termination in OCaml.\n\n== Latency\n\n#table(\n  columns: 3,\n  [lang], [p50], [p99],\n  [Rust], [0.4 ms], [1.1 ms],\n  [Python], [3.2 ms], [9.8 ms],\n)\n\n$ E = sum_(i=1)^n x_i / n $\n'

export const POLY: Record<string, string> = {
  'polyglot/Cargo.toml': '[package]\nname = "polyglot"\nversion = "0.1.0"\nedition = "2024"\n',
  'polyglot/README.md':
    '# polyglot\n\nThree languages, one build. `cargo run` greets, `uv run tools/score.py` scores, `dune build proof` checks the proof.\n\n## Layout\n\n- `src/` Rust CLI\n- `tools/` Python scoring\n- `proof/` OCaml\n- `docs/` Typst report\n',
  'polyglot/src/main.rs': RUST_POLY,
  'polyglot/src/lib.rs': 'pub fn greet(name: &str) -> String {\n    format!("Hello, {name}!")\n}\n',
  'polyglot/tools/score.py':
    'import json, sys\n\ndef score(rows: list[dict]) -> float:\n    return sum(r["p99"] for r in rows) / len(rows)\n\nif __name__ == "__main__":\n    print(score(json.load(sys.stdin)))\n',
  'polyglot/proof/term.ml':
    'let rec fib n = if n < 2 then n else fib (n - 1) + fib (n - 2)\n\nlet () = Printf.printf "%d\\n" (fib 10)\n',
  'polyglot/proof/dune': '(executable (name term))\n',
  'polyglot/docs/report.typ': REPORT_TYP,
  'polyglot/site/index.html':
    '<!doctype html><html><head><meta charset="utf-8"><title>Polyglot docs</title><style>body{font:15px/1.5 Georgia,serif;max-width:40em;margin:2em auto;padding:0 1em;color:#1e1b2e}code{background:#f0eef5;padding:1px 4px;border-radius:3px}</style></head><body><h1>Polyglot docs</h1><p>Rendered from <code>site/</code> by the static docs task. In the playground this tab is a sandboxed iframe over the workspace, reloaded on file change.</p><p id="t"></p><script>document.getElementById("t").textContent="Scripts run inside the sandbox: "+new Date().toLocaleTimeString()</script></body></html>\n',
}

export const COMPILE_FAIL = `-*- mode: compilation; default-directory: "/work/polyglot/" -*-
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
`

export const COMPILE_OK = `-*- mode: compilation; default-directory: "/work/polyglot/" -*-
Compilation started at Mon Oct 26 18:04:40

cargo build
   Compiling polyglot v0.1.0 (/work/polyglot)
    Finished \`dev\` profile [unoptimized + debuginfo] target(s) in 0.61s

Compilation finished at Mon Oct 26 18:04:41
`

/** Text lines of report.typ for the fake PDF (headings, paragraphs, table rows). */
export function typstLines(src: string): { text: string; size: number }[] {
  const out: { text: string; size: number }[] = []
  let inTable = false
  for (const raw of src.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#set')) continue
    if (line.startsWith('#table(')) {
      inTable = true
      continue
    }
    if (inTable) {
      if (line.startsWith(')')) inTable = false
      else if (line.startsWith('[')) {
        const cells = [...line.matchAll(/\[([^\]]*)\]/g)].map((m) => m[1] ?? '')
        out.push({ text: cells.join('    '), size: 10 })
      }
      continue
    }
    if (line.startsWith('== ')) out.push({ text: line.slice(3), size: 13 })
    else if (line.startsWith('= ')) out.push({ text: line.slice(2), size: 17 })
    else if (line.startsWith('$')) out.push({ text: line.replace(/\$/g, '').trim(), size: 10 })
    else out.push({ text: line, size: 10.5 })
  }
  return out
}
