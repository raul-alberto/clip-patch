# Manual test procedure (use a disposable copy)

```bash
go build -o bin/ ./cmd/...            # or: make build
rm -rf /tmp/cp-demo && cp -r examples/demo-project /tmp/cp-demo && cd /tmp/cp-demo
cat greeter.py
```
Use the absolute path to `bin/cpatch` below if it is not on your PATH.

## A. Successful SEARCH / REPLACE (clipboard)
1. Copy the contents of `examples/search-replace.patch` to the clipboard.
2. `cpatch --dry-run` -> expect `Patch detected: SEARCH / REPLACE`, a diff, `Dry run: no files were modified.`; `greeter.py` unchanged.
3. `cpatch`, press **Enter** only -> expect `Cancelled. No files were modified.`
4. `cpatch`, type `y` -> expect `Applied changes to 1 file(s)` and a backup path. `python3 greeter.py` prints `Hello, World!`.
5. Open the backup path: `files/greeter.py` equals the original, plus `manifest.json`.

## B. Successful unified diff (stdin)
1. Reset: `cp /path/to/repo/examples/demo-project/greeter.py .`
2. `cat /path/to/repo/examples/unified-diff.patch | cpatch --stdin` (answer `y`). Expect two printed lines after the change.

## C. Deliberately invalid patch
1. Reset the file. `cpatch --stdin --yes < /path/to/repo/examples/invalid-search-replace.patch`
2. Expect `ERROR: Patch context does not match.`, `File: greeter.py`, `No files were modified.`, a causes list, exit code 1 (`echo $?`), no new backup directory, file unchanged.

## D. Safety spot checks
- Patch with `### FILE: ../outside.txt` -> `Unsafe file path in patch.` and nothing touched.
- Change `greeter.py` after a prompt appears (in another terminal), then answer `y` -> `File changed since the preview.`
- Duplicate the function so SEARCH matches twice -> `ambiguous`.
- CRLF file: convert with `unix2dos` or equivalent, apply a patch, confirm the file is still CRLF (`file greeter.py`).

## E. Clipboard per OS
Verify `cpatch --dry-run` reads the clipboard on each OS you use. Linux needs `wl-paste` or `xclip`/`xsel`; in SSH sessions use `--stdin`.
