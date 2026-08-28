package dats

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The topic table is what makes a page reachable from `dats docs`. A new
// docs/*.md file that nobody added to it would ship inside the binary and be
// unreachable, which is the whole failure this test exists to prevent.
func TestDocPagesCoverEmbeddedFiles(t *testing.T) {
	embedded, err := fs.Glob(docsFS, "docs/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, embedded)

	var listed []string
	for _, page := range docPages {
		listed = append(listed, page.File)
	}

	assert.ElementsMatch(t, embedded, listed,
		"every docs/*.md file needs a topic name and summary in docPages, and every entry needs its file")
}

func TestDocPagesAreWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, page := range docPages {
		assert.NotEmpty(t, page.Name, "%s: topic name", page.File)
		assert.NotEmpty(t, page.Summary, "%s: summary", page.File)
		assert.Equal(t, strings.ToLower(page.Name), page.Name, "topic names are lowercase")

		for _, spelling := range page.spellings() {
			assert.NotContains(t, seen, spelling, "spelling %q already resolves to another page", spelling)
			seen[spelling] = page.Name
		}

		text, err := page.Text()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(text, "# "), "%s should open with an H1", page.File)
	}
}

func TestLookupDoc(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{"format", "format"},
		{"FORMAT", "format"},
		{"  format  ", "format"},
		{"file-format", "format"},
		{"file-format.md", "format"},
		{"docs/file-format.md", "format"},
		{"schema", "format"},
		{"readme", "overview"},
		{"README.md", "overview"},
		{"sandbox", "cli"},
		{"junit", "reports"},
		{"sandbox-masked-proc", "masked-proc"},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			page, ok := LookupDoc(tt.arg)
			require.True(t, ok, "%q should resolve", tt.arg)
			assert.Equal(t, tt.want, page.Name)
		})
	}
}

func TestLookupDocRejectsUnknown(t *testing.T) {
	for _, arg := range []string{"", "   ", "docs/", ".md", "nonsense", "test"} {
		_, ok := LookupDoc(arg)
		assert.False(t, ok, "%q should not resolve to a page", arg)
	}
}

func TestDocsReturnsACopy(t *testing.T) {
	pages := Docs()
	require.NotEmpty(t, pages)
	pages[0].Name = "clobbered"
	assert.NotEqual(t, "clobbered", Docs()[0].Name)
}

func TestDocTopicNamesListsEverySpelling(t *testing.T) {
	names := DocTopicNames()
	for _, page := range docPages {
		for _, spelling := range page.spellings() {
			assert.Contains(t, names, spelling)
		}
	}
	assert.IsIncreasing(t, names)
}
