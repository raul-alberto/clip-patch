# Verification Status

Last updated: 2026-11-26

The project has been compiled, tested, and manually verified. The automated CI pipeline passes on Windows, Linux, and macOS.

## 1. Verified through automated tests

The following checks have passed:

- `gofmt` formatting check
- `go vet ./...`
- `go test ./...`
- `go build ./...`
- Fuzz smoke test (`FuzzParse`, 20 seconds)
- CI pipeline on Windows, Linux, and macOS

The automated test suite covers:

- Patch engine: exact, ambiguous, missing and overlapping SEARCH blocks, Unicode, tabs, hunk offsets and empty files.
- Parser: both patch formats, prose, fenced blocks, CRLF input, malformed and unsupported inputs, ambiguity and fuzzing.
- Diff: simple diffs, hunk splitting, large-change fallback and randomized diff/parse/apply round trips.
- Safe filesystem operations: path traversal, absolute paths, drive letters, UNC paths, `.git`, alternate data streams, reserved names, symlink escapes, encodings, BOM/CRLF round trips and read-only files.
- Apply engine: backups, manifests, permissions, BOM/CRLF preservation, changed-after-preview detection, backup collisions and failures, temporary-file failures, rename failures, rollback and multi-file validation.
- Clipboard: platform-specific tool selection using a simulated environment.
- CLI: help/version, invalid flags, empty clipboard, clipboard failures, `--stdin`, `--dry-run`, approval/rejection/EOF, `--yes`, `--root`, `--backup-dir` and non-interactive terminals.

## 2. Verified through manual testing

Manual QA was performed on Windows with 19 successful scenarios, including:

- SEARCH/REPLACE application.
- Unified Diff application.
- Ambiguous SEARCH rejection.
- Path traversal rejection.
- Dry-run without modifying files.
- Read-only file rejection.
- Backup content and SHA-256 integrity verification.
- Malformed SEARCH/REPLACE rejection.
- Symbolic link target rejection.
- Multi-file patch application.
- Stale fingerprint detection after external file modification.
- Malformed Unified Diff rejection.
- Patch input through stdin.
- Automatic approval with `--yes`.
- Declining a patch without applying it.
- Empty clipboard rejection.
- Symlinked intermediate directory escape rejection.
- Security checks remaining active with `--yes`.
- Multi-file backup content and SHA-256 integrity verification.

## 3. Not yet verified

The following areas still require additional testing:

- Real clipboard behaviour on macOS and Linux X11/Wayland.
- Interactive terminal prompts on Unix (`/dev/tty`) and Windows (`CONIN$`).
- Colour output across different Windows terminal environments.
- Performance with very large files and patches involving many files.
- Full manual QA on Linux and macOS.
- Race detection on Windows (the local environment does not have the required CGO compiler; CI runs race tests on supported non-Windows runners).

## 4. Known design decisions and limitations

- Multi-file application is not globally atomic if a write fails after earlier files have already been applied.
- Backup integrity has been checked through file contents and SHA-256 hashes. A complete restore workflow has not been tested.
- The example diff in the original project brief declared `@@ -1,3 +1,4 @@` but contained only 2 old and 3 new lines. A strict parser rejects it by design; examples in this project use correct line counts.
