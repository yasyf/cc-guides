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
    for candidate in (start, *start.parents):
        if (candidate / ".claude" / "fragments" / "cc-guides.lock").is_file():
            return candidate
    return None


def repo_relative(entry: str) -> bool:
    return bool(entry) and entry.isprintable() and not (path := Path(entry)).is_absolute() and ".." not in path.parts


def lock_artifacts(root: Path) -> tuple[str, ...]:
    lock = root / ".claude" / "fragments" / "cc-guides.lock"
    try:
        data = tomllib.loads(lock.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return ()
    artifacts = data.get("artifacts")
    if not isinstance(artifacts, list):
        return ()
    return tuple(entry for entry in artifacts if isinstance(entry, str) and repo_relative(entry))


def layout_target(layout_dir: Path, fragments: Path) -> str:
    try:
        target = tomllib.loads((layout_dir / "layout.toml").read_text(encoding="utf-8")).get("target")
    except (OSError, ValueError):
        target = None
    return target if isinstance(target, str) else str(layout_dir.relative_to(fragments))


def fragment_dirs(root: Path, artifact: str) -> tuple[str, ...]:
    fragments = root / ".claude" / "fragments"
    return tuple(
        sorted(
            str(layout.parent.relative_to(fragments))
            for layout in fragments.rglob("layout.toml")
            if layout_target(layout.parent, fragments) == artifact
        )
    )


def matched_artifact(evt: BaseHookEvent) -> tuple[Path, str] | None:
    if (file := evt.file) is None or (cwd := evt.cwd) is None:
        return None
    target = (file.path if file.path.is_absolute() else cwd / file.path).resolve()
    if (root := lock_root(target.parent)) is None:
        return None
    if target.is_relative_to((root / ".claude" / "fragments").resolve()):
        return None
    for artifact in lock_artifacts(root):
        candidate = root / artifact
        if candidate.resolve() == target:
            return root, artifact
        if candidate.exists() and target.exists() and candidate.samefile(target):
            return root, artifact
    return None


class RenderedArtifact(CustomCondition):

    def check(self, evt: BaseHookEvent) -> bool:
        return matched_artifact(evt) is not None


@on(
    Event.PreToolUse,
    only_if=[Tool("Edit", "Write", "MultiEdit", "NotebookEdit"), RenderedArtifact()],
    tests={
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/AGENTS.md"),
            content="hand edit",
        ): Block(pattern=r"`AGENTS\.md` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/AGENTS\.md/`"),
        Input(
            tool="Write",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.claude/settings.json"),
            content="{}",
        ): Block(pattern=r"`\.claude/settings\.json` is cc-guides-rendered"),
        Input(
            tool="MultiEdit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            tool_input={
                "file_path": str(Path(__file__).parent / "tests/fixtures/managed/AGENTS.md"),
                "edits": [{"old_string": "a", "new_string": "b"}],
            },
        ): Block(pattern=r"`AGENTS\.md` is cc-guides-rendered"),
        Input(
            tool="NotebookEdit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            tool_input={
                "notebook_path": str(Path(__file__).parent / "tests/fixtures/managed/CLAUDE.md"),
                "new_source": "x",
            },
        ): Block(pattern=r"`CLAUDE\.md` is cc-guides-rendered"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed/.claude"),
            file="../AGENTS.md",
            content="escape attempt",
        ): Block(pattern=r"`AGENTS\.md` is cc-guides-rendered"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.gitignore"),
            content="hand edit",
        ): Block(pattern=r"`\.gitignore` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/gitignore/`"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/CLAUDE.md"),
            content="hand edit",
        ): Block(pattern=r"`CLAUDE\.md` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/CLAUDE\.md/`"),
        Input(
            tool="Write",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.mcp.json"),
            content="{}",
        ): Block(pattern=r"`\.mcp\.json` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/mcp\.json/`"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.github/workflows/docs.yml"),
            content="hand edit",
        ): Block(
            pattern=r"`\.github/workflows/docs\.yml` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/workflows/docs/`"
        ),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/managed"),
            file=str(Path(__file__).parent / "tests/fixtures/managed/.coveragerc"),
            content="hand edit",
        ): Block(
            pattern=r"`\.coveragerc` is cc-guides-rendered, and 2 fragment dirs claim it as their `target`\. Remove the duplicate"
        ),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/hostile"),
            file=str(Path(__file__).parent / "tests/fixtures/hostile/AGENTS.md"),
            content="hand edit",
        ): Block(pattern=r"^`AGENTS\.md` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/AGENTS\.md/`"),
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/hostile"),
            file=str(Path(__file__).parent / "tests/fixtures/hostile/CLAUDE.md"),
            content="hand edit",
        ): Block(pattern=r"^`CLAUDE\.md` is cc-guides-rendered\. Edit the fragments under `\.claude/fragments/CLAUDE\.md/`"),
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
        Input(
            tool="Edit",
            cwd=str(Path(__file__).parent / "tests/fixtures/badlock"),
            file=str(Path(__file__).parent / "tests/fixtures/badlock/AGENTS.md"),
            content="x",
        ): Allow(),
    },
)
def block_rendered_artifact_edit(evt: PreToolUseEvent) -> HookResult:
    root, artifact = matched_artifact(evt)
    match fragment_dirs(root, artifact) or (artifact,):
        case (only,):
            return evt.block(
                f"`{artifact}` is cc-guides-rendered. Edit the fragments under `.claude/fragments/{only}/`, then run `cc-guides render`."
            )
        case claimants:
            return evt.block(
                f"`{artifact}` is cc-guides-rendered, and {len(claimants)} fragment dirs claim it as their `target`. "
                "Remove the duplicate under `.claude/fragments/`, then run `cc-guides render`."
            )
