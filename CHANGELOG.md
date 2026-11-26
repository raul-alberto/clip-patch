# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - not yet released

Initial MVP (release pending the author's manual testing).

### Added
- Read patches from the clipboard (Windows, macOS, Linux X11/Wayland) or `--stdin`.
- SEARCH / REPLACE and unified diff formats, with auto-detection.
- Exact-match validation; ambiguous or missing context aborts the whole patch.
- Colour-optional unified diff preview, `--dry-run`, interactive confirmation, `--yes`.
- Automatic verified backups with a manifest, in the per-user data directory or `--backup-dir`.
- Path confinement to `--root`: rejects traversal, absolute/UNC/drive paths, symlinks, `.git`.
- Preservation of UTF-8 BOM, CRLF/LF line endings and final-newline state.
- Fingerprint re-check before writing; staged writes with rollback on failure.
