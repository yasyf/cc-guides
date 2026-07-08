package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/fragments"
	"github.com/yasyf/cc-guides/guide"
)

func newCatCmd() *cobra.Command {
	var embedded bool
	cmd := &cobra.Command{
		Use:   "cat <name>",
		Short: "Print a fragment's resolved body to stdout",
		Long: "Print the resolved body of a fragment. A local override wins by default;\n" +
			"--embedded forces the shipped body.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCat(cmd, args[0], embedded)
		},
	}
	cmd.Flags().BoolVar(&embedded, "embedded", false, "print the embedded body, ignoring local overrides")
	return cmd
}

func runCat(cmd *cobra.Command, name string, embedded bool) error {
	resolver := fragments.Resolver()
	if !embedded {
		resolver = buildChain("")
	}
	var found []guide.Fragment
	for _, k := range guide.AllKinds {
		f, ok, err := resolver.Resolve(name, k)
		if err != nil {
			return exit(2, err)
		}
		if ok {
			found = append(found, f)
		}
	}
	switch len(found) {
	case 0:
		return exit(2, fmt.Errorf("%w: %q", guide.ErrUnknownFragment, name))
	case 1:
		_, err := cmd.OutOrStdout().Write(found[0].Body)
		return err
	default:
		return exit(2, fmt.Errorf("ambiguous fragment %q: exists as multiple kinds; disambiguation is not supported", name))
	}
}
