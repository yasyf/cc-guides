# cc-guides Development Guide

Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and other repo artifacts from versioned, composable fragments. Distributed via Homebrew: `brew install yasyf/tap/cc-guides`.

## Repository Structure

```
cc-guides/
├── cmd/cc-guides/         # main package — the CLI entry point
├── guide/                 # artifact kinds: spec registry, compose, markers, validation
├── layout/                # layout.toml parsing
├── lockfile/              # cc-guides.lock read/merge/write
├── source/                # github:owner/repo fragment resolution + cache
├── internal/
│   ├── cli/               # cobra command tree: render, check, lint, ci-render, list, cat
│   ├── version/           # build version, stamped via -ldflags
│   └── log/               # slog setup
├── action.yml             # the consumer "Guides check" composite action (@action-v1)
├── install/               # release-binary install action
├── .github/               # workflows: CI, release, reusable re-render
├── AGENTS.md              # This file — shared conventions
└── README.md              # Project overview
```
