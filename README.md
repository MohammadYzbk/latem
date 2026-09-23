# latem

Latem is an offline-first, cross-platform desktop LaTeX editor focused on a
polished writing, compilation, diagnostics, and PDF-preview workflow. It
orchestrates an external TeX engine rather than implementing one.

## Current status

One application: a [Wails v2](https://wails.io) desktop app with a Go backend
and a TypeScript/CodeMirror frontend, running on macOS, Windows, and Linux from
a single codebase.

- Opens a project folder, edits `.tex` sources, and compiles with Tectonic.
- Parses engine logs into structured diagnostics.
- Renders the PDF preview with `pdf.js`.
- Supports bidirectional SyncTeX navigation: the preview follows the cursor,
  `Cmd-J` jumps to the current source location, and clicking the preview jumps
  back to source. The parser lives in [`internal/synctex`](internal/synctex/)
  with tests against real engine output.

Against the phased build plan in
[`docs/project-plan.md`](docs/project-plan.md), Phases 0-4 (foundations,
vertical slice, the live-ish compile loop, projects as folders, and
bidirectional SyncTeX) are in place. Phase 5 editor polish is partial, and
Phases 6-9 (GitHub sync, robustness, packaging) are not started.

Collaboration, plugins, AI, and telemetry remain out of scope.

## Repository layout

```text
main.go, app.go           Wails entrypoint and the bound app API
pdfserver.go              Serves compiled PDFs to the frontend
settings.go               Persisted user settings
internal/
  tex/                    TeX engine invocation
  texlog/                 Engine log parsing into diagnostics
  synctex/                SyncTeX parser for source/preview navigation
  project/                Project folder and file handling
frontend/                 Vite + TypeScript UI (CodeMirror editor, pdf.js preview)
build/                    Wails packaging inputs for darwin and windows
```

## Development

Requirements: Go, Node, [Wails v2](https://wails.io), and `tectonic` on `PATH`.

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails generate module   # regenerates frontend/wailsjs bindings
wails dev               # run with live reload
go test ./...           # Go tests
wails build             # produce a platform binary
```

JetBrains users can run the checked-in **Latem (Wails)** configuration in
[`.run/`](.run/), which is equivalent to `wails dev`.

`frontend/wailsjs` is generated and untracked. Keep `frontend/dist/.gitkeep`;
`main.go` embeds that directory, so a fresh checkout must contain it.

## Contributor entrypoints

- [`AGENTS.md`](AGENTS.md) for universal guardrails and task routing. Claude
  Code reaches the same entrypoint through [`CLAUDE.md`](CLAUDE.md).
- [`CONTRIBUTING.md`](CONTRIBUTING.md) for branch, commit, and pull-request
  format.
- [`docs/project-plan.md`](docs/project-plan.md) for the vision, architecture,
  stack decisions, and the phased build plan.
- [`CONTEXT.md`](CONTEXT.md) for product vocabulary.
- [`docs/phase-0-spikes.md`](docs/phase-0-spikes.md) for the Phase 0 spike
  results that verified the stack choices.

## License

MIT — see [LICENSE](LICENSE).
