---
title: What the screen is telling you
summary: Name the parts of the Emacs screen and read the mode line, so you always know where you are and what Emacs is waiting for.
minutes: 10
---

Everything Emacs knows about your situation is already on the screen, in a few compact lines. This lesson gives each part its name, so the rest of the course can point at it, and so that a screen you do not understand becomes a screen you can read.

:::vscode
The bar under each window, the **mode line**, is Emacs's status bar, but there is one per window instead of one per application. The last line of the screen does two jobs that VS Code spreads over notifications and the command palette: Emacs tells you things there, and asks you things there. Editor groups are :jargon[windows], and what a tab shows is a :jargon[buffer].
:::

## Frame, windows, buffers

The whole terminal is one :jargon[frame]. Right now it is split into two :jargon[windows]: `main.rs` on top and `README.md` below. A window is only a view; the text lives in a buffer, and a buffer usually holds the contents of a file. Two windows can show the same buffer, and closing a window never closes the buffer it showed.

One window is *selected*: it has the cursor, and its mode line is drawn brighter than the others. Clicking in a window selects it, and so does :try[C-x o], which moves to the next window. Try it twice and watch the bright mode line move.

## Reading a mode line

Read it from the left. The first characters say whether the buffer has changed since it was last saved: `-:---` means unchanged, `-:**-` means :jargon[modified], and `%%` means :jargon[read-only]. Then comes the buffer's name, then where you are: `Top`, `Bot`, `All` or a percentage, and the line and column, such as `L12 C4`. The words in parentheses are the modes. `(Rust)` is the :jargon[major mode], which decides how this buffer behaves; later you will see `(Rust Eglot)`, where Eglot is a :jargon[minor mode] that adds a language server on top.

:::aside{title="Saving in the playground"}
The playground normally saves your files a few seconds after you stop typing, so nothing is lost if a frame closes. This lesson switches that off, so the `**` stays visible until you save yourself.
:::

## The bottom line

When Emacs has something to tell you, the last line is the :jargon[echo area]: *Wrote /work/hello/README.md* after a save, *Quit* after `C-g`. When Emacs needs something from you, the same line becomes the :jargon[minibuffer], a one-line buffer with a prompt where every editing key works. Press :try[M-x] to see it turn into a prompt, and `C-g` to put it away.

Two more strips belong to the playground rather than to Emacs. Under the screen, the :jargon[ticker] names every command your keys ran. On the right, *Where am I* reads the same mode line and minibuffer you just learned and says, in one sentence, what state you are in and how to leave it.

The five steps below are questions about the screen. Each one is answered by doing something, and the step list checks the answer against the live Emacs.

::steps
