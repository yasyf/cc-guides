# ![cc-guides](docs/assets/readme-banner.webp)

**Render your AGENTS.md. Stop hand-syncing it across repos.** cc-guides renders `AGENTS.md`, `CLAUDE.md`, and shell scripts from embedded, versioned fragments, and `cc-guides check` fails CI the moment an artifact drifts from its source.

[![Release](https://img.shields.io/github/v/release/yasyf/cc-guides?sort=semver)](https://github.com/yasyf/cc-guides/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/yasyf/cc-guides/ci.yml?branch=main&label=ci)](https://github.com/yasyf/cc-guides/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/yasyf/cc-guides/blob/main/LICENSE)

## Get started

```bash
brew install yasyf/tap/cc-guides
```

Write the parts you own and drop in a `{{> name}}` directive for each canonical guide. `render` expands the directives, stamps a GENERATED banner, and writes the sibling artifact:

```console
$ cat AGENTS.src.md
# Acme

House rules for agents.

{{> ccx}}

$ cc-guides render
rendered AGENTS.src.md -> AGENTS.md

$ head -3 AGENTS.md
<!-- cc-guides 0.1.0 src=AGENTS.src.md | GENERATED — do not edit: change AGENTS.src.md and run 'cc-guides render'. Everything below is in force. -->
# Acme

House rules for agents.
```

Everything below the banner is generated. You edit `AGENTS.src.md`; the `ccx` guide arrives verbatim from the binary, identical across every repo that renders it.

Driving with an agent? Paste this:

```text
Install cc-guides (`brew install yasyf/tap/cc-guides`), then migrate my
hand-written AGENTS.md to a rendered source: run `cc-guides init AGENTS.md`,
commit the AGENTS.src.md it produces, and wire `cc-guides check` into CI.
Reference: `cc-guides --help`.
```

---

## Use cases

### Migrate a hand-written guide you already have

Your `AGENTS.md` is full of canonical prose you pasted in by hand, and it has since drifted from the source it came from. `init` finds each recognized block, collapses it to a directive, and rewrites the file — refusing to touch anything it cannot reproduce byte-for-byte:

```console
$ cc-guides init AGENTS.md
VERIFIED	AGENTS.md
```

You get a clean `AGENTS.src.md`, holding the parts you own plus a `{{> name}}` for each guide, and a freshly rendered `AGENTS.md`. A block whose text no longer matches the canonical fragment is reported as `MISMATCH` and nothing is written, so a stale copy never migrates silently.

### Override one guide for a single repo

A repo needs its own spin on a shared guide without forking the whole set. Drop a file at `.claude/fragments/<name>.md` and it shadows the embedded body for that repo alone:

```console
$ printf '## Compact Context (ccx)\n\nLocal ccx rules for this repo.\n' > .claude/fragments/ccx.md
$ cc-guides render
```

The override renders wrapped in `<!-- local: .claude/fragments/ccx.md -->` markers, so anyone reading the artifact sees which guide is local and which came from the binary. Embedded fragments inline with no markers at all.

### Fail CI when an artifact drifts

An artifact edited by hand, or left stale after a fragment changed, should redden the build. `check` re-renders every source in memory and byte-compares it against what is on disk:

```console
$ cc-guides check
OK	AGENTS.md
STALE	install-binary.sh
$ echo "exit: $?"
exit: 1
```

`OK`, `STALE`, and `MISSING` go to stdout as TSV; exit 1 signals drift, exit 2 an invalid source. `cc-guides render && git diff --exit-code` is an equivalent idiom for the same gate.

## Sources and artifacts

Every source is named `X.src.<ext>` and renders to the sibling `X.<ext>`, for `<ext>` in `md` and `sh`. `AGENTS.src.md` becomes `AGENTS.md`; `install-binary.src.sh` becomes `install-binary.sh`. Run `render` with no arguments to walk the tree from the current directory and render every source it finds.

A source is your own prose plus column-0 directive lines. A directive pulls in one fragment; arguments after the name fill `{{token}}` placeholders inside that fragment's body:

```text
{{> ccx}}
{{> install-binary-pinned binary=slop-cop repo=yasyf/slop-cop brew=yasyf/tap/slop-cop plugin=slop-cop}}
```

Prose is never substituted. `{{VAR}}`, `{{#SECTION}}`, and `${{ github.sha }}` pass through byte-for-byte, and an inline mention of `{{> name}}` mid-sentence stays literal — only a directive at the start of a line expands. Every token needs a matching argument and every argument must be consumed, or render fails loud and names the offender.

The first comment line of every artifact is the banner: the machine contract that marks the file generated and records the source and the binary version that wrote it. `render` refuses to overwrite a handwritten file that carries no banner — that first handwritten-to-generated step is what `init` is for.

## Commands

One invocation per surface; run `cc-guides <command> --help` for the full flag list.

| Command | What it does |
|---|---|
| `render [paths…]` | Render each `X.src.{md,sh}` to its sibling artifact. No paths: discover and render every source under the working directory. |
| `check [paths…]` | Re-render in memory and byte-compare against disk. TSV `OK`/`STALE`/`MISSING` on stdout; exit 1 on drift, 2 on invalid input. |
| `init <artifact>` | Migrate a legacy stamped markdown artifact to a `.src.md` source, self-verifying the round-trip before writing. |
| `list` | List every fragment with its kind and origin: embedded, or a local override path. |
| `cat <name>` | Print a fragment's resolved body; `--embedded` ignores local overrides. |

`--version` prints the bare version that the banner records.

## Development

`cc-guides` is a Go module; build, test, and lint conventions live in [AGENTS.md](AGENTS.md).

```bash
task test   # go test -race ./...
task ci     # vet, lint, test, build
```

Licensed under [MIT](LICENSE).
