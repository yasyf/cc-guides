package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/source"
)

type checkOpts struct {
	diff    bool
	sources []string
}

func newCheckCmd(ctx context.Context) *cobra.Command {
	var o checkOpts
	cmd := &cobra.Command{
		Use:   "check [paths...]",
		Short: "Verify artifacts are in sync with their layouts (TSV STATUS on stdout)",
		Long: "Re-compose each artifact in memory — pinned to the shas its own banner\n" +
			"records — and byte-compare it against the file on disk. Emit one TSV row\n" +
			"per artifact: OK, STALE, or MISSING. Exit 1 on any drift, 2 on invalid\n" +
			"input. With no paths, discover artifact dirs and transitional sources from\n" +
			"the repo root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(ctx, cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.diff, "diff", false, "print a unified diff to stderr for each STALE artifact")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runCheck(ctx context.Context, cmd *cobra.Command, args []string, o checkOpts) error {
	stderr := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()
	root := repoRoot()
	overrides, err := parseSourceOverrides(o.sources)
	if err != nil {
		return exit(2, err)
	}

	v3dirs, v1srcs, err := collectUnits(root, args)
	if err != nil {
		return exit(2, err)
	}
	if len(v3dirs)+len(v1srcs) == 0 {
		foutln(stderr, "cc-guides: no artifact dirs or *.src.* sources found")
		return nil
	}
	if err := preflightTargets(v3dirs, v1srcs); err != nil {
		return exit(2, err)
	}

	worst := 0
	bump := func(code int) {
		if worst < code {
			worst = code
		}
	}
	for _, dir := range v3dirs {
		status, path, invalid, cerr := checkV3Dir(ctx, root, dir, overrides, o.diff, stderr)
		record(out, stderr, dir, status, path, invalid, cerr, bump)
	}
	for _, src := range v1srcs {
		status, path, invalid, cerr := checkV1Src(ctx, root, src, overrides, o.diff, stderr)
		record(out, stderr, src, status, path, invalid, cerr, bump)
	}
	if worst == 0 {
		return nil
	}
	return silent(worst)
}

// record emits one row (or an invalid-input diagnostic) and updates the worst code.
func record(out, stderr io.Writer, label, status, path string, invalid bool, err error, bump func(int)) {
	if invalid {
		fout(stderr, "cc-guides: %s: %v\n", label, err)
		bump(2)
		return
	}
	fout(out, "%s\t%s\n", status, path)
	if status != "OK" {
		bump(1)
	}
}

// checkV3Dir re-composes one artifact dir pinned to its banner's shas and compares
// it to disk. The reproduction stamps the banner's own version + fragments strings
// verbatim, so a cross-version check never false-STALEs on the version token.
func checkV3Dir(ctx context.Context, root, dir string, overrides map[string]string, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	ad, err := loadArtifactDir(root, dir)
	if err != nil {
		return "", dir, true, err
	}
	abs := filepath.Join(root, filepath.FromSlash(ad.target))
	disk, err := os.ReadFile(abs) // #nosec G304 -- reads the artifact target of a discovered dir
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING", ad.target, false, nil
		}
		return "", ad.target, true, err
	}
	info, ok := guide.ParseBanner(ad.kind, disk)
	if !ok {
		return "", ad.target, true, fmt.Errorf("%s has no cc-guides banner (run 'cc-guides render')", ad.target)
	}
	resolver, err := source.New(source.Options{Specs: mergeSpecs(ad.lay.Sources, overrides), Pinned: parsePins(info.Fragments)})
	if err != nil {
		return "", ad.target, true, err
	}
	body, err := ad.compose(ctx, resolver)
	if err != nil {
		return "", ad.target, true, err
	}
	diskBody, _ := guide.StripBanner(ad.kind, disk)
	return compareBodies(ad.target, diskBody, body, diff, stderr), ad.target, false, nil
}

// checkV1Src verifies a transitional v1 source's artifact, resolving directives
// against the shas its banner pins.
func checkV1Src(ctx context.Context, root, src string, overrides map[string]string, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	abs := filepath.Join(root, filepath.FromSlash(src))
	raw, err := os.ReadFile(abs) // #nosec G304 -- check reads a discovered/user-named source
	if err != nil {
		return "", src, true, err
	}
	kind, err := guide.KindForPath(src)
	if err != nil {
		return "", src, true, err
	}
	doc, err := guide.Parse(raw, kind)
	if err != nil {
		return "", src, true, err
	}
	artifactRel, err := guide.ArtifactPath(src)
	if err != nil {
		return "", src, true, err
	}
	disk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifactRel))) // #nosec G304 -- reads the artifact sibling of a source
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING", artifactRel, false, nil
		}
		return "", artifactRel, true, err
	}
	info, ok := guide.ParseBanner(kind, disk)
	if !ok {
		return "", artifactRel, true, fmt.Errorf("%s has no cc-guides banner (run 'cc-guides render')", artifactRel)
	}
	resolver, err := newV1Resolver(overrides, parsePins(info.Fragments))
	if err != nil {
		return "", artifactRel, true, err
	}
	chain, err := v1Chain(ctx, root, doc, kind, resolver)
	if err != nil {
		return "", artifactRel, true, err
	}
	body, err := guide.Render(doc, chain)
	if err != nil {
		return "", artifactRel, true, err
	}
	diskBody, _ := guide.StripBanner(kind, disk)
	return compareBodies(artifactRel, diskBody, body, diff, stderr), artifactRel, false, nil
}

// compareBodies byte-compares the artifact's on-disk body-after-banner against the
// recomposed body. It never reconstructs the banner, so a v1 banner is echoed
// verbatim (no false STALE from v1→v2 re-serialization) — the true self-pinning
// semantics: only the body is compared, the banner is trusted.
func compareBodies(label string, diskBody, composed []byte, diff bool, stderr io.Writer) string {
	if bytes.Equal(diskBody, composed) {
		return "OK"
	}
	if diff {
		fout(stderr, "%s", guide.UnifiedDiff(label, diskBody, composed))
	}
	return "STALE"
}

// parsePins parses a banner `fragments=` value into alias -> sha. The sentinels
// `none`, `local`, and the empty (v1) string carry no pins.
func parsePins(raw string) map[string]string {
	pins := map[string]string{}
	if raw == "" || raw == "none" || raw == source.LocalPin {
		return pins
	}
	for _, part := range strings.Split(raw, ",") {
		alias, sha, ok := strings.Cut(part, "@")
		if ok && alias != "" && sha != "" {
			pins[alias] = sha
		}
	}
	return pins
}

// mergeSpecs overlays --source overrides onto a layout's sources.
func mergeSpecs(sources, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(sources)+len(overrides))
	for k, v := range sources {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
