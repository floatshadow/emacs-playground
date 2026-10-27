> **Status: historical record.** This is the newcomer usability review of 2026-10-27, kept as written.
> Every numbered item (B1..B18, R1..R10, the stuck moments and smaller things) was triaged in
> [2026-10-27-fix-plan.md](2026-10-27-fix-plan.md) and resolved; the remaining known limitations are in `docs/notes.md` §6.
> Current behaviour is documented in the spec docs (`docs/api.md`, `backend.md`, `emacs.md`, `frontend.md`, `CURRICULUM.md`) and `docs/notes.md`.

# Emacs Playground: a newcomer's evening with it (2026-10-27)

Reviewer profile: daily VS Code user, never used Emacs. Clean-room test: I only used the web app at
http://127.0.0.1:8080 ("local mode", terminal frame), driving a headless Chrome at 1600x1000 and
looking at screenshots. I did not read source, docs or git history. All screenshot names below are
under `artifacts/ux-review/`.

Lessons attempted: 0.1, 0.2, 0.3, 0.4, 1.1, 1.2, 1.4, 2.3, 2.4, 3.1, 3.5, plus the course map, the
Session page, Focus mode, the GUI switch, "Rescue me", "Reset this step", "I'm stuck", and the
viewport at 1280x800 and 1024x768.

A note on timings: my keystrokes were scripted, so a lesson's raw key time was a minute or two.
The per-lesson times below are my honest estimate of what a human learner would spend, reading
included; the *check latency* numbers (how fast a step turned green after I did the thing) are
measured.

---

## First impressions

**What I landed on.** The root URL is the course map (`01-landing.png`): a serif headline "From the
first keystroke to your own config", a short blurb, four legend chips (`terminal`, `gui`, `remote`,
`project`) and then fifteen parts listed as rows with a VS Code equivalent under each lesson
("in VS Code: status bar, Problems"). The purpose is obvious within five seconds. The "in VS Code:"
sub-lines are the single best orientation device on the page; I immediately knew which lessons map to
things I already do.

**What was wrong on arrival.** The map already said most lessons were `done`, with a purple
"you are here · 12 min" box on 0.2 and a big "Continue: 0.2 C-g and the three ways to get lost"
button, before I had pressed a single key. For a first-time visitor that is confusing and slightly
worrying ("did I break something? is this someone else's account?"). I looked for a "start over" or
"reset progress" control on the map and on the Session page (`05-session-page.png`) and found none.
The stale state also bit me later: in several lessons steps 2..N were already ticked "Done" when I
arrived, so the step list told me I had done things I had not.

**Look and feel.** Calm, paper-like: off-white background, a serif text face for prose, a monospace
face for keys and code, one purple accent, generous line height. It reads like a well-typeset book
rather than a SaaS dashboard, which suits a course. Density is right in the prose column; the right
rail is denser. Key chords are rendered as little keycaps (`C-x` `C-f`), which makes chords scannable.
The map page is very long (7200 px) with no in-page navigation, so finding Part 3 means a lot of
scrolling, but as a first page it does its job: it tells me what the course is, how long each part is,
and that I can "start anywhere".

**Is it clear what to do first?** Mostly. The "Continue" button is the obvious call to action; the
problem is only that it pointed at 0.2 because of the stale progress. Clicking lesson 0.1 took me
straight to the three-column lesson view and Emacs was already there (the session was warm, so the
pane appeared in under 2 s; `06-lesson01-just-loaded.png`).

---

## Walkthrough notes per lesson

Layout used by every lesson (`06-lesson01-just-loaded.png`): lesson prose and the step list on the
left (380 px, scrolls independently), the Emacs terminal in the middle with a status strip above
("Emacs 31.1, terminal frame in tmux, 122×37 · modules: base editing files") and the key ticker below,
and a right rail with "Where am I", "Did you mean", "Rescue" and "Keys for this buffer". A purple pill
"Current step: ..." floats at the bottom of the left column and scrolls the prose to the active step
when clicked (`49-l04-after-pill-click.png`). Good.

### 0.1 What the screen is telling you  (about 8 min; check latency 1.5-2.2 s)
- Prose is clear and the "Coming from VS Code" box ("Editor groups are windows, and what a tab shows
  is a buffer") did real work for me.
- Q1 "Select the README.md window": I clicked in the lower window with the mouse; the step ticked
  in ~1.5 s and "Where am I" switched to README.md (`08-l01-q1-clicked-readme.png`). But the prose
  says the selected window's "mode line is drawn brighter than the others" and in the terminal theme
  the two mode lines look identical to me (compare the two bars in `08`). I could not "watch the bright
  mode line move" as instructed.
- Q2 "Make README.md modified": C-e then typing worked; the mode line went from `-:---` to `-:**-`
  and the rail said "README.md (unsaved changes)" (`09-l01-q2-typed.png`). The ticker filled up with
  four identical `self-insert-command` cards and pushed my C-e off screen.
- Q3 "Go to line 5": arrow keys were accepted (`10-l01-q3-arrows.png`), nice for a newcomer.
- Q4 "Save README.md": I pressed Ctrl+S out of habit. This was the best moment of the evening: the rail
  said "You are searching (I-search at the bottom)" and a "Did you mean" card appeared: "Coming from
  VS Code? C-s is not save. C-s starts an incremental search (I-search at the bottom). Save is C-x
  C-s. C-g leaves the search." (`11-l01-ctrl-s-vscode-habit.png`). Exactly the right help at exactly
  the right time.
- Q5 "Ask Emacs for its version": after M-x emacs-version RET the echo area first showed "You can run
  the command 'emacs-version' with M-x em-ve" (`15-l01-q5-done.png`) and only two seconds later the
  actual "GNU Emacs 31.1 (build 2, ...)" (`16-l01-steps-complete.png`). The step passed, but the
  first thing a learner reads as "Emacs's answer" is a cryptic abbreviation tip.
- Completion state is pleasant: green ticks, a "Do this step again" link per step and a green box
  "Lesson complete. Next: 0.2 ..." (`16`).
- Also: the mode line reads `(Rust wb)` but the prose only explains `(Rust)`; "wb" is never named
  (the rail calls it "Ws-butler", which is also unexplained).

### 0.2 C-g and the three ways to get lost  (about 10 min; check latency 1.5-2 s)
- The prose has inline "try it" buttons next to chords. Clicking "C-x C-f try it" and "C-g try it"
  actually sent the keys to Emacs and completed step 1 for me without touching the keyboard
  (`19-l02-tryit-cxcf.png`, `20-l02-tryit-cg.png`). Convenient, but it means a learner can pass
  steps by clicking.
- "Watch it" on step 2 ("Start a search, then cancel it") played the demo into the live Emacs with a
  banner "Playing the demo for 'Start a search, then cancel it'..." and a Stop button
  (`21-l02-watchit-0.png`). It finished in under 3 seconds and **marked the step done**
  (`21-l02-watchit-1.png`). I never practised cancelling a search. Watching should not count as doing.
- Step 3 ("Open the buffer list, then close it"): the text says "C-x C-b lists every buffer in a
  second window ... C-x 1 keeps only the window you are in". In my session C-x C-b opened ibuffer in
  the *same* window (`25-l02-cxcb-bufferlist.png`) because the `files` module was already on (stale
  session), so C-x 1 did nothing ("No other windows to delete", `26-l02-cx1.png`). To its credit the
  UI noticed and showed a hint by itself: "If the cursor is in the list, q closes it instead." q then
  passed the step. The parenthetical "(From Part 2 on, C-x C-b opens ibuffer ...)" is there, but a
  Part 0 learner has no reason to think they are "from Part 2 on".
- Steps 4 and 5 (help for a key, the tutorial) were smooth; "Where am I" named each state correctly
  ("C-h k is waiting for a key to explain", "This is the Emacs tutorial, an ordinary buffer you can
  type in", `28-l02-chk-prompt.png`, `30-l02-tutorial.png`).
- Step 6 ("Make a big mess, then escape with ESC ESC ESC"): after C-x 2, C-x 3, M-x the rail said
  "The frame is split into 3 windows. Three or more is often an accident." and a "Did you mean" card
  scolded me: "Three windows is usually an accident." (`34-l02-big-mess.png`), while the lesson had just
  told me to do exactly that. One ESC ESC ESC closed only the prompt; a second one was needed for the
  windows (`37-l02-esc3.png`, `38-l02-esc3-again.png`). The prose does say "one thing at a time", but
  "Esc three times" reads as a total. After it all finished, "Where am I" was stuck on "ESC is a
  prefix: Emacs is waiting for the next key." (see Bugs).

### 0.3 Asking Emacs  (about 6 min; check latency ~2 s)
- On arrival, steps 2-6 were already ticked "Done" (stale). They re-evaluated to "not yet" as each
  became current, so the ticks were lies until then.
- C-h k C-x C-s, then `s` in the Help window opened files.el.gz at `(defun save-buffer`
  (`41-l03-source-files-el.png`). The rail copy here is excellent: "files.el.gz is read-only (%% in
  the mode line), so typing does nothing. C-x C-q makes it writable, if you really mean to edit it."
- C-h f, C-h v, C-h m, C-h ? b all ticked within 2 s. For C-h ? the "the help menu was opened" check
  only turned green after I pressed b, which looked like lag.
- Every "Did you mean" and stale "ESC is a prefix" card followed me from 0.2 into this lesson.

### 0.4 Which-key and the key ticker  (about 6 min; check latency ~1.8 s)
- C-x r and waiting showed the which-key popup, paged "(1 of 2) [C-h paging/help]"
  (`50-l04-cxr-whichkey.png`). The prose does not say how to see page 2.
- Bookmark set/list, register save/jump and M-x display-line-numbers-mode all passed
  (`57-l04-line-numbers.png`). Short and satisfying. The ticker card "M-x typed name
  display-line-numbers-mode · Toggle line numbers" is a nice touch.

### 1.1 Motion  (about 10 min including my habit-testing; check latency ~1.5 s)
- The lesson file is a Rust program with "(1)...(5)" spots in comments (`58-l11-loaded.png`). Good
  exercise design: five concrete targets, "reach each with as few keys as you can".
- Before doing the steps I pressed my VS Code shortcuts (see "Stuck moments"). Two of them destroyed
  text: End deleted a character and Delete deleted backwards (see Bugs).
- "Reset this step" restored the file in ~4.5 s and the ticker showed "Reset · playground-reset-step ·
  files and layout restored from the step checkpoint" (`82-reset-step-after.png`). No confirmation
  dialog: it silently threw away my edits, which the "Never touches your files" sentence just above the
  button contradicts.
- All five spots passed quickly. The ticker labels were right and useful here ("C-M-a ·
  treesit-beginning-of-defun · Go to the start of this function").
- "Did you mean: C-b is not toggle sidebar" and "C-l is not select line" fired when the lesson itself
  told me to press C-b and C-l (`88-l11-spot5.png`).

### 1.2 Kill, yank and the kill ring  (about 7 min; check latency ~1.8 s)
- shapes.rs with a FIXME line and three functions to reorder (`89-l12-loaded.png`): a realistic edit.
- The ticker labelled C-k as "kill-region · Cut the selected region" (`90-l12-step1-killed.png`) and
  M-y as "yank · Paste the last cut text" (`94-l12-step3-My.png`), while the prose calls them "kills
  to the end of the line" and "replaces what you just yanked with the kill before it". When the ticker
  and the lesson disagree, the learner cannot tell which is right.
- Step 2 (set mark, C-n x4, C-w, M->, C-y) and step 3 (C-y then M-y) were a genuinely good exercise:
  I understood the kill ring from doing it.
- "Did you mean: C-k is not chord prefix" appeared while the lesson told me to press C-k.

### 1.4 Incremental search  (about 6 min; check latency ~1.8 s)
- Smooth. The counter "1/4 ... 4/4" and the highlighted matches (`99-l14-isearch-4of4.png`) make
  isearch click immediately. The occur list with line numbers (`104-l14-occur-list.png`) and
  M-g M-n were satisfying.
- "Did you mean: C-s is not save" fired again at the first C-s, although 0.4's prose promised cards
  show up "once each".

### 2.3 Opening files  (about 5 min; check latency ~1.8 s)
- Marked "not started" on the map; it behaved like a fresh lesson. Ctrl+P first (habit): the card
  "C-p is not command palette. C-p moves up one line. The command palette is M-x." appeared.
- C-x C-f, "ma", TAB completed to `/work/hello/src/main.rs` with no visible candidate list
  (`108-l23-findfile-tab.png`); a VS Code user expects a dropdown. The ticker called TAB
  "completion-at-point", which is not what happens in the minibuffer.
- Change Hello to Hi, save, C-x C-b and q, C-x C-f RET into Dired and q: all passed. The Dired rail
  copy ("This is Dired, a listing of a folder (you opened a directory). RET opens the file on the
  line, ^ goes up a folder, q closes the listing.") is exactly what I needed (`113-l23-dired.png`).

### 2.4 Switching, saving and killing buffers  (about 15 min, abandoned)
- Step 1 "Open four files, two of them called main.rs" never passed. I opened
  /work/hello/src/main.rs, /work/hello/src/lib.rs, /work/projects/polyglot/src/main.rs and
  ../README.md; the screen showed `main.rs<hello>` and `README.md<polyglot>`
  (`118-l24-four-files.png`, `120-l24-after-switching.png`); the check "hello's main.rs and lib.rs
  are open" stayed grey while "polyglot's main.rs and README.md are open" went green
  (`121-l24-step1-stuck.png`). Details in "Stuck moments" and "Bugs".
- The later steps were pre-ticked "Done" (stale), so after giving up I used "Do this step again" on
  step 2 to move on; it reset state in a way that made step 2 impossible too (`131-l24-after-step2.png`).

### 3.1 Windows and frames  (about 8 min, abandoned at step 2)
- The `windows` module switched on automatically ("modules: base editing files windows").
- Step 1 (C-x 2) passed. Step 2 "Edit in the lower window and watch the upper one": I did C-x o, M->,
  typed a comment; the screenshot shows exactly the requested state (`134-l31-edit-lower.png`), but
  "the lower window is selected" stayed grey. After "Reset this step" and redoing it, that check
  went green but "main.rs has the new comment" did not (`142-l31-step2-retry.png`).
- Steps 3-6 (C-x 3 + README, C-x 0, C-x 5 2 / C-x 5 0, C-x 4 0) all *worked in Emacs* and the rail
  described each layout correctly, but the step list never advanced because step 2 was stuck.
- "Three windows is usually an accident." nagged again during the instructed three-window layout.

### 3.5 Taming pop-ups  (about 12 min; check latency ~2 s)
- Step 1 (C-x p c, sh build.sh) opened the compile window at the bottom, a third high
  (`145-l35-build-output.png`); the rail said "Another window shows *compilation*." Step 2 (C-h v
  display-buffer-alist) passed.
- Step 3: I searched for "bottom" and replaced the first hit, which was in a comment, not the rule
  (`149-l35-edited-saved.png`); the check stayed grey and Help still opened at the bottom
  (`151-l35-help-right.png`). The first hint ("Edit the two values in the rule and save with C-x C-s")
  did not tell me which "bottom"; "Show the exact keys" did (`C-s "(side . bottom"`). After fixing it
  Help opened on the right (`154-l35-help-on-right.png`) and C-x w s hid it.
- The lesson is the most "real" of the ones I did: edit a config rule, evaluate it, see the effect.

---

## Stuck moments

1. **2.4 step 1 cannot be completed (hard block).** What I did: opened the four files by absolute path
   (check stayed grey); pressed "Show a hint" (it said "C-x C-f, type src/main.rs, RET. Then C-x C-f
   again: the prompt now starts in src/, so lib.rs is enough."); "Show the exact keys"; then "Watch it".
   The demo typed for 12 s, ended with the buffer `lib.rs<src>` and a prompt reading
   `{/work/hello/src/src/} /work/proje...` (`123-l24-watchit-mid.png`), and the check was still grey
   (`124-l24-watchit-end.png`). "Reset this step" left me in README.md<polyglot>, so the hint's relative
   paths then opened polyglot's files (`126-l24-redo-still-stuck.png`). "Rescue me" reported "nothing
   to undo" (`127-l24-after-rescue.png`). I finally used "I'm stuck: tell the course author", which
   opens a modal "Report that you are stuck ... sends your last few hundred keys and what Emacs
   showed ..." (`128-l24-im-stuck-clicked.png`) and confirms with a toast "Thanks. The report was
   sent." (`129-l24-stuck-report-sent.png`). That is a good escape hatch, but it is the only one that
   worked, and it does not let me continue. Was it enough? No: nothing in the UI lets a learner skip a
   broken step or mark it done manually.
2. **3.1 step 2 does not recognise the correct state.** Described above. Hint and reset did not
   unblock it. Not enough.
3. **0.2 step 3, C-x 1 "did nothing".** The auto-hint ("If the cursor is in the list, q closes it
   instead.") rescued me within a second. Enough, and well done; but the step text itself was wrong for
   my session.
4. **0.2 step 6, ESC ESC ESC "only closed the prompt".** No hint appeared; I guessed to press it again.
   Borderline.
5. **3.5 step 3, replaced the wrong "bottom".** The first hint was too vague; the second level ("exact
   keys") fixed it. Enough, but the hint ladder should name the thing to change.
6. **Damaged the lesson file with End/Delete (1.1).** "Reset this step" restored it in 4.5 s. Enough,
   though the keys should not have damaged anything (see Bugs).
7. **Focus mode did nothing visible.** I clicked it, the button got an outline and nothing else changed
   (`156-focus-mode-on.png`). The "Did you mean" card for C-w says "Browsers often grab this key; use
   Focus mode.", so I expected a mode that explains itself. No tooltip, no banner, no description.
   Not enough.

---

## Learning-experience critique

**Pacing.** Part 0 is paced well: each lesson is 4-6 short steps, each answerable in a few keys, and
each lesson builds one mental model (screen anatomy, C-g, help, which-key). Part 1 lessons are
denser but still single-topic. 2.4 and 3.1 jump in size: 2.4 step 1 alone asks for four file opens
across two projects with a path trick ("typing a / after the offered directory starts over from the
root") introduced in the same breath. That step would be better as two.

**Clarity of prose.** The writing is unusually good for a tutorial: concrete, short sentences, no
hedging, and the "Coming from VS Code" boxes are consistently the most useful paragraph on the page
("C-g is your Esc", "Ctrl+Tab is C-x b RET", "There is no box: the search prompt is the bottom
line"). Terminology (frame, window, buffer, point, mark, kill, yank) is introduced once and then used
consistently. Weak spots: statements that are not true in the learner's session ("its mode line is
drawn brighter", "C-x C-b lists every buffer in a second window"), unexplained mode-line tokens
("wb", "Ws-butler"), and the "Esc three times" phrasing.

**Hint quality.** Two-level hints ("Show a hint" then "Show the exact keys") are a sensible ladder, but
level one usually just restates the step ("Press C-x, then C-b." for a step whose text already says
"C-x C-b"). The level-two key strings are precise and I liked them, but they assume a starting state
("the prompt now starts in src/") that resets do not guarantee. The *automatic* hint after a failed
C-x 1 was the best hint of the evening because it reacted to what I actually did.

**"Watch it".** Running the demo in my live Emacs is a great idea, but at 2-3 s it is too fast to
follow, there is no narration of which keys are being pressed (the ticker shows them afterwards,
newest first, which reads backwards), and completing the step for me removes the reason to practise.

**Exercise realism.** High. Real files (a Rust CLI with modules, a README, an Elisp rule file), real
tasks (reorder functions, fix a config rule, run a build). The 1.1 "five spots" file and the 3.5
popups.el file are the kind of thing I would actually do. 2.3's "make a mess on purpose" steps are
clever: they rehearse the two most common panic moments.

**What felt magical.** (1) Pressing Ctrl+S and being told, in one card, what happened and what the
real key is. (2) "Where am I" always knowing the state: "You are searching", "C-h k is waiting for a
key to explain", "Emacs asks a question: Save your position in the tutorial? (y or n)", "This is
Dired". (3) Opening the source of save-buffer with one key. (4) Seeing Help jump to the right edge
after evaluating my own rule.

**What felt boring or wrong.** Typing a full command name into M-x with no completion list at all
(Part 4 apparently fixes that, but Part 0 asks me to type `emacs-version` and `display-line-numbers-mode`
blind). Steps that were already ticked when I arrived, so there was nothing to earn. Being nagged by
"Did you mean" for keys the lesson told me to press.

---

## UI design critique

**Layout.** Three columns at 1600 px: 380 / fluid / 320. It works, and the independent scrolling of
the prose column is right. The step list is below the prose, so on arrival the steps are off screen;
the floating "Current step" pill mitigates this but truncates ("Current step: Question 2: what does
-:**- ..."). The Emacs pane has a lot of empty dark area below short files; a 37-row terminal is
fine, but visually the dark block dominates the page.

**Typography.** The serif prose and monospace keycaps are a pleasant, distinctive pairing. Keycaps
inline in prose are readable. In the right rail, chords are rendered as separate caps without spaces
("C-xo", "C-x0", "C-xC-q" in the plain text), which read as one token when the rail is narrow.

**Colour.** Restrained: purple accent, green for met checks, a yellow "region active" chip, red only
in Emacs. The two mode lines in the terminal theme do not differ enough to show which window is
selected, which the course relies on. Good contrast everywhere else.

**Density.** Fine on the left, heavy on the right once a prompt is open: three stacked cards
("M-x is waiting for a command name", "A prompt is open at the bottom: M-x", "The frame is split
into two windows") say overlapping things (`14-l01-q5-typed-emacs-version.png`). "Did you mean" cards
then push "Rescue" below the fold, which is exactly where I need it when things go wrong
(`74-habit-ctrl-slash.png` shows five cards stacked and the Rescue box pushed down).

**The ticker.** Useful when it names a chord ("C-M-a · treesit-beginning-of-defun · Go to the start of
this function"); noisy when I type text: every character becomes a `self-insert-command` card and the
interesting chord scrolls out within four letters. It persists across lessons (0.2 opened with 0.1's
`M-x emacs-version` cards still showing) but is wiped by a full page reload, so its memory is
inconsistent. Newest-first ordering makes a demo read backwards. Some labels are wrong (C-k shown as
kill-region, M-y as yank, End as C-d, TAB in minibuffer as completion-at-point) and one card was
garbled ("C-y · t" with no description, `73-habit-ctrl-y.png`). Verdict: it helps me learn command
names, but it needs to collapse runs of typing and get its labels right.

**Side panels.** "Where am I" is the strongest part of the product: a four-line state table plus one
or two cards with the exit key. "Did you mean" is excellent in concept; in practice it misfires on
instructed keys and forgets its "once each" promise after a reload. "Keys for this buffer" is accurate
per mode (Rust, Markdown, IBuffer, Dired, Help, Elisp) and I used it. The "Rescue" box's description
("Never touches your files") sits directly above both "Rescue me" and "Reset this step", and the second
one does touch files.

**Motion.** Essentially none; state changes just appear. That is fine, except the step tick has no
transition, so I sometimes missed that a step had completed and the list had shifted.

**Responsiveness.** 1280x800: the terminal shrinks to 76x27 columns (`162-viewport-1280x800.png`),
which wraps code lines; the prose and rail keep their full widths. 1024x768: the rail collapses into a
"Where am I" header button and the breadcrumb truncates to "Cou..." (`163-viewport-1024x768.png`);
the terminal is 92x26. No horizontal overflow at either size. The terminal is the element that should
keep its width; the rail could collapse earlier.

**Accessibility concerns.** The terminal is a canvas with no DOM text (no accessibility tree unless
"Screen reader mode for the terminal" on the Session page is on; I did not test it). Green-only step
ticks plus a dot are fine for colour blindness because there is also a check icon. Keycaps have no
`aria-label` that I could see. "Did you mean" dismiss buttons are labelled "Dismiss". The inline "try
it" buttons are tiny targets. Focus is not visibly managed when the lesson pane auto-scrolls.

---

## Bugs and glitches

Each with repro steps and screenshots.

1. **End key deletes a character; Delete key deletes backwards.** Repro: open any lesson, click in
   the Emacs pane, put the cursor at the start of a line (`77-endkey-before.png`), press End. The
   ticker shows "C-d · delete-char" and the character under the cursor is gone; the buffer becomes
   modified (`78-endkey-after.png`). Press Delete: ticker shows "DEL · delete-backward-char"
   (`79-deletekey-after.png`). Home is reported as "C-a" (works, but mislabelled); PageDown is fine
   (`<next>`). Destructive for a newcomer who leans on Home/End/Delete. Also recorded in the habit run
   (`66-habit-home.png`, `67-habit-end.png`).
2. **Lesson 2.4 step 1 "hello's main.rs and lib.rs are open" never becomes met**, even when the
   official "Watch it" demo does it. Repro: open 2.4, C-x C-f /work/hello/src/main.rs RET, C-x C-f
   /work/hello/src/lib.rs RET, C-x C-f /work/projects/polyglot/src/main.rs RET, C-x C-f ../README.md
   RET. Screen shows main.rs<hello>, lib.rs, main.rs<polyglot>, README.md<polyglot>
   (`118-l24-four-files.png`, `120-l24-after-switching.png`); check stays grey
   (`121-l24-step1-stuck.png`). "Watch it" ends in `lib.rs<src>` with a doubled path
   `{/work/hello/src/src/}` in the prompt (`123-l24-watchit-mid.png`) and the check is still grey
   (`124-l24-watchit-end.png`). "Reset this step" does not restore the hello working directory
   (`125-l24-after-reset.png`), so the hint's relative paths open polyglot's files
   (`126-l24-redo-still-stuck.png`).
3. **Lesson 3.1 step 2 does not detect the correct state.** Repro: open 3.1, C-x 2, C-x o, M->,
   type `// written in the lower window`. Screen matches the step (`134-l31-edit-lower.png`); "the
   lower window is selected" stays grey (`140-l31-step2-state.png`). After "Reset this step", C-x o,
   M->, typing: "the lower window is selected" turns green but "main.rs has the new comment" does not
   (`142-l31-step2-retry.png`).
4. **Stale "ESC is a prefix" state in Where am I.** Repro: C-x 2, C-x 3, M-x, then Escape three
   times. After keyboard-escape-quit runs, the rail keeps "ESC is a prefix: Emacs is waiting for the
   next key." (`37-l02-esc3.png`, still there 2.5 s later and after a second ESC ESC ESC,
   `38-l02-esc3-again.png`). It persisted into lesson 0.3 (`39-l03-loaded.png`) and reappeared after
   pressing Escape in Focus mode, surviving the GUI switch and back (`161-back-to-terminal.png`).
   The ticker also shows a trailing "ESC · waiting for the next key" card after the triple-ESC card.
5. **Stale progress on a fresh visit, and no way to clear it.** Repro: load `/`. Most lessons are
   `done`, "you are here" is on 0.2, "Continue: 0.2" (`01-landing.png`). Inside lessons, later steps
   are pre-ticked (`39-l03-loaded.png`, `115-l24-loaded.png`). After my session the map marks 2.4 and
   3.1 "done" although I never completed them, and the Continue button now points at 3.8, which I never
   opened (`165-course-map-after.png`). No reset control on `/session` (`05-session-page.png`).
6. **"Watch it" completes the step for the learner.** Repro: 0.2, click "Watch it" on "Start a
   search, then cancel it". Demo runs ~2.5 s (`21-l02-watchit-0.png`) and the step is ticked
   (`21-l02-watchit-1.png`, `22-l02-watchit-end.png`).
7. **Terminal size flaps while a demo banner is shown.** The status strip reads 122×37 normally,
   122×36 after the first demo, 122×35 during 2.4's demo, then 122×37 again
   (`21-l02-watchit-0.png`, `26-l02-cx1.png`, `123-l24-watchit-mid.png`). Each change reflows the
   Emacs frame.
8. **"Did you mean" fires on keys the lesson told me to press.** Seen for C-x 2/C-x 3 ("Three windows
   is usually an accident.", `34-l02-big-mess.png`, `135-l31-three-windows.png`), C-b and C-l in 1.1
   (`88-l11-spot5.png`), C-k in 1.2 (`92-l12-step2-moved.png`) and 3.5 (`145-l35-build-output.png`),
   C-s and C-a in 1.4 (`98-l14-isearch-1of4.png`). Also fires again after a reload despite "once each".
9. **Dismissing one "Did you mean" card removed all but the newest.** Repro: accumulate five cards
   (`74-habit-ctrl-slash.png`), click the × on the top card; only "C-y is not redo" remains
   (`76-habit-after-close-card.png`).
10. **Ticker mislabels.** C-k → "kill-region · Cut the selected region" (`90-l12-step1-killed.png`);
    M-y → "yank" (`94-l12-step3-My.png`); minibuffer TAB → "completion-at-point"
    (`108-l23-findfile-tab.png`); C-y on an empty kill ring → a card reading only "C-y · t"
    (`73-habit-ctrl-y.png`).
11. **Generic prompt card is wrong for yes/no and number prompts.** On "Save your position in the
    tutorial? (y or n)" the rail shows both the correct "Answer y or n" card and "RET accepts, C-g
    cancels. TAB completes names. M-p brings back what you typed before." (`32-l02-kill-save-position-q.png`);
    the same generic card appears for "Goto line:" (`87-l11-goto-line-prompt.png`) and for M-x
    alongside a near-duplicate "M-x is waiting for a command name" (`14-l01-q5-typed-emacs-version.png`).
12. **Switching GUI → Terminal leaves "Connection lost to X server ':1'" in the echo area and
    changes the window layout.** Repro: on 3.5 click GUI, wait, click Terminal. The terminal frame
    comes back split in two with the error message at the bottom (`161-back-to-terminal.png`,
    `162-viewport-1280x800.png`).
13. **GUI frame renders tiny and partial.** After clicking GUI, a 854x480 canvas shows an Emacs frame
    drawn at roughly half the pane width with very small text, showing only the tail of the buffer
    (`159-gui-after-10s.png`). The rail copy ("You are in the graphical frame...") is good; the
    rendering is not.
14. **Focus mode has no visible effect or explanation.** Click "Focus mode": the button gets an
    outline, nothing else changes, no tooltip (`156-focus-mode-on.png`, `157-focus-mode-off.png`).
    (Headless Chrome may be the reason keyboard lock does nothing; the UI should still say what the
    button is for.)
15. **The "You can run the command ... with M-x em-ve" message masks the lesson's answer** for ~2 s
    after M-x emacs-version (`15-l01-q5-done.png` vs `16-l01-steps-complete.png`).
16. **Lesson 0.1 claims the selected window's mode line is "brighter"; it is not distinguishable**
    in the shipped terminal theme (`08-l01-q1-clicked-readme.png`, `134-l31-edit-lower.png`).
17. **Lesson 0.2 step 3 text is wrong when the files module is on** (ibuffer takes over the window;
    C-x 1 fails with "No other windows to delete", `26-l02-cx1.png`).
18. **"the help menu was opened" check lags until the second key** (C-h ? then b, `46-l03-help-menu.png`).

---

## Top 10 recommendations, ranked by impact

1. **Fix the step checkers that cannot be satisfied (2.4 step 1, 3.1 step 2) and add a test that runs
   every "Watch it" demo and asserts the step passes.** A broken checker is a locked door; the demo is
   the obvious regression test.
2. **Fix End and Delete key translation in the terminal.** End must be `<end>`, Delete must be
   `<deletechar>`. Today End deletes text.
3. **Start fresh visitors fresh.** Either namespace progress per browser or show a "Start over"
   control on the map and the Session page; never pre-tick steps a learner has not done, and do not
   count stale ticks toward "done" on the map.
4. **Make "Watch it" a demonstration, not a completion.** Slow it to human speed with a running caption
   of the keys, then restore the checkpoint and leave the step for the learner. Likewise, inline "try
   it" clicks should not satisfy checks.
5. **Suppress "Did you mean" when the current step's text contains the key**, and honour "once each"
   across reloads. The feature is superb when it fires on a real habit; it loses credibility when it
   fires on an instruction.
6. **Collapse runs of `self-insert-command` in the ticker** into one card ("typed 26 characters") and
   fix the mislabelled commands (C-k, M-y, TAB, Home/End, the empty-kill-ring "t" card).
7. **De-duplicate the rail cards for prompts.** One card per prompt type with the right keys (y/n,
   number, name with completion); drop the generic "TAB completes names" text when it is false.
8. **Make the selected window visibly different** in the terminal theme (a brighter or coloured mode
   line, as the prose promises) and explain the `wb` / `Ws-butler` token the first time it appears.
9. **Clear stale rail state** (the "ESC is a prefix" card) when Emacs reports no pending keys, and
   clear the ticker when a new lesson loads.
10. **Explain Focus mode and the GUI switch in place** (a tooltip or one-line banner), fix the
    "Connection lost to X server" message and layout change on switching back, and separate "Reset
    this step" (which does overwrite files) from the "Never touches your files" sentence.

Smaller things worth doing: split 2.4 step 1 in two; add a "Skip this step" or "Mark done" fallback
behind "I'm stuck"; mention how to page which-key ("C-h for the next page"); give level-one hints
something the step text does not already say; keep the terminal at full width before shrinking the
rail at 1280 px.

---

## Scorecard (1-5)

| Area | Score | One sentence |
|---|---|---|
| First-run clarity | 3 | The map and the "in VS Code:" lines explain the course instantly, but stale "done" marks and a wrong "Continue" target undermine the very first screen. |
| Feedback immediacy | 4 | Every working check turned green within 1.5-2.2 s and "Where am I" updated on every keystroke; the score is not 5 because two steps never reacted at all. |
| Help when stuck | 3 | The automatic hint after a failed C-x 1, the "Did you mean" card for Ctrl+S and the "I'm stuck" report are excellent, but hints restate the step, "Watch it" does the step for you, and nothing lets you skip a broken step. |
| Visual design | 4 | Calm, book-like typography with keycaps that make chords scannable; loses a point for the stacked, overlapping rail cards and the indistinguishable mode lines. |
| Prose quality | 4 | Clear, concrete, confident writing with consistently useful VS Code bridges; marred by claims that are false in the learner's session. |
| Realism of exercises | 4 | Real files, real edits, a real build and a real config rule; 2.4's four-file step is the one that overreaches. |
| Performance | 4 | The warm session showed Emacs in under 2 s, lessons loaded in 3-4 s, checks in ~2 s; only the GUI frame and demo-time terminal resizes felt rough. |
| Overall | 3 | A genuinely good idea with the best "what state am I in" panel I have seen in a tutorial, held back tonight by two unpassable steps, a destructive End key and progress that lies. |
