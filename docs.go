package dats

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// The prose documentation ships INSIDE the binary. A user who has the
// executable and nothing else -- no checkout, no network -- can read the
// complete reference with `dats docs <topic>`.
//
// The pages are embedded verbatim from docs/, so there is no second copy of
// the reference to keep in sync with the first one.
//
//go:embed docs/*.md
var docsFS embed.FS

// DocPage is one embedded documentation page.
type DocPage struct {
	// Name is the canonical topic name, as `dats docs <name>` takes it.
	Name string

	// Aliases are the other spellings that resolve to this page.
	Aliases []string

	// Summary is the one-line description the topic list prints.
	Summary string

	// File is the page's path inside the embedded filesystem.
	File string
}

// Text returns the page's markdown source.
func (p DocPage) Text() (string, error) {
	b, err := docsFS.ReadFile(p.File)
	if err != nil {
		return "", fmt.Errorf("embedded doc %q: %w", p.File, err)
	}
	return string(b), nil
}

// docPages is the topic table, in the order the topic list prints it:
// overview first, then the two references an author reaches for most.
//
// TestDocPagesCoverEmbeddedFiles pins this table to the embedded directory in
// both directions, so a new docs/*.md file fails the build until it has a
// topic name and a summary.
var docPages = []DocPage{
	{
		Name:    "overview",
		Aliases: []string{"readme", "index", "intro"},
		Summary: "What dats is, a quick start, and the key concepts on one page",
		File:    "docs/README.md",
	},
	{
		Name:    "format",
		Aliases: []string{"file-format", "schema", "yaml", "fields", "keys"},
		Summary: "Complete .dats reference: every key, placeholder, assertion, and parse error",
		File:    "docs/file-format.md",
	},
	{
		Name:    "cli",
		Aliases: []string{"usage", "flags", "sandbox", "watch", "jobs"},
		Summary: "Commands, flags, discovery, sandboxing, -j, watch mode, and output format",
		File:    "docs/cli.md",
	},
	{
		Name:    "examples",
		Aliases: []string{"example", "cookbook", "recipes"},
		Summary: "Annotated .dats files, from a one-line check to a full CLI suite",
		File:    "docs/examples.md",
	},
	{
		Name:    "library",
		Aliases: []string{"go", "api"},
		Summary: "Running suites in-process from Go: Options, Result, and the error contract",
		File:    "docs/library.md",
	},
	{
		Name:    "reports",
		Aliases: []string{"report", "junit", "json"},
		Summary: "--report-junit and --report-json formats, and their stability contract",
		File:    "docs/reports.md",
	},
	{
		Name:    "action",
		Aliases: []string{"github", "gha", "ci"},
		Summary: "Running dats from another repository's GitHub Actions workflow",
		File:    "docs/action.md",
	},
	{
		Name:    "sandbox-internals",
		Aliases: []string{"backends", "argv"},
		Summary: "How each backend builds its argv, and which details are load-bearing",
		File:    "docs/sandbox-internals.md",
	},
	{
		Name:    "masked-proc",
		Aliases: []string{"sandbox-masked-proc", "proc"},
		Summary: "Why a container refuses the sandbox a private /proc, and what the fallback keeps",
		File:    "docs/sandbox-masked-proc.md",
	},
}

// Docs returns every embedded documentation page, in topic-list order.
func Docs() []DocPage {
	out := make([]DocPage, len(docPages))
	copy(out, docPages)
	return out
}

// LookupDoc resolves a topic name or alias to its page. Matching ignores case
// and tolerates the spellings a reader arrives with: a `.md` suffix, a `docs/`
// prefix, and the file's own base name.
func LookupDoc(name string) (DocPage, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	want = strings.TrimPrefix(want, "docs/")
	want = strings.TrimSuffix(want, ".md")
	if want == "" {
		return DocPage{}, false
	}
	for _, p := range docPages {
		for _, cand := range p.spellings() {
			if want == cand {
				return p, true
			}
		}
	}
	return DocPage{}, false
}

// DocTopicNames returns every accepted spelling, sorted, for an error message
// that has to tell the reader what does work.
func DocTopicNames() []string {
	var names []string
	for _, p := range docPages {
		names = append(names, p.spellings()...)
	}
	sort.Strings(names)
	return names
}

// spellings is every string that resolves to this page: its name, its
// aliases, and its file's base name.
func (p DocPage) spellings() []string {
	base := strings.TrimSuffix(strings.TrimPrefix(p.File, "docs/"), ".md")
	out := make([]string, 0, len(p.Aliases)+2)
	out = append(out, p.Name)
	out = append(out, p.Aliases...)
	if !contains(out, strings.ToLower(base)) {
		out = append(out, strings.ToLower(base))
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
