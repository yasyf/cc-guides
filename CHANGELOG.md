# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The v1 removal release. cc-guides is now lock-only: every consumer carries a
`.claude/fragments/cc-guides.lock` (the whole fleet already does), and the legacy
`.src` render path is gone.

### Removed (breaking)
- **The v1 `.src` render path.** `render` and `check` no longer discover or process
  `X.src.{md,sh}` sources or their `{{> name}}` include directives. Artifacts come
  only from `.claude/fragments/<target>/` layout dirs.
- **The `migrate` and `init` commands.** Both existed solely to convert legacy
  artifacts (v1 `.src` sources, hand-written stamped artifacts) into v3 layouts;
  that conversion is complete fleet-wide and repo-bootstrap scaffolds native layouts.
- **Legacy banner recognition.** `render` and `check` no longer read the old
  per-artifact `cc-guides … | GENERATED` banner. A markdown/shell artifact is
  cc-guides-managed only via its version-free `GENERATED` marker; drift is checked
  only against the lock. An artifact dir with no lock entry is now a hard
  invalid-input error telling you to run `cc-guides render`.
- **The `Guides check` action's banner fallback.** The action now requires a tracked
  `cc-guides.lock`; without one it fails with a message to render with a
  `>= 0.1.13` binary. The reusable re-render workflow is lock-driven only.

A repo last rendered by `0.1.12` or earlier adopts the lock with a single
`cc-guides render` against a current binary (it rewrites each artifact with a marker
and writes the lock). Existing pinned action SHAs and older `cc-guides` releases keep
working unchanged.

## [0.1.13]

The pull-model release: layouts address shared packs by manifest, provenance moves
to a lock file, and `.claude/settings.json` becomes a rendered JSON target.

### Changed (breaking)
- **No default source.** The baked-in `cc-skills` alias is gone. Every layout that imports a shared fragment must declare its `[sources.<alias>]` table; an undeclared alias is a hard error. `layout.toml` encoding now emits every source.
- **Manifest-form specs.** A source spec may now be `github:<owner>/<repo>[@<ref>]` (no `//path`). The resolver follows the target repo's `.claude/cc-guides.toml` (root `cc-guides.toml` fallback) to the pack's guides dir. The explicit-path form `github:<owner>/<repo>//<path>[@<ref>]` still works.
- **Lock file, not banners.** `render` writes `.claude/fragments/cc-guides.lock` recording the render version and one commit per source alias. `check` pins off the lock. Markdown and shell artifacts carry a version-free `GENERATED` marker; a code-only release now re-renders byte-identically. Legacy banners are still recognized during the transition (with a deprecation warning).

### Added
- **JSON render targets.** A `json` fragment kind deep-merges local and imported JSON fragments (objects recursively, arrays as a structural-equality union, scalars later-wins) into a marker-free artifact — so `.claude/settings.json` can be composed from shared fragments. `lint` validates `json/` fragments as well-formed objects.
- The `Guides check` action reads the lock (installing exactly its version and refusing a `dev` version or a `local` commit) and falls back to the banner path for un-migrated repos; the re-render workflow skips a lock-only diff.

[Unreleased]: https://github.com/yasyf/cc-guides/commits/main
[0.1.13]: https://github.com/yasyf/cc-guides/releases/tag/v0.1.13
