---
title: Workspaces with tab-bar
summary: Keep separate window layouts in tabs, one per project, and switch between them without losing either.
minutes: 12
---

Tabs in Emacs are not files. A :jargon[tab] holds a whole window layout: switch tabs and the frame shows a different arrangement of windows, each with its buffers and cursors. That makes tabs workspaces: one for this project, one for that, one for reading the manual, and switching never disturbs the others.

:::vscode
A tab is closest to a separate VS Code window per folder, all inside one frame. Every tab can show every buffer, because buffers belong to Emacs, not to the tab. If you want per-window tabs of open files, the editor tabs you know, turn on `tab-line-mode`.
:::

## Tab keys

All tab commands are under `C-x t`. :try[C-x t 2] opens a new tab, :try[C-x t r] renames the current one, :try[C-x t o] goes to the next and :try[C-x t RET] to one by name, with completion. `C-x t 0` closes the current tab; its buffers stay open. `C-x t u` reopens the last tab you closed, layout included. The tab bar appears at the top of the frame as soon as there are two tabs.

## One tab per project

The playground's windows module changes :try[C-x p p] so that switching project opens it in a tab named after the project, or switches to that tab if it exists. Each project keeps its own layout: code and tests on one side, a report and its preview on the other. `C-x t p` works like `C-x 4 4` for tabs: whatever command comes next shows its buffer in a new tab.

## Per-window tabs

`M-x tab-line-mode` adds a line of tabs above each window, listing the buffers that window has shown. It is the closest thing to VS Code's editor tabs, and it combines with tab-bar: workspaces at the top, open files above each window.

::steps
