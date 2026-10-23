# Lessons

Append-only until stale; delete entries that stop being true. One line each.

## mygo
- `ui.Element` is per-build-pass. Anything stored across frames panics in tests (`strict`). Store plain data.
- Duplicate `c.Key()` among siblings panics in the Tester, so keys must be per-item stable strings.
- A click goes to the **innermost interactive** element in the hit chain; nothing bubbles to ancestors.
- `tt.Click(text)` finds the *first* label whose text matches exactly — a duplicate label (header note, sidebar) silently steals the click. Keep labels unique.
- A `ButtonBase` centres its children; a rail of nav items needs `Justify(ui.Start)` or each icon drifts with its label's length.
- A clickable row is a plain `Row` with `.OnClick`; make chevrons/expanders non-interactive so they fall through to it.
- `Hovered()` is paint-time only, but reading it on the row while building the row is enough: it also makes mygo request a frame when the pointer enters or leaves.
- Render tests write `testdata/shots` relative to the working directory, and `go test` runs a package from its own directory — resolve the root with `..`. Shots are opt-in (`OUTRASH_SHOTS`, `just shots`) so a plain `go test` leaves the tree clean.
- `Tester.Find` matches shown text *or* `Label`. `tt.Click("x")` fails if the text sits off-window.
- Native UI has no CSS; every look is fluent calls + `Draw` painter. Theme copies are cheap.
- Windows hidden title bar: `mygo.TitleBarHidden` keeps real caption buttons over the view; their glyphs come from `Backend.isDark()`, so a light app on a dark desktop needs `Theme.SetSource(mygo.ThemeLight)`, not just a light palette.
- A `Column` inside a `Row` needs `FillHeight()`, or `Grow` children never fill and the bottom bar rides up.
- `ui.Scroll` inside fixed-height rows needs an explicit size or `Grow`, else it collapses.

## Windows
- A junction is `ModeIrregular` to `os.Lstat`, not `ModeSymlink`, and `filepath.EvalSymlinks` leaves it unresolved — "no symlinks" is not "no links".
- `mklink /J` needs no privilege where `os.Symlink` does, so link guards are testable without elevation.
- Files can be locked (indexers, AV). Any real delete must tolerate `ERROR_SHARING_VIOLATION` and report, never retry-loop.
- `%LOCALAPPDATA%\go-build` is often multi-GB; a naive recursive `WalkDir` on it blocks seconds. Stream and cache sizes.
- Path separators: always build with `filepath`, never concatenate `/`.

## Product
- An unreadable cache and an empty cache look identical unless the error is carried to the row. Keep `Root.Err`/`Skipped`, and show them.
- A scan that lands on an empty list makes the user do the first decision; landing on a safe recommendation makes the first screen an answer.
- The summary screen and the cleanup screen are the same decision at two scales: checkboxes on rows, a tri-state box on a family.
