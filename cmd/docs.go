package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	dats "github.com/wow-look-at-my/dats"
)

var docsCmd = &cobra.Command{
	Use:   "docs [topic...]",
	Short: "Print the embedded documentation",
	Long: `Print dats' own documentation, which ships inside the binary.

Run without arguments to list the topics. Pass one or more topic names to
print them, or "all" to print every page. ` + "`dats help <topic>`" + ` prints the
same pages, and topic names also accept their file spellings (for example
"file-format", "file-format.md", or "docs/file-format.md" for "format").

The output is the markdown source of the page, so it pipes into a pager or a
markdown renderer unchanged.`,
	Example: `  dats docs                 # list the topics
  dats docs format          # the complete .dats file reference
  dats docs cli             # flags, discovery, sandboxing, watch mode
  dats docs all | less      # everything, in one stream`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDocs(cmd.OutOrStdout(), args)
	},
}

// runDocs lists the topics (no args), or prints the named pages. "all" prints
// every page in topic-list order.
func runDocs(out io.Writer, args []string) error {
	if len(args) == 0 {
		printDocTopics(out)
		return nil
	}

	pages, err := resolveDocArgs(args)
	if err != nil {
		return err
	}

	// A single page prints bare, so `dats docs format > format.md` yields the
	// file itself; several pages need a banner to tell them apart.
	for i, page := range pages {
		text, err := page.Text()
		if err != nil {
			return err
		}
		if len(pages) > 1 {
			if i > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "========== %s ==========\n\n", page.File)
		}
		fmt.Fprint(out, text)
		if !strings.HasSuffix(text, "\n") {
			fmt.Fprintln(out)
		}
	}
	return nil
}

// resolveDocArgs maps topic arguments to pages, expanding "all" and rejecting
// an unknown name rather than quietly printing a shorter list than asked for.
func resolveDocArgs(args []string) ([]dats.DocPage, error) {
	var pages []dats.DocPage
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "all") {
			pages = append(pages, dats.Docs()...)
			continue
		}
		page, ok := dats.LookupDoc(arg)
		if !ok {
			return nil, fmt.Errorf("unknown docs topic %q (topics: %s)", arg, canonicalDocTopics())
		}
		pages = append(pages, page)
	}
	return pages, nil
}

func printDocTopics(out io.Writer) {
	fmt.Fprintln(out, "dats ships its own documentation. Print a topic with `dats docs <topic>`")
	fmt.Fprintln(out, "or `dats help <topic>`; `dats docs all` prints every page.")
	fmt.Fprintln(out)

	width := 0
	for _, page := range dats.Docs() {
		if len(page.Name) > width {
			width = len(page.Name)
		}
	}
	for _, page := range dats.Docs() {
		fmt.Fprintf(out, "  %-*s  %s\n", width, page.Name, page.Summary)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Each topic also answers to its aliases, e.g. `dats docs file-format` for `format`.")
}

// canonicalDocTopics is the comma-separated topic list an error message
// offers. It names the canonical topics only -- the aliases exist to catch a
// reader's first guess, not to be recited back at them.
func canonicalDocTopics() string {
	names := make([]string, 0, len(dats.Docs()))
	for _, page := range dats.Docs() {
		names = append(names, page.Name)
	}
	return strings.Join(names, ", ")
}

func init() {
	rootCmd.AddCommand(docsCmd)
}
