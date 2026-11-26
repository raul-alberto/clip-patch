# Publication plan (do not publish before your own manual testing)

## Pre-publication checklist
- [ ] `make fmt && make all` passes locally; `make race` and `make fuzz` run clean
- [ ] `docs/MANUAL_TEST.md` completed on every OS you claim to support
- [ ] Pick a license, add `LICENSE` with your real name/year, update README
- [ ] Replace module path: `go mod edit -module github.com/<you>/clip-patch` and fix imports (`grep -rl '"clip-patch/' . | xargs sed -i 's#"clip-patch/#"github.com/<you>/clip-patch/#'`)
- [ ] Update `docs/VERIFICATION.md` and README status line with real results
- [ ] CI is green on Windows, Linux and macOS
- [ ] Add a private-contact or enable private vulnerability reporting (SECURITY.md)

## Screenshots (real output only; terminal at ~100x30, readable font)
| File (in `docs/media/`) | Command | Must show |
|---|---|---|
| `01-detect.png` | `cpatch --dry-run` with `examples/search-replace.patch` copied | header block: format detected, files, changes |
| `02-preview.png` | same command, scrolled to the diff | red/green before-and-after |
| `03-applied.png` | `cpatch`, answer `y` | `Applied changes` and the backup path |
| `04-rejected.png` | `cpatch --stdin --yes < examples/invalid-search-replace.patch`, then `cat greeter.py` | error + "No files were modified" + untouched file |
Use any terminal (Windows Terminal, iTerm2, GNOME Terminal), dark theme, no personal paths visible.

## Demo GIF
`docs/media/demo.gif`, 20-30 s, ~900 px wide, from `vhs` (scriptable) or `asciinema` + `agg`; keep under ~2 MB.
Script: show `cat greeter.py` -> copy patch (show it briefly in an editor or chat window) -> run `cpatch` -> diff appears ->
press `y` -> show backup path -> `python3 greeter.py`. Put it directly under the first code block of the README.

## Repository metadata
- Description: "Safely apply AI-generated code patches from your clipboard: preview, validate, back up, apply. Local-first, no API keys."
- Topics: `cli`, `go`, `golang`, `clipboard`, `patch`, `diff`, `ai-tools`, `developer-tools`, `code-review`, `llm`
- Versioning: SemVer; first tag `v0.1.0` only after manual testing. Release name: "v0.1.0 - first MVP".
- Release flow: push the tag; `release.yml` builds binaries and creates a **draft** release for you to review.

## Launch text (short)
> I built clip-patch, a small local Go CLI that applies AI-generated patches from your clipboard. It validates exact
> context, shows a diff, backs up files, and refuses ambiguous or out-of-tree changes. No API keys or network.
> Supports SEARCH/REPLACE blocks and unified diffs on Windows, macOS and Linux. Feedback and bug reports welcome: <repo link>

Possible places (read each community's self-promotion rules first): r/golang, r/commandline, Hacker News "Show HN",
lobste.rs, Dev.to. No outcome is guaranteed; a short honest README and a working demo matter most.
