package cli

import (
	"errors"
	"testing"
)

func TestParseReleaseVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want releaseVersion
		ok   bool
	}{
		{name: "zero", in: "0.0.0", want: releaseVersion{0, 0, 0}, ok: true},
		{name: "release", in: "1.2.3", want: releaseVersion{1, 2, 3}, ok: true},
		{name: "leading zeros", in: "001.02.0003", want: releaseVersion{1, 2, 3}, ok: true},
		{name: "dev", in: "dev"},
		{name: "git describe", in: "0.1.35-1-g85c8380"},
		{name: "too few", in: "1.2"},
		{name: "too many", in: "1.2.3.4"},
		{name: "negative", in: "-1.2.3"},
		{name: "plus sign", in: "+1.2.3"},
		{name: "empty component", in: "1..3"},
		{name: "whitespace", in: "1.2.3 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseReleaseVersion(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseReleaseVersion(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCompareReleaseVersions(t *testing.T) {
	tests := []struct {
		name string
		a    releaseVersion
		b    releaseVersion
		want int
	}{
		{name: "equal", a: releaseVersion{1, 2, 3}, b: releaseVersion{1, 2, 3}, want: 0},
		{name: "major less", a: releaseVersion{1, 9, 9}, b: releaseVersion{2, 0, 0}, want: -1},
		{name: "major greater", a: releaseVersion{2, 0, 0}, b: releaseVersion{1, 9, 9}, want: 1},
		{name: "minor less", a: releaseVersion{1, 2, 9}, b: releaseVersion{1, 3, 0}, want: -1},
		{name: "minor greater", a: releaseVersion{1, 3, 0}, b: releaseVersion{1, 2, 9}, want: 1},
		{name: "patch less", a: releaseVersion{1, 2, 3}, b: releaseVersion{1, 2, 4}, want: -1},
		{name: "patch greater", a: releaseVersion{1, 2, 4}, b: releaseVersion{1, 2, 3}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareReleaseVersions(tt.a, tt.b); got != tt.want {
				t.Fatalf("compareReleaseVersions(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestVersionGuardSentinels(t *testing.T) {
	if err := rejectVersionSkew("0.1.29", "0.1.34"); !errors.Is(err, errVersionSkew) {
		t.Fatalf("rejectVersionSkew error = %v, want errVersionSkew", err)
	}
	tests := []struct {
		name      string
		effective string
		override  string
		want      string
	}{
		{
			name:      "implicit binary version",
			effective: "0.1.35-1-g85c8380",
			want:      "refusing to replace a released cc-guides lock with an unreleased version: lock is 0.1.34, render would record 0.1.35-1-g85c8380; use a released cc-guides binary or pass --lock-version",
		},
		{
			name:      "explicit override",
			effective: "0.1.35-1-g85c8380",
			override:  "v0.1.35-1-g85c8380",
			want:      `refusing to replace a released cc-guides lock with an unreleased version: --lock-version "v0.1.35-1-g85c8380" is not a release version (want X.Y.Z)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectUnreleasedLockVersion(tt.effective, "0.1.34", tt.override)
			if !errors.Is(err, errUnreleasedLockVersion) {
				t.Fatalf("rejectUnreleasedLockVersion error = %v, want errUnreleasedLockVersion", err)
			}
			if err.Error() != tt.want {
				t.Fatalf("rejectUnreleasedLockVersion error = %q, want %q", err, tt.want)
			}
		})
	}
}
