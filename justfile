# just — the commands this repo actually uses. Windows-first: recipes stay
# one-liners so the default shell (PowerShell) and sh both run them.
set windows-shell := ["pwsh", "-NoLogo", "-NoProfile", "-Command"]

# -s drops the symbol table, -w the DWARF data, -trimpath the build paths, and
# -buildvcs the version stamp: 14.2 MB down to 9.9 MB, nothing a user needs.
link_flags := "-s -w"

# Windows gets no console window; other targets would reject -H.
exe := if os_family() == "windows" { ".exe" } else { "" }
gui := if os_family() == "windows" { " -H=windowsgui" } else { "" }

default:
    @just --list

# Build, vet and test; what CI runs on both platforms.
ci:
    go build ./...
    go vet ./...
    go test ./...

# Vet, including the native UI build-lifetime checks mygo adds.
lint:
    go vet ./...
    go tool mygo vet .

# The tests on their own.
test:
    go test ./...

# The tests again under the race detector.
race:
    go test -race ./...

# Coverage per package.
cover:
    go test -cover ./...

# Stripped, path-free, no console window, into gitignored build/.
build:
    go build -trimpath -buildvcs=false -ldflags "{{link_flags}}{{gui}}" -o build/outrash{{exe}} .

# The same build without the size linker flags. It keeps the console
# subsystem: this is the build to debug, and a GUI app's stdout and stderr
# are invisible when it is started from a terminal.
build-full:
    go build -o build/outrash-full{{exe}} .

# The packaged app as it ships; only this one has a version in its about box.
release:
    go tool mygo build

# The window, with every plan printed to stdout and deletion disabled.
debug:
    go run . -debug

# The window as a user sees it, but every deletion refused.
safe:
    go run . -no-delete

# Regenerate docs/screenshot.png (committed) and testdata/shots (scratch).
shots:
    $env:OUTRASH_SHOTS = "1"; go test ./internal/app -count=1

# Regenerate the icon and the Windows resource object the build links.
icon:
    go run ./assets/genicon
