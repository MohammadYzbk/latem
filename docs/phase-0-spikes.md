# Phase 0 — foundations: what was verified

> **Historical evidence.** This document records the Phase 0 spikes from the
> [project plan](project-plan.md) and the decisions that came out of them. It is
> a dated snapshot, not a description of current architecture or status.

Dated 2026-07-27, on macOS 15.7.7 / arm64 (Apple M4 Pro). The plan says to
re-verify library APIs at implementation time rather than trusting a snapshot, so
this file records what was actually checked and the decisions that came out of
it. Re-check before Phase 6, which is the next phase that adds new dependencies.

## Toolchain in use

| Piece | Version | Notes |
|---|---|---|
| Go | 1.26.1 | `go.mod` targets 1.25.0 |
| Wails | **v2.13.0** | see decision below |
| Node | 26.5.0 | npm 11.17.0 |
| Vite | 7.3.6 | from the Wails template |
| Tectonic | 0.17.0 | Homebrew, on `PATH` |
| CodeMirror | 6 (`@codemirror/view` 6.43.x) | `stex` legacy stream mode |
| PDF.js | `pdfjs-dist` 6.1.200 | **legacy** build, see below |

## Decision: Wails v2, not v3

v3 is still `v3.0.0-alpha2.118` (tagged 2026-07-26 — actively moving, but alpha).
v2.13.0 is the maintained stable line and was released 2026-07-06. Phase 0 through
Phase 9 all sit on v2; a v3 migration is a deliberate later choice, not something
to absorb mid-build.

## Spike 1 — Tectonic driven from Go

`internal/tex` builds the command line, runs the engine, and reports where the
artifacts landed. Covered by `internal/tex/compile_test.go`, which skips cleanly
when no engine is on `PATH`.

Verified:

- A hello-world document produces a **PDF, a `.log`, and a `.synctex.gz`**. The
  log and SyncTeX files only survive because of `--keep-logs` and `--synctex`;
  without them the engine cleans up after itself. The tests assert all three
  exist specifically to catch that flag regressing.
- **A broken document is not an error of ours.** `Compile` returns
  `Success=false` with a nil error when the engine ran and the *document* is
  bad, and a non-nil error only when there was no verdict at all (binary
  missing, timeout, cancellation). Phase 2's inline error markers depend on this
  split, so it is asserted directly.
- The raw log carries both `Undefined control sequence` and the offending command
  name, which is the raw material Phase 2 needs.

Timings, and they matter for Phase 2:

| | Time |
|---|---|
| Cold cache (first ever run, downloads support files) | **~96 s** |
| Warm cache | **~190–250 ms** |

The warm number is the one the "feels live" loop is built on, and it comfortably
supports a debounced auto-compile. The cold number is a **Phase 9 first-run
problem**: the zero-config promise costs ~90 s and a network connection the very
first time, and populates a ~42 MB cache at
`~/Library/Caches/TectonicProject.Tectonic`. Shipping without a progress
indicator for that first compile would read as a hang.

Also decided here: `Compiler.Untrusted` defaults to **true**, disabling
shell-escape. A project can be a freshly cloned repo whose `.tex` we have never
read. Documents that genuinely need shell-escape (minted, gnuplot) will have to
opt out per project.

## Spike 2 — PDF.js renders engine output

Works, but only via the **legacy** build:

```ts
import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.mjs?url';
```

The modern build fails at the first render inside the macOS WKWebView with:

```
TypeError: this.#e.getOrInsertComputed is not a function
```

PDF.js 6.x uses `Map.prototype.getOrInsertComputed`, which this system WebKit
does not implement. The legacy bundle carries the core-js polyfill for it. Since
a Wails app runs against whatever webview the OS provides — not a version we
pin — legacy is the correct default here, not a workaround to remove later. Cost
is roughly +55 kB on the main chunk and +160 kB on the worker.

Two further notes for Phase 1 and Phase 4:

- Canvases are backed at `devicePixelRatio` and laid out in CSS pixels, so text
  stays crisp on a Retina display. The render transform must match, or output is
  blurry at exactly the moment quality is most visible.
- In PDF.js 6, `page.render()` takes `canvas`; `canvasContext` is deprecated.
- `renderPDF` returns the unscaled page size in points. Phase 4 needs that,
  plus the viewport transform, to map SyncTeX coordinates.

## Spike 3 — CodeMirror 6 mounts with LaTeX awareness

`internal`-free, in `frontend/src/editor.ts`. Highlighting, bracket matching,
folding, history, and tab indent all work against the `stex` legacy stream mode.
That mode is enough for highlighting and nothing more: **Phase 5 replaces it**
with real completions (environments, `\ref`/`\cite` targets pulled from the
project). If structure-aware features want a Lezer LaTeX grammar, that swap
belongs in the same phase.

## Spike 4 — the Wails bridge

`App.CheckEngine` is bound to the frontend and the spike page calls it on load,
so the bridge and the generated bindings are proven too. The four status pills in
the app header are the live result of all four spikes.

Two things about it are deliberately temporary:

- **The PDF crosses the bridge as base64.** Fine for a one-page spike, wrong for
  a real document. Phase 1 should serve the PDF through the Wails asset handler
  instead.
- **The editor is not wired to the compiler.** `CheckEngine` compiles a document
  embedded in the binary, not the buffer. Wiring the two together is exactly
  Phase 1's vertical slice, and doing it here would have been building Phase 1
  early rather than de-risking it.

## Repo shape gotcha

`main.go` has `//go:embed all:frontend/dist`, but the build output is gitignored.
A fresh checkout would therefore fail `go build ./...` with nothing to embed, so
`frontend/dist/.gitkeep` is committed and `.gitignore` ignores the directory's
*contents* rather than the directory. Likewise `build/darwin` and `build/windows`
are packaging *inputs* (plist and manifest templates) and must stay tracked;
only `build/bin` is ignored.

## Noted for later

- **go-github** is at major **v75**; the plan's table is not version-specific,
  and Phase 6 should pin whatever is current then.
- **go-git** is at v5.19.1. Still keep the Git layer behind an interface, per the
  plan's risk register, so a `git`-CLI backend can be swapped in.
- CI installs Tectonic via its documented installer so the compile tests really
  run rather than skipping. Expect the first CI run on a fresh cache to be slow.

## Done-when, checked

The empty shell builds and launches on macOS, and each risky dependency has a
proven minimal example: engine ✓, PDF.js ✓, CodeMirror ✓, bridge ✓.
