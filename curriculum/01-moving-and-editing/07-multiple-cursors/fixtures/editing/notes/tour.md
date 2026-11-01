# A field guide to moving around

This file is long on purpose. It is here so that the page keys have
something to page through: one screen of text is not enough to feel
what `C-v` and `M-v` do. Read it, or just watch it go by.

## Screens, not lines

A file in Emacs is shown through a window, and the window is smaller
than most files. Moving the cursor one line at a time is fine for the
next few lines, but a whole chapter away takes too many presses.

`C-v` moves the view down by one screen, less two lines. The two lines
that stay are the last two of the old screen, now at the top of the new
one, so your eyes keep their place. `M-v` moves the view back up by the
same amount. The cursor goes along and keeps its place on the screen,
so after a page down it is on the same row of the window as before.

In Emacs's own words, `C-v` runs `scroll-up-command`, which sounds
backwards. The name describes the text, not the view: the text moves
up, and you see what was below. `M-v` runs `scroll-down-command`. The
key ticker shows both names as you press them.

## Where the page keys come from

Control is the small step and Meta the bigger one for most motion keys,
but the page keys break that pattern: both move by a screen, one in
each direction. `v` was free when the keys were chosen, and it stayed.

If Alt does not reach Emacs (some browsers keep Alt+v for a menu),
press Escape and then `v`. Escape followed by a key is always the same
as Meta with that key, so `ESC v` pages up wherever `M-v` would.

## The end of the buffer

Page down often enough and you reach the last screen. The next `C-v`
moves the cursor to the very end of the text. One more, and Emacs says
"End of buffer" in the echo area. That is not an error in the sense of
something broken: there is simply nothing more to show. `M-v` at the
top says "Beginning of buffer" for the same reason.

`M->` jumps straight to the end and `M-<` straight to the top. They
are the fastest way to the edges, and they leave a mark behind, so
`C-u C-SPC` brings you back to where you were before the jump.

## Keeping your place

When you page, the cursor's line changes. Sometimes you want to look
further down without losing the line you are editing. Two habits help.

The first is `C-l`. It scrolls so the cursor's line is in the middle of
the window; press it again and the line goes to the top, again and it
goes to the bottom. It moves the view, never the cursor.

The second is the other window. `C-M-v` scrolls the other window of the
frame, if there is one, while the cursor stays where it is. With a
help buffer or a compile log beside your file, `C-M-v` reads it without
leaving your code. With a minus count, `M-- C-M-v`, it scrolls back up.

## Counting

Every motion key takes a count. `C-u 3 C-v` would scroll by three lines
rather than three screens: for the page keys, the count is a number of
lines. That makes `C-u 5 C-v` a gentle nudge, useful when the line you
want is just below the edge of the window.

A count before `C-n` is the more common case: `C-u 10 C-n` moves ten
lines down. `M-5 C-n` does the same, with the count typed while you
hold Meta. Either way, the count comes first and the motion second.

## Lines by number

When an error message names a line, `M-g g` asks for its number and
takes you there. The mode line shows the current line as `L` followed
by a number, so you can always check where you are.

Line numbers in the margin are a matter of taste. Code buffers in this
course show them; prose buffers do not. `M-x display-line-numbers-mode`
switches them for the buffer you are in.

## Words, sentences and paragraphs

Between the single character and the whole screen there are steps of
every size. `M-f` and `M-b` move by words. `M-a` and `M-e` move by
sentences, which in prose means a full stop followed by a space.
`M-{` and `M-}` move by paragraphs: a blank line separates them.

In this file, try `M-}` a few times. Each press lands on the blank line
after a paragraph. It is the prose version of a page key: smaller than
a screen, larger than a line, and it always stops somewhere that makes
sense to the text.

## Functions and blocks

In code, the natural units are expressions and functions. `C-M-f` and
`C-M-b` move over a balanced expression: a word, a string, or a block
from its opening bracket to the matching closing one. `C-M-a` and
`C-M-e` move to the start and end of the function the cursor is in.

These keys work in every language Emacs knows, because each language's
mode tells Emacs what its brackets and functions look like. In a
Markdown file like this one, a function is not a thing, so `C-M-a`
moves to the start of the buffer or of the current section instead.

## Searching is moving

The fastest way to a place you can describe is to search for it.
`C-s` starts a search forward: type a few letters and the cursor jumps
to the first match as you type. `C-s` again goes to the next match, and
`RET` stops the search with the cursor where it is. `C-r` searches
backward. Searching is covered properly in lesson 1.4; for now it is
enough to know that it is also a motion.

## Marks and the way back

Many jumps leave a mark at the place you left: `M-<`, `M->`, a search
that ends away from where it started. `C-u C-SPC` pops back to the last
mark, and pressing it again goes further back. It is the closest thing
Emacs has to a browser's Back button, and it works in every buffer.

## The page keys in other places

The page keys work in every buffer, not just in files. A help page, a
list of buffers, a directory listing or a long compile log all scroll
with `C-v` and `M-v`. In buffers that are read-only, `SPC` and `DEL`
often page down and up as well, the way they do in a pager such as
`less`. A help buffer is one of them: press `SPC` there to read on.

The minibuffer is the exception. When Emacs asks for a file or a
command, `C-v` in the minibuffer may scroll the completion list, or
the list may scroll on its own as you move through it. Each completion
style chooses.

## Scrolling a line at a time

Sometimes a screen is too much. Emacs has no default key that scrolls
by exactly one line, but the count trick from above works: `C-u 1 C-v`
moves the view one line down. The mouse wheel, when you have one,
scrolls a few lines per notch. In a terminal Emacs, as in this course,
the wheel may scroll the terminal instead of Emacs; the keys always
reach Emacs.

## Why the cursor moves

Emacs keeps the cursor inside the window. When the view moves away from
the cursor's line, the cursor has to move with it, or it would be off
screen. Many editors let the cursor stay behind, out of sight; Emacs
does not. If you want to come back to the line you were editing, set a
mark first with `C-SPC C-SPC`, page around, and `C-u C-SPC` takes you
back.

## A short drill

Here is a drill you can repeat in any long buffer until the keys are
in your fingers rather than in your head:

1. `M-<` to go to the top.
2. `C-v` three times, reading the first line of each screen.
3. `M-v` once, and see the screen you just left come back.
4. `C-l` three times, and watch the cursor line visit the middle, the
   top and the bottom of the window.
5. `M->` to go to the end, and `C-u C-SPC` to come back.

Do it once slowly, then once without looking at the keyboard.

## What to remember

There are only a few keys to remember for moving by screens:

- `C-v` pages down and `M-v` pages up.
- `ESC v` is `M-v` when Alt is not available.
- `M-<` and `M->` go to the edges.
- `C-l` moves the view around the cursor.
- `C-M-v` scrolls the other window.
- "End of buffer" means there is nothing more below.

Everything else in this guide is a variation on them: a count, another
window, or a different unit of text.

## Notes for later parts

Part 2 explains buffers, and why `C-x b` switched to this file even
though no new file was opened: it was already open, waiting in a buffer
of its own. Part 3 shows how to put two windows side by side, which is
where `C-M-v` becomes useful. Part 10 comes back to moving around, with
keys that jump to any visible word in two or three presses.

Until then, the keys in this guide are enough to move anywhere in any
file without touching the mouse or the arrow keys.

## An afterword on habits

The arrow keys and the Page Down key work in Emacs too. Nothing stops
you from using them. The reason to learn `C-v` and its friends is not
that they are better keys, but that they sit under your fingers while
you type, and that they combine: a count, a prefix, another window.
Once the grid of motion keys is familiar, guessing a key you have not
been taught becomes possible, and that is when Emacs starts to feel
fast.

It takes about a week of daily use for the keys to stop needing
thought. The ticker at the top of the page helps: every command it
shows is a name you can look up with `C-h f`, and every key you press
is a key you can look up with `C-h k`.

## One more screen

This last section is here so that the final `C-v` has somewhere to go.
If you have paged this far, you have seen the page keys at work on a
file that is longer than the window, which is what this file is for.

Go back to the top with `M-<`, or page back up with `M-v`, as many
times as you like. The lesson checks only that you paged down and back
up, and that the window no longer starts at the first line.

This is the end of the field guide.
