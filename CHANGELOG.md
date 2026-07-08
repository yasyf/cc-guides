# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release: render canonical agent guides from embedded, versioned fragments.
- `render` expands column-0 `{{> name}}` directives in `X.src.{md,sh}` sources to their sibling `X.{md,sh}` artifacts, stamping a GENERATED banner.
- `check` re-renders in memory and byte-compares against disk, emitting `OK`/`STALE`/`MISSING` TSV rows (exit 1 on drift, 2 on invalid input).
- `init` migrates a legacy stamped markdown artifact to a `.src.md` source, self-verifying the round-trip before writing.
- `list` and `cat` inspect the available fragments; local overrides in `.claude/fragments/` shadow the embedded bodies and render with `local:` provenance markers.
- Directive arguments substitute `{{token}}` placeholders inside fragment bodies; source prose is never touched.
- Embedded fragments: the six canonical markdown guides (`ask-before-assuming`, `ccx`, `code-review-response`, `parallelize`, `version-control`, `writing-plans`) and two parameterized shell fragments (`install-binary-pinned`, `install-binary-latest`).
- Fleet CI: a composite Guides action (`yasyf/cc-guides@action-v1`) that checks artifacts with the exact version their banner records, a reusable re-render workflow with a banner-only diff gate, and a release fan-out that dispatches `cc-guides-render` to every `fleet.json` repo.

[Unreleased]: https://github.com/yasyf/cc-guides/commits/main
