package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	dats "github.com/wow-look-at-my/dats"
)

// helpCmd replaces cobra's built-in help command so `dats help <topic>`
// reaches the embedded documentation. Cobra's own version answers anything it
// does not recognize with the root command's usage, which sends a reader
// looking for the .dats file reference back to a list of flags.
var helpCmd = &cobra.Command{
	Use:   "help [command | topic]",
	Short: "Help for a command, or an embedded documentation topic",
	Long: `Print the help for a command, or a page of the documentation embedded in
this binary.

  dats help watch       the watch subcommand's flags and behavior
  dats help format      the complete .dats file reference
  dats docs             the list of documentation topics`,
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHelp(cmd, args)
	},
}

// runHelp resolves the argument as a command first and as a documentation
// topic second, so a subcommand name never loses to a topic alias.
func runHelp(cmd *cobra.Command, args []string) error {
	root := cmd.Root()
	if len(args) == 0 {
		return root.Help()
	}

	if target, _, err := root.Find(args); err == nil && target != nil && target != root {
		target.InitDefaultHelpFlag()
		target.InitDefaultVersionFlag()
		return target.Help()
	}

	if page, ok := dats.LookupDoc(args[0]); ok {
		return runDocs(cmd.OutOrStdout(), []string{page.Name})
	}

	return fmt.Errorf("unknown help topic %q: not a command, and not a docs topic (run `dats docs` for the list)", args[0])
}

func init() {
	rootCmd.SetHelpCommand(helpCmd)
}
