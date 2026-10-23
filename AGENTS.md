# outrash

Windows-first dev-cache cleaner. Go + mygo native UI. Cross-platform logic, no webviews.

## Hard rules

- **Never delete anything.** Not in code paths, not in tests, not by running the app.
  Every action is a *plan*: list the paths, sizes, and order. The user reads it and decides.
- No persistence. No config files, no registry, no logs. Temp dir only, and only if unavoidable.
- Rules live in one place: `internal/core/rules.json`, embedded with `go:embed` as `core.Rules`.
- Only removable-by-redownload content. Never sources, never project dirs, never consoles.

## Layout

| file | role |
|---|---|
| `main.go` | flags, window wiring; nothing else lives at the root |
| `internal/core` | rules: parse + env override + path expansion · scan: walk dirs concurrently, per-entry size/mtime, classify versions · plan: policy → ordered list of `Step` (path, bytes, reason) · clean: guarded `os.RemoveAll`, the only code that deletes · theme: palette + type scale (ledger: paper, ink, one accent) · `rules.json` catalog |
| `internal/app` | `app.go` title bar, rail, header, list, action bar · `cards.go` item and version rows, tri-state check, presets · `sheet.go` plan sheet, report sheet, clean run |

## Commands

```
just ci                  # build + vet + test, the CI pair
just debug               # every plan printed to stdout, execute button hidden
just safe                # same refusal, keeps the normal UI
just shots               # regenerate testdata/shots/*.png from internal/app
go tool mygo vet .
```

## mygo essentials

- `ui.View(fn)` builds every frame. Elements die; keep only plain state (`map`, `*bool`, `ui.Handle`).
- `c.SetTheme(t)` each frame, before widgets.
- `Children(func())` scopes the parent.
- `Window.Update(fn)` for goroutine → UI state.
- A click reaches only the innermost interactive element; put `.OnClick` on the row you mean.
- Title bar: `mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden}` + `c.TitleBar()` + `.DragWindow()`.
