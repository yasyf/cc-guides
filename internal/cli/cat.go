package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/source"
)

func newCatCmd(ctx context.Context) *cobra.Command {
	var sources []string
	cmd := &cobra.Command{
		Use:   "cat <ref>",
		Short: "Print a fragment body: an import (alias:name) or a local piece (name)",
		Long: "Print the resolved body of a fragment. `alias:name` fetches a shared\n" +
			"import; a bare `name` finds a local *.fragment.* piece across the repo's\n" +
			"artifact dirs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides, err := parseSourceOverrides(sources)
			if err != nil {
				return exit(2, err)
			}
			return runCat(ctx, cmd, args[0], overrides)
		},
	}
	cmd.Flags().StringArrayVar(&sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runCat(ctx context.Context, cmd *cobra.Command, ref string, overrides map[string]string) error {
	if alias, name, ok := strings.Cut(ref, ":"); ok {
		return catImport(ctx, cmd, alias, name, overrides)
	}
	return catLocal(cmd, ref)
}

// catImport fetches a shared import and prints it, probing both kinds. The alias
// must be declared by some layout in the repo (or supplied via --source).
func catImport(ctx context.Context, cmd *cobra.Command, alias, name string, overrides map[string]string) error {
	specs, err := discoveredSpecs(repoRoot(), overrides)
	if err != nil {
		return exit(2, err)
	}
	if _, ok := specs[alias]; !ok {
		return exit(2, fmt.Errorf("unknown source alias %q: declare it in a layout.toml [sources.*] table or pass --source %s=<spec>", alias, alias))
	}
	resolver, err := source.New(source.Options{Specs: specs})
	if err != nil {
		return exit(2, err)
	}
	for _, kind := range guide.AllKinds {
		body, found, err := resolver.Resolve(ctx, alias, name, kind)
		if err != nil {
			return exit(2, err)
		}
		if found {
			_, werr := cmd.OutOrStdout().Write(body)
			return werr
		}
	}
	return exit(2, fmt.Errorf("%w: %s:%s", guide.ErrUnknownFragment, alias, name))
}

// catLocal finds and prints a local fragment piece by name across the repo's
// artifact dirs.
func catLocal(cmd *cobra.Command, name string) error {
	root := repoRoot()
	dirs, err := discoverArtifactDirs(root)
	if err != nil {
		return exit(2, err)
	}
	var hits []string
	for _, dir := range dirs {
		for _, kind := range guide.AllKinds {
			p := filepath.Join(root, filepath.FromSlash(dir), name+".fragment"+kind.Ext())
			if _, statErr := os.Stat(p); statErr == nil {
				hits = append(hits, p)
			}
		}
	}
	sort.Strings(hits)
	switch len(hits) {
	case 0:
		return exit(2, fmt.Errorf("%w: no local fragment %q found in any artifact dir", guide.ErrUnknownFragment, name))
	case 1:
		body, err := os.ReadFile(hits[0]) // #nosec G304 -- prints a discovered local fragment
		if err != nil {
			return exit(2, err)
		}
		_, werr := cmd.OutOrStdout().Write(body)
		return werr
	default:
		return exit(2, fmt.Errorf("ambiguous local fragment %q: found in %s", name, strings.Join(hits, ", ")))
	}
}
