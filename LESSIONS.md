# Lessons

Append-only until stale; delete entries that stop being true. One line each.

## mygo
- `ui.Element` is per-build-pass. Anything stored across frames panics in tests (`strict`). Store plain data.
- Duplicate `c.Key()` among siblings panics in the Tester, so keys must be per-item stable strings.
- A click goes to the **innermost interactive** element in the hit chain; nothing bubbles to ancestors.
- `tt.Click(text)` finds the *first* label whose text matches exactly — a duplicate label (header note, sidebar) silently steals the click. Keep labels unique.
- A clickable row is a plain `Row` with `.OnClick`; make chevrons/expanders non-interactive so they fall through to it.
- `Hovered()` is paint-time only; there is no public per-state style hook. Skip hover fills rather than fight it.
- `Tester.Find` matches shown text *or* `Label`. `tt.Click("x")` fails if the text sits off-window.
- Native UI has no CSS; every look is fluent calls + `Draw` painter. Theme copies are cheap.
- Windows hidden title bar: `mygo.TitleBarHidden` keeps real caption buttons over the view.
- A `Column` inside a `Row` needs `FillHeight()`, or `Grow` children never fill and the bottom bar rides up.
- `ui.Scroll` inside fixed-height rows needs an explicit size or `Grow`, else it collapses.

## Windows
- Files can be locked (indexers, AV). Any real delete must tolerate `ERROR_SHARING_VIOLATION` and report, never retry-loop.
- `%LOCALAPPDATA%\go-build` is often multi-GB; a naive recursive `WalkDir` on it blocks seconds. Stream and cache sizes.
- Path separators: always build with `filepath`, never concatenate `/`.
