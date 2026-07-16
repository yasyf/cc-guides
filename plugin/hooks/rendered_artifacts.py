"""Block a direct edit to a cc-guides-rendered artifact, steering to the fragments + render flow.

Every managed artifact (``AGENTS.md``, ``CLAUDE.md``, ``.claude/settings.json``, …) is generated
by ``cc-guides render`` from ``.claude/fragments/<artifact>/`` — the ``*.fragment.*`` parts plus a
``layout.toml`` — and a direct edit to the artifact is discarded on the next render. The authoritative
list of generated artifacts is the ``artifacts`` array in the repo's ``.claude/fragments/cc-guides.lock``:
it is the ONLY managed-file signal, because a JSON artifact (``.claude/settings.json``) carries no
in-file marker a banner check could read. So an ``Edit``/``Write``/``MultiEdit``/``NotebookEdit`` whose
target aliases one of the lock's ``artifacts`` is blocked, pointing at the fragments and
``cc-guides render``. A repo with no (or a malformed) lock, a path the lock does not list, or a render
SOURCE under ``.claude/fragments/`` is left alone.
"""

from __future__ import annotations

import tomllib
from pathlib import Path

from captain_hook import (
    Allow,
    BaseHookEvent,
    Block,
    CustomCondition,
    Event,
    HookResult,
    Input,
    PreToolUseEvent,
    Tool,
    on,
)


def lock_root(start: Path) -> Path | None:
    """The nearest ancestor of *start* carrying ``.claude/fragments/cc-guides.lock``, or None."""
    for candidate in (start, *start.parents):
        if (candidate / ".claude" / "fragments" / "cc-guides.lock").is_file():
            return candidate
    return None


def lock_artifacts(root: Path) -> tuple[str, ...]:
    """The ``artifacts`` the lock at *root* lists — () for a missing, malformed, or mis-shaped lock."""
    lock = root / ".claude" / "fragments" / "cc-guides.lock"
    try:
        data = tomllib.loads(lock.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError):
        return ()
    artifacts = data.get("artifacts")
    if not isinstance(artifacts, list):
        return ()
    return tuple(entry for entry in artifacts if isinstance(entry, str))


def matched_artifact(evt: BaseHookEvent) -> str | None:
    """The lock ``artifacts`` entry the edit target aliases, or None when the edit is unmanaged.

    The target (``file_path`` for Edit/Write/MultiEdit, ``notebook_path`` for NotebookEdit) resolves
    against the session cwd, and the lock is found by walking the TARGET's ancestors — a cwd inside a
    subdirectory or a ``../``-shaped path cannot escape the guard. Membership is by inode
    (``samefile``, which also catches case-insensitive and symlink aliases) with a resolved-path
    fallback for a target that does not exist yet. A file under the lock root's
    ``.claude/fragments/`` is a render SOURCE, never an artifact.
    """
    if (file := evt.file) is None or (cwd := evt.cwd) is None:
        return None
    target = (file.path if file.path.is_absolute() else cwd / file.path).resolve()
    if (root := lock_root(target.parent)) is None:
        return None
    if target.is_relative_to((root / ".claude" / "fragments").resolve()):
        return None
    for artifact in lock_artifacts(root):
        candidate = root / artifact
        try:
            if candidate.samefile(target):
                return artifact
        except OSError:
            pass
        if candidate.resolve() == target:
            return artifact
    return None


class RenderedArtifact(CustomCondition):
    """True when the edit target aliases one of the artifacts the repo's cc-guides.lock lists."""

    def check(self, evt: BaseHookEvent) -> bool:
        return matched_artifact(evt) is not None


@on(
    Event.PreToolUse,
    only_if=[Tool("Edit", "Write", "MultiEdit", "NotebookEdit"), RenderedArtifact()],
    tests={
        # denies: lock-listed artifacts across every tool payload shape, incl. a ../ path from a subdir cwd
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/AGENTS.md"),
            content="hand edit",
        ): Block(pattern=r"AGENTS\.md is cc-guides-rendered — edit the fragments under \.claude/fragments/AGENTS\.md/"),
        Input(
            tool="Write",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.claude/settings.json"),
            content="{}",
        ): Block(pattern=r"\.claude/settings\.json is cc-guides-rendered"),
        Input(
            tool="MultiEdit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            tool_input={
                "file_path": str(Path(__file__).parent / "tests/fixtures/managed/AGENTS.md"),
                "edits": [{"old_string": "a", "new_string": "b"}],
            },
        ): Block(pattern=r"AGENTS\.md is cc-guides-rendered"),
        Input(
            tool="NotebookEdit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            tool_input={
                "notebook_path": str(Path(__file__).parent / "tests/fixtures/managed/CLAUDE.md"),
                "new_source": "x",
            },
        ): Block(pattern=r"CLAUDE\.md is cc-guides-rendered"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed/.claude"),
            file="../AGENTS.md",
            content="escape attempt",
        ): Block(pattern=r"AGENTS\.md is cc-guides-rendered"),
        # allows: unlisted file, render source, no lock, mis-shaped lock
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/internal/cli/root.go"),
            content="x",
        ): Allow(),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.claude/fragments/AGENTS.md/part-1.fragment.md"),
            content="x",
        ): Allow(),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/unmanaged"),
            file=str(Path(__file__).parent / "tests/fixtures/unmanaged/AGENTS.md"),
            content="x",
        ): Allow(),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/malformed"),
            file=str(Path(__file__).parent / "tests/fixtures/malformed/AGENTS.md"),
            content="x",
        ): Allow(),
    },
)
def block_rendered_artifact_edit(evt: PreToolUseEvent) -> HookResult:
    """Block the edit, naming the artifact and its fragment dir so the agent re-renders instead."""
    artifact = matched_artifact(evt)
    return evt.block(
        f"{artifact} is cc-guides-rendered — edit the fragments under .claude/fragments/{artifact}/ "
        f"and run `cc-guides render`; commit fragments, artifacts, and lock together."
    )
