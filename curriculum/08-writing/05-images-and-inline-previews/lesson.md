---
title: Images, SVG and inline previews
summary: Look at photos and SVG drawings in Emacs, browse a folder of images as thumbnails, and show images and formulas inline in an Org file.
minutes: 10
---

The graphical frame draws images as well as text. A JPEG, PNG or SVG file opens as a picture you can zoom and rotate, a folder of images becomes a page of thumbnails, and an Org file can show its linked images and its formulas in place, between the lines of text.

:::vscode
Opening an image in the editor is the same here: `C-x C-f` on the file. Zoom is `i +` and `i -` instead of Ctrl and the mouse wheel (which also works), and VS Code's SVG preview extension is built in: `C-c C-c` switches between the drawing and its XML. Markdown's inline image preview is what Org does with `C-c C-x C-v`.
:::

:::gui-only
This lesson runs in the graphical frame: a terminal cannot draw pictures.
:::

## One image

An image file opens in image-mode. The keys that change the picture start with `i`: `i +` and `i -` zoom, `i r` rotates by 90 degrees, `i h` flips it. `n` and `p` show the next and previous image of the same folder. Nothing changes on disk unless you save with `i o`.

An SVG file is text, an XML description of the drawing. Emacs renders it, and :try[C-c C-c] toggles between the picture and the source, which you can edit like any XML file and toggle back to see the result.

## A folder of images

`M-x image-dired` asks for a folder and shows a thumbnail of every image in it, in one window, with the folder's listing in another. Arrow keys or `C-f` and `C-b` move between thumbnails, and `RET` shows the selected one full size.

## Images and formulas in Org

Org is built into Emacs; Part 9 covers it in depth. Here it only needs to show what its links point to. With the cursor in a section, :try[C-c C-x C-v] shows the section's image links as the images themselves, and :try[C-c C-x C-l] renders its LaTeX formulas, such as `$t(n) = a + b\,n$`, as typeset images. The same keys hide them again. With `C-u C-u` first, either one works on the whole file.

The files are in `/work/writing`: `figures/` holds a photo and a sketch, `plot.svg` is a chart, and `gallery.org` links to both.

::steps
