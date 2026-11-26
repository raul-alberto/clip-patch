# Contributing

Thanks for helping. clip-patch edits people's source files, so correctness and safety come first.

## Ground rules
- Standard library only unless a dependency clearly pays for itself; explain it in the PR.
- Never weaken a safety check to make a test pass. Add a test for every new rejection or edge case.
- Behaviour changes need README updates; do not document features that do not exist.
- Anything touching paths, backups, parsing or writing needs tests, and parser changes should keep `FuzzParse` passing.

## Workflow
```bash
gofmt -l .            # must print nothing (make fmt fixes it)
go vet ./...
go test ./...
go test -race ./...   # Linux/macOS
```
Keep patch logic (`internal/patch`, `internal/parser`) free of file and terminal I/O so it stays testable.

## Reporting bugs
Include the OS, `clip-patch --version`, the exact command, the full output, and a minimal patch plus file.
For security problems, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.
