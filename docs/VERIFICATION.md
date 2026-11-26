# Verification status

Last updated while building the first version in an environment **without a Go toolchain and
without network access**. Therefore nothing below has been compiled or executed.

## 1. Verified through automated tests
Nothing yet. `go test ./...` has not been run.

Tests that are written and ready to run (see `internal/*/..._test.go`):
- patch engine: exact/ambiguous/missing/overlapping SEARCH, unicode, tabs, hunk offset, empty file, missing final newline
- parser: both formats, prose/fence/CRLF input, ~35 malformed or unsupported inputs, ambiguity, fuzz target
- diff: simple diffs, hunk splitting, large-change fallback, randomized diff -> parse -> apply round trip
- safefs: traversal, absolute, drive letter, UNC, `.git`, ADS, reserved names, symlink escape, encodings, BOM/CRLF round trip, read-only
- apply: backups with manifest and permissions, BOM/CRLF preservation, changed-after-preview, backup collision/failure,
  temp-file failure, rename failure + rollback, multi-file validation failure, duplicate targets
- clipboard: tool selection per OS with fake environment
- cli: help/version, bad flags, empty clipboard, clipboard failure, `--stdin`, `--dry-run`, approval/rejection/EOF, `--yes`, `--root`, `--backup-dir`, no-TTY

## 2. Verified through manual testing
Nothing yet. Follow `docs/MANUAL_TEST.md`.

## 3. Not yet verified
- That the code compiles and `gofmt -l .` is clean (run `make fmt` first).
- `go vet`, `go test`, `go test -race`, fuzzing.
- Real clipboard reading on Windows, macOS, Linux X11, Linux Wayland.
- Interactive prompt via `/dev/tty` (Unix) and `CONIN$` (Windows).
- Colour output on Windows terminals.
- The GitHub Actions workflows (never run).
- Behaviour on very large files and many-file patches (performance is unmeasured).

## Known design deviations / decisions
- The example diff in the original project brief declared `@@ -1,3 +1,4 @@` but contained
  only 2 old / 3 new lines. A strict parser rejects it by design; examples here use correct counts.
