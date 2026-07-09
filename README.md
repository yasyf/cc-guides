# ![cc-guides](docs/assets/readme-banner.webp)

**Write the prose you own; import the guides you share.** cc-guides composes `AGENTS.md`, `CLAUDE.md`, and shell scripts from local fragments and shared guides pulled in by reference, and `cc-guides check` fails CI the moment an artifact drifts.

[![Release](https://img.shields.io/github/v/release/yasyf/cc-guides?sort=semver)](https://github.com/yasyf/cc-guides/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/yasyf/cc-guides/ci.yml?branch=main&label=ci)](https://github.com/yasyf/cc-guides/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/yasyf/cc-guides/blob/main/LICENSE)

## Get started

```bash
brew install yasyf/tap/cc-guides
```

A repo describes each generated file with a `.claude/fragments/<target>/` directory: a `layout.toml` that composes local `*.fragment.*` prose with imports of shared guides, plus the local pieces. `render` fetches each import at a pinned commit, stamps a GENERATED banner, and writes the target:

```console
$ cat .claude/fragments/AGENTS.md/intro.fragment.md
# Acme

House rules for agents.

$ cat .claude/fragments/AGENTS.md/layout.toml
fragments = [
  "intro",
  "cc-skills:ccx",
]

$ cc-guides render
rendered .claude/fragments/AGENTS.md -> AGENTS.md

$ head -6 AGENTS.md
<!-- cc-guides <version> src=.claude/fragments/AGENTS.md fragments=cc-skills@<sha12> | GENERATED — do not edit: edit .claude/fragments/AGENTS.md/ and run 'cc-guides render'. Everything below is in force. -->
# Acme

House rules for agents.

## Compact Context (ccx)
```

`intro` is your prose; `cc-skills:ccx` arrives verbatim from the shared guides in [cc-skills](https://github.com/yasyf/cc-skills), identical across every repo that imports it. Change the shared guide once and every repo re-renders.

Driving with an agent? Paste this:

```text
Install cc-guides (`brew install yasyf/tap/cc-guides`). I have a hand-written
AGENTS.md — migrate it: run `cc-guides init AGENTS.md`, commit the
.claude/fragments/AGENTS.md/ directory it produces, and wire `cc-guides check`
into CI. Reference: `cc-guides --help`.
```

---

## Use cases

### Fail CI when an artifact drifts

An artifact edited by hand, or left stale after a shared guide changed, should redden the build. `check` re-composes each target in memory — pinned to the shas its own banner records — and byte-compares against disk:

```console
$ cc-guides check
OK	AGENTS.md

$ echo "agent slop" >> AGENTS.md
$ cc-guides check
STALE	AGENTS.md
$ echo "exit: $?"
exit: 1
```

`OK`, `STALE`, and `MISSING` go to stdout as TSV; exit 1 signals drift, 2 an invalid layout. In GitHub Actions, one step gates every artifact:

```yaml
- uses: actions/checkout@v7
- uses: yasyf/cc-guides@action-v1
```

The action installs the exact cc-guides version each banner records, so a new release never reddens a repo that has not re-rendered yet.

### Keep one repo's spin on a shared guide

A repo needs its own version of a guide the rest of the fleet shares. Compose a local fragment in the slot where the import would sit — no import, no shadowing:

```toml
fragments = [
  "intro",
  "ccx",          # local ccx.fragment.md, not cc-skills:ccx
]
```

`render` reads `ccx.fragment.md` from the artifact dir instead of fetching the shared guide, and the banner records `fragments=none` when a layout imports nothing.

### Migrate an existing repo

A repo already rendered by an older cc-guides, or holding a hand-pasted guide, moves to the layout shape in one command. `migrate` converts a v1 `X.src.md` source; `init` converts a hand-written stamped artifact. Both write the `.claude/fragments/<target>/` directory and self-verify the composition reproduces the current artifact byte-for-byte, refusing to write on a mismatch:

```console
$ cc-guides migrate AGENTS.src.md
MIGRATED	AGENTS.src.md -> .claude/fragments/AGENTS.md/
```

## Composition

An artifact dir is any directory under `.claude/fragments/` that holds a `layout.toml`, and its path below that root is the target it renders. `.claude/fragments/AGENTS.md/` renders `AGENTS.md`; a nested path renders a nested target. The kind — Markdown or shell comment style — comes from the target extension.

`layout.toml` is an ordered, heterogeneous `fragments` array. The array comes first, before any `[sources.*]` table: a top-level key written after a table header nests inside that table, and the binary hard-errors on that shape instead of composing empty.

```toml
fragments = [
  "agents-md",                       # local: agents-md.fragment.md in this dir
  "cc-skills:ccx",                   # import: guides/md/ccx.md from cc-skills
  { use = "cc-skills:install-binary-latest", args = { binary = "slop-cop", plugin = "slop-cop", repo = "yasyf/slop-cop", brew = "yasyf/tap/slop-cop" } },
]

[sources.cc-skills]                  # optional — this exact table is the baked default
source = "github:yasyf/cc-skills//guides@main"
```

A string entry is a local `<name>.fragment.<ext>` or an `alias:name` import; an inline table adds `args` that fill `{{token}}` placeholders in the imported body. The `cc-skills` alias resolves to the shared guides by default, so most layouts drop the `[sources]` table. Pieces join with one blank line between them, LF only, one trailing newline. Prose is never token-substituted, so `${{ github.sha }}` and `{{VAR}}` survive verbatim.

## Commands

One invocation per surface; run `cc-guides <command> --help` for the full flag list.

| Command | What it does |
|---|---|
| `render [paths…]` | Compose each artifact dir to its target. No paths: discover every layout under the repo. |
| `check [paths…]` | Re-compose in memory, pinned to each banner's shas, and byte-compare. TSV `OK`/`STALE`/`MISSING`; exit 1 on drift, 2 on invalid input. |
| `migrate [paths…]` | Convert a v1 `X.src.{md,sh}` source to an artifact dir, self-verifying the round-trip. |
| `init <artifact>` | Convert a hand-written stamped markdown artifact to an artifact dir. |
| `lint <dir>` | Check a shared-guides directory for purity: LF, one trailing newline, kind, shell shebang. |
| `list` | List each artifact dir and the fragments it composes. |
| `cat <ref>` | Print a fragment body: an `alias:name` import or a local piece by name. |

`--source alias=<spec>` overrides where an import resolves — a `github:` spec or a local directory for development. `--version` prints the version the banner records.

## How it fits together

cc-guides ships two things: this binary and an importable GitHub Action. The content lives in its consumers — [cc-skills](https://github.com/yasyf/cc-skills) is the reference home for the shared guides, and each repo carries its own layouts. An import resolves to an immutable commit through `git ls-remote`, fetches that tree from codeload, and caches it under the user cache dir; every artifact in a run pins the same sha. `check` reads the shas straight off each banner, so it reproduces an artifact offline once the cache is warm and never false-fails across binary versions. Build, test, and lint conventions live in [AGENTS.md](AGENTS.md).

```bash
task test   # go test -race ./...
task ci     # vet, lint, test, build
```

Licensed under [MIT](LICENSE).
