# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
