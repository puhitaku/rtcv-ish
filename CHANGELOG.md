# Changelog

Each release has a section headed `## <tag>` (for example `## v1.0.0` or
`## v1.0.0-rc1`), newest first. The release workflow copies the body of the
section matching the pushed tag into the GitHub release notes and fails if
that section is missing or empty, so add it before tagging. Anything after
the tag on the heading line (such as ` (2026-01-31)`) is ignored.

Write entries for users: what changed and why it matters, not a commit
list. Collect changes under `## Unreleased` and rename that heading to the
tag when releasing.

## Unreleased

- First release: Go core with the melonDS fork for Linux, macOS and Windows.
