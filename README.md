# clip-patch

**Copy an AI-generated patch, run `cpatch`, review the diff, apply it safely.**

> **Status: pre-release (0.1.0 MVP).** Not yet published. The code has not yet been
> compiled or tested by its author at the time of writing; see
> [docs/VERIFICATION.md](docs/VERIFICATION.md) for exactly what has and has not been verified.

AI assistants often return code changes as partial blocks or diffs. Pasting them
by hand risks wrong indentation, edits in the wrong place, and damaged code.
`clip-patch` reads the patch from your clipboard, checks it against your real
files, shows a diff, backs up the originals, and only then writes.
Everything runs locally: no network, no API keys, no background services.

```text
$ cpatch
clip-patch v0.1.0

Patch detected: SEARCH / REPLACE
Files affected: 1
Changes detected: 1

[MODIFIED] greeter.py
...diff preview...

Apply these changes? [y/N] y

Applied changes to 1 file(s):
  [MODIFIED] greeter.py
Backup of the originals: ~/.local/share/clip-patch/backups/20260101-120000-ab12cd34
```

<!-- Demo GIF goes here once recorded from the real tool: docs/media/demo.gif -->

## Key features

- Two input formats: **SEARCH / REPLACE blocks** and **unified diff** (Git-style or plain), auto-detected.
- **Exact matching.** SEARCH text must appear exactly once, whitespace included. Zero or several matches abort the whole patch.
- **Preview first.** A unified diff of every change; `--dry-run` never writes.
- **Confirmation by default.** Pressing Enter does *not* apply. `--yes` skips the prompt but keeps every check and backup.
- **Automatic verified backups** of exact original bytes, with a manifest, before any write.
- **All-or-nothing validation.** Every file is checked and every result computed in memory before anything is written. A fingerprint check aborts if a file changed after the preview.
- **Path safety.** Paths in the patch are untrusted: `..`, absolute, UNC, drive-letter, symlinks and `.git` are rejected.
- **Format preservation.** UTF-8 BOM, CRLF/LF line endings and the final-newline state are kept.
- A single static binary built from the Go standard library only. No runtime to install.

## Installation

Requires Go 1.21 or newer to build.

```bash
git clone <your-repository-url> clip-patch
cd clip-patch
go install ./cmd/clip-patch ./cmd/cpatch     # installs both names into $(go env GOPATH)/bin
# or: make build   -> bin/clip-patch and bin/cpatch
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

**Clipboard tools** (the clipboard is read by the platform's standard tool):

| OS | Tool used | Notes |
|---|---|---|
| Windows 10/11 | `powershell` (`Get-Clipboard`), falls back to `pwsh` | Built in |
| macOS | `pbpaste` | Built in |
| Linux, Wayland | `wl-paste` | Install `wl-clipboard` |
| Linux, X11 | `xclip` or `xsel` | Install one of them |

If no clipboard tool works, use standard input instead.

## Quick start

```bash
# 1. Copy the AI's patch to the clipboard.
# 2. From your project directory:
cpatch --dry-run     # preview only
cpatch               # preview, confirm, apply
```

Without a clipboard:

```bash
cat patch.diff | cpatch --stdin --dry-run          # Linux / macOS
Get-Content patch.diff -Raw | cpatch --stdin       # Windows PowerShell
```

With `--stdin` the confirmation prompt is read from the terminal, not from the pipe. If no
terminal is available, pass `--yes` or `--dry-run`.

## SEARCH / REPLACE format

```text
### FILE: greeter.py

<<<<<<< SEARCH
def greet():
    print("Hello")
=======
def greet(name="World"):
    print(f"Hello, {name}!")
>>>>>>> REPLACE
```

- A `### FILE: path` header names the file for the blocks that follow it. Multiple blocks and multiple files are allowed.
- Text outside blocks (explanations, code fences) is ignored.
- SEARCH is matched against the **original** file and must match exactly once. Blocks must not overlap.
- An empty REPLACE section (no lines) deletes the matched lines.
- An empty SEARCH section is rejected (clip-patch does not create files or append blindly).
- A line that is exactly `=======` inside a block is ambiguous and rejected.

## Unified diff format

```diff
diff --git a/greeter.py b/greeter.py
--- a/greeter.py
+++ b/greeter.py
@@ -1,2 +1,3 @@
 def greet():
-    print("Hello")
+    print("Hello, World!")
+    print("Welcome!")
```

- Multiple files and multiple hunks are supported. Git is not required.
- Line counts in `@@` headers must be correct. Miscounted headers are rejected, not guessed at.
- If a hunk's line number is off but its context matches **exactly one** place, it is applied there and the preview shows a `NOTE`. Zero or several candidate places abort.
- Not supported (rejected explicitly, nothing is touched): new files, deleted files, renames, mode changes, binary patches, and `\ No newline at end of file` markers.

## Options

```text
-h, --help           Show help
    --version        Display version
    --dry-run        Preview without modifying files
    --yes            Apply without interactive confirmation (checks and backups still run)
    --format FORMAT  auto (default), search-replace, unified-diff
    --root PATH      Project root (default: current directory)
    --backup-dir PATH  Backup location (default: per-user data directory)
    --stdin          Read the patch from standard input
    --color WHEN     auto (default), always, never
```

`--dry-run` and `--yes` cannot be combined. Exit codes: `0` success (including dry run), `1` patch
rejected / apply failed / you declined, `2` invalid command line.

## Backups

Before any file is changed, its exact original bytes are copied and read back for verification:

| OS | Default location |
|---|---|
| Windows | `%LOCALAPPDATA%\clip-patch\backups` |
| macOS | `~/Library/Application Support/clip-patch/backups` |
| Linux | `$XDG_DATA_HOME/clip-patch/backups` (default `~/.local/share/clip-patch/backups`) |

Each run gets a new unique directory containing `files/<relative path>` and a `manifest.json`
(paths, SHA-256, permissions). Existing backups are never overwritten and old backups are never
deleted automatically. To undo, copy the files back.

## Security model

- Everything is local. There is no network code. Clipboard text is only parsed as data and never executed.
- Patch paths are untrusted. Rejected: `..` segments, absolute and UNC paths, drive letters, `:` (alternate data streams), Windows device names, any `.git` segment, symlinked targets, and files whose real path leaves `--root`.
- Only existing, regular, writable UTF-8 text files up to 32 MiB are modified. Binary files, UTF-16, invalid UTF-8, bare-CR and mixed line endings are rejected.
- Files are re-checked by SHA-256 immediately before writing; changed files abort the run.
- Writes go through a temporary file in the target directory and an atomic rename, after verified backups. On failure, already-replaced files are restored.

**Honest limits:** replacing several files is *not* globally atomic. A crash or power loss in the middle of the rename phase can leave some files updated; the backup directory is your recovery path. There is a short time-of-check/time-of-use window between the fingerprint check and the rename. clip-patch protects against honest mistakes and hostile patch text, not against a hostile process running as you.

## Supported operating systems

Written for Windows 10/11, Linux (X11 and Wayland) and macOS. A GitHub Actions workflow
that runs the tests on all three is included. **Until that workflow and the manual checks have
actually been run, none of these platforms should be considered verified.**

## Known limitations

- Clipboard reading shells out to a platform tool; headless sessions and SSH need `--stdin`.
- No creation, deletion or renaming of files. No binary files.
- No approximate/fuzzy matching, by design.
- Files with mixed or bare-CR line endings are rejected rather than converted.
- Context lines are compared exactly, so a diff whose blank context lines had trailing spaces stripped by the assistant may not match a file that has them.
- Windows ANSI colours are only enabled automatically in terminals known to support them (Windows Terminal); use `--color always` or `--color never` to override.
- No restore command yet; restore by copying from the backup directory.

## Development

```bash
make test         # go test ./...
make race         # go test -race ./...
make vet          # go vet ./...
make fmt          # gofmt -w .
make fuzz         # 30s fuzz run of the parser
make build        # bin/clip-patch and bin/cpatch
```

Layout: `internal/parser` (text → patch), `internal/patch` (apply to text), `internal/safefs`
(path and encoding safety), `internal/apply` (plan, backup, write, rollback), `internal/diff` and
`internal/preview` (output), `internal/clipboard`, `internal/cli`.
The patch engine has no I/O, and the clipboard is behind an interface, so tests need no desktop.

Run `make fmt` once before your first commit; the CI formatting check is strict.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md).

## Roadmap

Ideas, not promises: a `--restore` command, tolerant recounting of miscounted `@@` headers,
optional whitespace-tolerant matching (opt-in only), file creation as an explicit opt-in.

## License

**No license has been selected yet**, so the code is currently "all rights reserved". Before
publishing, choose one and add a `LICENSE` file with the correct copyright holder and year.
MIT or Apache-2.0 are common permissive choices for CLI tools (Apache-2.0 adds an explicit patent
grant). This repository does not guess your legal name.
