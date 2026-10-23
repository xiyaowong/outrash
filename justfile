# just — the commands this repo actually uses. Windows-first: recipes stay
# one-liners so the default shell (PowerShell) and sh both run them.
set windows-shell := ["pwsh", "-NoLogo", "-NoProfile", "-Command"]

default:
    @just --list

# Build, vet and test; what CI runs on both platforms.
ci:
    go build ./...
    go vet ./...
    go test ./...

# The window, with every plan printed to stdout and deletion disabled.
debug:
    go run . -debug

# The window as a user sees it, but every deletion refused.
safe:
    go run . -no-delete

# Regenerate testdata/shots/*.png from the render tests.
shots:
    go test . -count=1
