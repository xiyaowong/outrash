# outrash

Windows-first dev-cache cleaner. Go + mygo native UI. Cross-platform logic, no webviews.

## Hard rules

- **The user's click is the only delete.** Never remove anything yourself — not from a script,
  not to clean up after a test, not by running the app. Everything the product offers is a
  selection of catalog paths the user can read first.
  The list *is* the plan: a scan lands on a recommendation, the user narrows it, 开始清理 acts on it.
  A build that refuses to delete (`-debug`, `-no-delete`) shows the written plan instead.
- No persistence. No config files, no registry, no logs. Temp dir only, and only if unavoidable.
- Rules live in one place: `internal/core/rules.json`, embedded with `go:embed` as `core.Rules`.
- Only removable-by-redownload content. Never sources, never project dirs, never consoles.

## Layout

| file | role |
|---|---|
| `main.go` | flags, window wiring; nothing else lives at the root |
| `internal/core` | rules: parse + env override + path expansion · scan: walk dirs concurrently, per-entry size/mtime, classify versions · plan: policy → ordered list of `Step` (path, bytes, reason) · clean: guarded `os.RemoveAll`, the only code that deletes · theme: palette + type scale (ledger: paper, ink, one accent) · `rules.json` catalog |
| `internal/app` | `app.go` title bar, header, list, right panel, action bar, recommendation, clean estimate · `cards.go` item and version rows, tri-state check, tags, presets · `sheet.go` plan sheet (no-delete builds only), report sheet, clean run |

## Commands

```
just ci                  # build + vet + test, the CI pair
just debug               # no deletion: the list is printed to stdout, 查看清理计划 opens the sheet
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
