# Security policy

## Scope
clip-patch modifies local files based on untrusted text. Reports about path confinement
(traversal, symlinks, `.git`), parser denial of service, data loss, or backup/rollback failures are in scope.

## Reporting
Please report privately rather than in a public issue. Once the repository is published, use
GitHub's "Report a vulnerability" (private security advisories). *(Maintainer: enable this
feature and/or add a contact address here before publishing.)*

## Design notes
See the "Security model" and its "Honest limits" in the README. In short: no network access,
clipboard text is data only, all paths are validated, backups are verified before writes, and
multi-file replacement is best-effort (not globally atomic).

## Supported versions
Only the latest release receives fixes.
