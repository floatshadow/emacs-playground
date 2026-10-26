# Why the keys look like this

Notes for the window lessons: long enough to scroll, so follow-mode has
something to do and olivetti has a page to centre.

## Mnemonics, mostly

Most of the motion keys are the first letter of what they do. `C-f`
moves forward a character and `C-b` back; `C-n` goes to the next line
and `C-p` to the previous one. `C-a` is the start of the line because
`a` is the start of the alphabet, and `C-e` is its end.

The Meta versions do the same thing to a bigger unit: `M-f` and `M-b`
move by words, `M-a` and `M-e` by sentences. Add Control and Meta
together and the unit becomes a balanced expression: `C-M-f` jumps over
a bracketed block, `C-M-a` goes to the start of a function.

Once you see the pattern, a key you have never pressed is often a good
guess. That is the point of learning the pattern rather than the keys.

## Meta was a real key

The keyboards Emacs grew up on had a key labelled Meta. Today Alt plays
its part, and when a terminal or a browser swallows Alt, pressing Esc
and then the key does the same: `Esc f` is `M-f`.

## Kill and yank are older than cut and paste

When the commands were named, there was no clipboard to borrow words
from. Text you remove is killed and goes to the kill ring; text you
bring back is yanked. The ring remembers many kills, which is why
`M-y` after `C-y` can reach older ones.

The playground connects the kill ring to your browser's clipboard, so
the old words and the new habits meet in the middle.

## Windows are not what your desktop calls windows

Emacs used the word window for a pane of the screen before desktops
used it for something else. What your desktop calls a window, Emacs
calls a frame. A frame holds windows, a window shows a buffer, and a
buffer holds text, often the contents of a file.

Keep those four words apart and every window command reads plainly:
`C-x 2` splits a window, `C-x 0` deletes a window (the buffer stays),
`C-x k` kills a buffer (the file stays), `C-x 5 2` makes a new frame.

## Prefix keys

`C-x` and `C-c` start longer key sequences. `C-x` holds the commands
everyone uses: files, buffers, windows. `C-c` followed by a plain letter
is reserved for you, the user, and `C-c` followed by a control key
belongs to the major mode of the buffer you are in.

`C-h` is the help prefix. After any prefix, pressing `C-h` lists what
can follow it, and the which-key popup does the same without asking.

## The bottom of the screen

The line at the very bottom has two jobs. When Emacs has something to
tell you, it is the echo area. When Emacs needs something from you, it
becomes the minibuffer: a tiny buffer with a prompt, where every
editing key still works.

Above it, each window has a mode line: the buffer's name, whether it is
modified, where the cursor is, and which modes are on.

## Reading long files in two columns

follow-mode makes two windows on the same buffer behave like the
columns of a newspaper: the right window starts where the left one
ends, and scrolling either one scrolls both. On a wide screen this
shows twice as much of a long file at once.

## Centred text

olivetti-mode narrows the text to a comfortable width in the middle of
the window and leaves the margins empty. It changes nothing in the
file; it only changes where the lines are drawn. Turn it off and the
text fills the window again.

## Zoom

In the graphical frame, `C-x C-+` makes the text in one buffer larger
and `C-x C--` smaller; `C-x C-0` resets it. `C-x C-M-+` zooms every
buffer at once. A terminal frame cannot change its font size, so these
keys only matter in the GUI frame.

## Repeating

`C-x z` repeats the last command, and each further `z` repeats it
again. Many commands also have a repeat map: after `C-x o`, a plain `o`
moves to the next window again, and after `C-x }`, a plain `}` widens
the window again.

## The end

If both windows show this line at the same time, follow-mode is off.
