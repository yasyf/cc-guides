package cli_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/yasyf/cc-guides/internal/version"
	"github.com/yasyf/cc-guides/lockfile"
)

const versionGuardDir = ".claude/fragments/AGENTS.md"

func setupVersionGuardRepo(t *testing.T, lockedVersion string) {
	t.Helper()
	repo(t)
	write(t, versionGuardDir+"/layout.toml", "fragments = [\"intro\"]\n")
	write(t, versionGuardDir+"/intro.fragment.md", "# Repo\n")
	write(t, "AGENTS.md", mdMarker(versionGuardDir)+"\n# Repo\n")
	lock := &lockfile.Lock{Schema: 1, Version: lockedVersion, Artifacts: []string{"AGENTS.md"}, Sources: map[string]lockfile.SourcePin{}}
	write(t, lockfile.Path, string(lock.Encode()))
}

func setBinaryVersion(t *testing.T, v string) {
	t.Helper()
	previous := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = previous })
}

func lockedVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(lockfile.Path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return lock.Version
}

func TestVersionSkewGuardE2E(t *testing.T) {
	const olderError = "cc-guides: artifacts were last rendered by cc-guides 99.0.0; this binary is 0.1.35 — upgrade: brew upgrade cc-guides\n"
	const incidentError = "cc-guides: artifacts were last rendered by cc-guides 0.1.34; this binary is 0.1.29 — upgrade: brew upgrade cc-guides\n"
	const unreleasedError = "cc-guides: refusing to replace a released cc-guides lock with an unreleased version: lock is 0.1.34, render would record 0.1.35-1-g85c8380; use a released cc-guides binary or pass --lock-version\n"
	const devUnreleasedError = "cc-guides: refusing to replace a released cc-guides lock with an unreleased version: lock is 0.1.34, render would record dev; use a released cc-guides binary or pass --lock-version\n"
	const overrideUnreleasedError = "cc-guides: refusing to replace a released cc-guides lock with an unreleased version: --lock-version \"0.1.35-1-g85c8380\" is not a release version (want X.Y.Z)\n"
	const rendered = "rendered .claude/fragments/AGENTS.md -> AGENTS.md\n"

	tests := []struct {
		name              string
		binary            string
		locked            string
		args              []string
		wantCode          int
		wantStdout        string
		wantStderr        string
		wantLocked        string
		wantLockUnchanged bool
	}{
		{
			name:       "render refuses older binary",
			binary:     "0.1.35",
			locked:     "99.0.0",
			args:       []string{"render"},
			wantCode:   2,
			wantStderr: olderError,
			wantLocked: "99.0.0",
		},
		{
			name:       "check refuses older binary",
			binary:     "0.1.35",
			locked:     "99.0.0",
			args:       []string{"check"},
			wantCode:   2,
			wantStderr: olderError,
			wantLocked: "99.0.0",
		},
		{
			name:       "render equal version proceeds",
			binary:     "0.1.35",
			locked:     "0.1.35",
			args:       []string{"render"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
		{
			name:       "check equal version proceeds",
			binary:     "0.1.35",
			locked:     "0.1.35",
			args:       []string{"check"},
			wantStdout: "OK\tAGENTS.md\n",
			wantLocked: "0.1.35",
		},
		{
			name:       "render newer binary heals lock",
			binary:     "0.1.35",
			locked:     "0.1.34",
			args:       []string{"render"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
		{
			name:       "check newer binary proceeds",
			binary:     "0.1.35",
			locked:     "0.1.34",
			args:       []string{"check"},
			wantStdout: "OK\tAGENTS.md\n",
			wantLocked: "0.1.34",
		},
		{
			name:       "scoped render incident refuses downgrade",
			binary:     "0.1.29",
			locked:     "0.1.34",
			args:       []string{"render", versionGuardDir},
			wantCode:   2,
			wantStderr: incidentError,
			wantLocked: "0.1.34",
		},
		{
			name:              "empty override does not bypass older binary guard",
			binary:            "0.1.29",
			locked:            "0.1.34",
			args:              []string{"render", "--lock-version", ""},
			wantCode:          2,
			wantStderr:        incidentError,
			wantLocked:        "0.1.34",
			wantLockUnchanged: true,
		},
		{
			name:       "dev lock heals",
			binary:     "0.1.35",
			locked:     "dev",
			args:       []string{"render"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
		{
			name:       "pseudo lock heals",
			binary:     "0.1.35",
			locked:     "0.1.34-1-g85c8380",
			args:       []string{"render"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
		{
			name:       "pseudo binary render refuses clean lock",
			binary:     "0.1.35-1-g85c8380",
			locked:     "0.1.34",
			args:       []string{"render"},
			wantCode:   2,
			wantStderr: unreleasedError,
			wantLocked: "0.1.34",
		},
		{
			name:       "dev warning is suppressed when render is refused",
			binary:     "dev",
			locked:     "0.1.34",
			args:       []string{"render"},
			wantCode:   2,
			wantStderr: devUnreleasedError,
			wantLocked: "0.1.34",
		},
		{
			name:       "unreleased lock override reports the flag value",
			binary:     "0.1.35",
			locked:     "0.1.34",
			args:       []string{"render", "--lock-version", "0.1.35-1-g85c8380"},
			wantCode:   2,
			wantStderr: overrideUnreleasedError,
			wantLocked: "0.1.34",
		},
		{
			name:       "pseudo binary check proceeds",
			binary:     "0.1.35-1-g85c8380",
			locked:     "0.1.34",
			args:       []string{"check"},
			wantStdout: "OK\tAGENTS.md\n",
			wantLocked: "0.1.34",
		},
		{
			name:       "released lock override permits pseudo binary render",
			binary:     "0.1.35-1-g85c8380",
			locked:     "0.1.34",
			args:       []string{"render", "--lock-version", "0.1.35"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
		{
			name:       "override bypasses older binary guard",
			binary:     "0.1.35",
			locked:     "99.0.0",
			args:       []string{"render", "--lock-version", "0.1.35"},
			wantStderr: rendered,
			wantLocked: "0.1.35",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupVersionGuardRepo(t, tt.locked)
			beforeLock, err := os.ReadFile(lockfile.Path)
			if err != nil {
				t.Fatal(err)
			}
			setBinaryVersion(t, tt.binary)
			code, stdout, stderr := exec(tt.args...)
			if code != tt.wantCode || stdout != tt.wantStdout || stderr != tt.wantStderr {
				t.Fatalf("exec(%q) = code %d, stdout %q, stderr %q; want code %d, stdout %q, stderr %q", tt.args, code, stdout, stderr, tt.wantCode, tt.wantStdout, tt.wantStderr)
			}
			if got := lockedVersion(t); got != tt.wantLocked {
				t.Fatalf("lock version = %q, want %q", got, tt.wantLocked)
			}
			if tt.wantLockUnchanged {
				afterLock, err := os.ReadFile(lockfile.Path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(afterLock, beforeLock) {
					t.Fatalf("lock changed:\nbefore:\n%s\nafter:\n%s", beforeLock, afterLock)
				}
			}
		})
	}
}
