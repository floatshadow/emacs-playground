---
title: Resizing and balancing
summary: Make a window taller, wider, fit its text or share the space evenly, and rotate, flip or transpose a whole layout.
minutes: 10
---

New windows split the space in half, which is rarely what you want for long. A build log needs a few lines; the code above it needs the rest. Emacs resizes from the keyboard in small steps, fits a window to its contents, and in Emacs 31 can turn a whole layout around.

:::vscode
Dragging a border works here too, even in the terminal. Maximise Editor Group is `C-x 1`, or `maximize-window` to keep the others visible but small. Reset Editor Group Sizes is `C-x +`. Toggle Vertical/Horizontal Layout is `C-x w r` and the arrows, and it works for any number of windows.
:::

## Taller, wider, smaller

:try[C-x ^] makes the selected window one line taller; :try[C-x }] makes it one column wider and :try[C-x {] one narrower. Repeat-mode applies: after the first chord, the last key alone (`^`, `}`, `{`) repeats it, so `C-x } } } }` widens by four. A count works too, `C-u 5 C-x ^`, and a negative count shrinks.

:try[C-x -] shrinks a window to fit its text, which is just right for short output such as a build log. `M-x fit-window-to-buffer` grows it to fit as well. :try[C-x +] makes every window the same size again.

## Turning the layout

Emacs 31 adds commands that change the whole layout at once, under `C-x w`. :try[C-x w r] then `<right>` rotates it clockwise: windows stacked above each other end up side by side. `C-x w t` transposes rows and columns, `C-x w f` then an arrow flips it, and `C-x w o` then an arrow moves every buffer to the next window while the shape stays. Each keeps the windows' buffers and cursors.

:::aside{title="The mouse"}
In the terminal frame the mouse works: drag a mode line up or down, or the line between two side-by-side windows, to resize them.
:::

::steps
